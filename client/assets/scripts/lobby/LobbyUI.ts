// 大厅场景：开始匹配 / 取消匹配、排队等待与超时提示、匹配成功切换对局
// 场景。UI 骨架由编辑器搭建（ADR-0005），本组件只驱动行为。
import {
  Button,
  Component,
  director,
  Label,
  _decorator,
} from 'cc';
import { applySessionMessage, matchSession } from '../core/MatchSession';
import { net } from '../net/NetClient';
import {
  CODE_BAD_STATE,
  ErrorPayload,
  MSG_ERROR,
  MSG_MATCH_STARTED,
  MSG_QUEUE_CANCELLED,
  MSG_QUEUE_JOINED,
  MSG_QUEUE_TIMEOUT,
  ServerMessage,
  encodeQueueCancel,
  encodeQueueJoin,
} from '../protocol/Protocol';

const { ccclass, property } = _decorator;

type LobbyState = 'idle' | 'connecting' | 'queued';

@ccclass('LobbyUI')
export class LobbyUI extends Component {
  @property(Label)
  private statusLabel: Label | null = null;

  @property(Label)
  private buttonLabel: Label | null = null;

  @property(Button)
  private actionButton: Button | null = null;

  private state: LobbyState = 'idle';
  private unsubMessage: (() => void) | null = null;
  private unsubClose: (() => void) | null = null;

  onLoad() {
    this.actionButton?.node.on(Button.EventType.CLICK, this.onAction);
    this.enterIdle('点击「开始匹配」寻找对手');
    this.unsubMessage = net.onMessage(this.handleMessage);
    this.unsubClose = net.onClose(this.handleClose);
    // 再战入口（issue 08）：结算画面点「再战」切回大厅即自动重新排队。
    if (matchSession.autoQueue) {
      matchSession.autoQueue = false;
      this.startQueue();
    }
  }

  onDestroy() {
    // 场景销毁先拆子节点再拆自身组件：actionButton 可能已被析构（node 被引擎置回 null），须再判空。
    this.actionButton?.node?.off(Button.EventType.CLICK, this.onAction);
    if (this.unsubMessage) {
      this.unsubMessage();
      this.unsubMessage = null;
    }
    if (this.unsubClose) {
      this.unsubClose();
      this.unsubClose = null;
    }
  }

  private onAction = () => {
    if (this.state === 'idle') {
      this.startQueue();
    } else if (this.state === 'queued') {
      if (!net.send(encodeQueueCancel())) {
        this.enterIdle('连接已断开，请重试');
      }
    }
  };

  private startQueue() {
    if (this.state !== 'idle') {
      return;
    }
    this.state = 'connecting';
    this.setStatus('连接服务器…');
    net.connect()
      .then(() => {
        if (!this.isValid || this.state !== 'connecting') {
          return;
        }
        if (net.send(encodeQueueJoin())) {
          this.state = 'queued';
          this.setButton('取消匹配');
          this.setStatus('排队中…等待对手');
        } else {
          this.enterIdle('连接已断开，请重试');
        }
      })
      .catch(() => {
        if (this.isValid && this.state === 'connecting') {
          this.enterIdle('无法连接服务器，请稍后重试');
        }
      });
  }

  private enterIdle(status: string) {
    this.state = 'idle';
    this.setButton('开始匹配');
    this.setStatus(status);
  }

  private setStatus(text: string) {
    if (this.statusLabel) {
      this.statusLabel.string = text;
    }
  }

  private setButton(text: string) {
    if (this.buttonLabel) {
      this.buttonLabel.string = text;
    }
  }

  private handleMessage = (msg: ServerMessage) => {
    if (!this.isValid) {
      return;
    }
    applySessionMessage(msg);
    switch (msg.type) {
      case MSG_QUEUE_JOINED:
        // 冗余保护：协议上 queue_join 无回执前的过渡态也收敛为排队中。
        if (this.state === 'connecting') {
          this.state = 'queued';
          this.setButton('取消匹配');
          this.setStatus('排队中…等待对手');
        }
        break;
      case MSG_QUEUE_CANCELLED:
        this.enterIdle('已取消匹配');
        break;
      case MSG_QUEUE_TIMEOUT:
        this.enterIdle('暂无对手，请重新排队');
        break;
      case MSG_ERROR: {
        const payload = msg.payload as ErrorPayload;
        if (this.state === 'queued' && payload.code === CODE_BAD_STATE) {
          this.enterIdle('连接状态异常，请重新排队');
        } else {
          this.setStatus(`错误：${payload.message}`);
        }
        break;
      }
      case MSG_MATCH_STARTED:
        if (this.state === 'queued') {
          this.state = 'idle';
          this.setStatus('匹配成功！');
          director.loadScene('Match');
        }
        break;
      default:
        break;
    }
  };

  private handleClose = () => {
    if (this.isValid) {
      this.enterIdle('连接已断开，请重试');
    }
  };
}
