// WebSocket 连接管理：单例跨场景复用。hub 的会话状态机（idle→queued→
// inRoom）绑定在连接上，大厅进入对局后切换场景必须继续使用同一条连接。
import { ServerMessage, parseServerMessage } from '../protocol/Protocol';

type MessageListener = (msg: ServerMessage) => void;
type CloseListener = (event: CloseEvent) => void;

export type NetStatus = 'idle' | 'connecting' | 'open' | 'closed';

// 服务端地址：缺省本机调试端口，可用页面参数 ?server=ws://host/ws 覆盖。
export function resolveServerUrl(): string {
  if (typeof window !== 'undefined' && window.location && window.location.search) {
    const server = new URLSearchParams(window.location.search).get('server');
    if (server) {
      return server;
    }
  }
  return 'ws://127.0.0.1:8080/ws';
}

export class NetClient {
  private ws: WebSocket | null = null;
  private msgListeners = new Set<MessageListener>();
  private closeListeners = new Set<CloseListener>();
  private openPromise: Promise<void> | null = null;
  status: NetStatus = 'idle';

  /** 订阅服务端消息，返回退订函数。 */
  onMessage(listener: MessageListener): () => void {
    this.msgListeners.add(listener);
    return () => this.msgListeners.delete(listener);
  }

  /** 订阅连接关闭，返回退订函数。 */
  onClose(listener: CloseListener): () => void {
    this.closeListeners.add(listener);
    return () => this.closeListeners.delete(listener);
  }

  get isOpen(): boolean {
    return this.status === 'open';
  }

  /** 建立连接；已在途或已连接时复用既有连接。 */
  connect(): Promise<void> {
    if (this.openPromise) {
      return this.openPromise;
    }
    this.status = 'connecting';
    this.openPromise = new Promise<void>((resolve, reject) => {
      let ws: WebSocket;
      try {
        ws = new WebSocket(resolveServerUrl());
      } catch (err) {
        this.status = 'closed';
        this.openPromise = null;
        reject(err instanceof Error ? err : new Error(String(err)));
        return;
      }
      this.ws = ws;
      ws.onopen = () => {
        this.status = 'open';
        resolve();
      };
      ws.onmessage = (event) => {
        if (typeof event.data !== 'string') {
          return;
        }
        let msg: ServerMessage;
        try {
          msg = parseServerMessage(event.data);
        } catch {
          // 服务端帧畸形：本端无法处理，忽略并保持连接。
          return;
        }
        for (const listener of Array.from(this.msgListeners)) {
          listener(msg);
        }
      };
      ws.onclose = (event) => {
        this.status = 'closed';
        this.ws = null;
        this.openPromise = null;
        for (const listener of Array.from(this.closeListeners)) {
          listener(event);
        }
      };
      ws.onerror = () => {
        // 错误细节随 close 事件到达，这里只兜底未 open 的失败。
        if (this.status === 'connecting') {
          this.status = 'closed';
          this.ws = null;
          this.openPromise = null;
          reject(new Error('websocket connect failed'));
        }
      };
    });
    return this.openPromise;
  }

  /** 发送一条已编码消息；连接不可用时返回 false。 */
  send(text: string): boolean {
    if (this.status !== 'open' || !this.ws) {
      return false;
    }
    this.ws.send(text);
    return true;
  }

  close(): void {
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.status = 'closed';
    this.openPromise = null;
  }
}

/** 全局单例：跨场景共享同一条 WS 连接。 */
export const net = new NetClient();

// 断线演练辅助（issue 08 验收）：页面带 ?debug=1 时把连接单例挂到 window，
// 供自动化脚本强制断线、驱动重连恢复流程。正常游玩零行为差异。
if (typeof window !== 'undefined' && typeof window.location !== 'undefined' && new URLSearchParams(window.location.search).has('debug')) {
  (window as unknown as { __poptrioNet?: NetClient }).__poptrioNet = net;
}
