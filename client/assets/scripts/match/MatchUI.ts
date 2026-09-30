// 对局场景：棋盘交互与表现层编排（issue 07）。
//
// 同步模型（ADR-0003）：服务端权威、客户端纯重放。本文件维护一条串行
// 「时间线队列」——服务端帧（结果/否决/重排）排队逐个重放；玩家发送交换
// 时若队列空闲则立即做乐观交换预览（纯视觉、可整体作废、不写任何权威
// 状态），收到结果帧后续播消除/下落/连锁，收到否决帧则回弹。回合边界
// 快照（turn_started）是天然对账点：无条件硬同步到快照（清队列、重绘、
// 重置未决状态）。任何时刻屏幕表现最终都与服务端快照可对账。
// UI 骨架由编辑器搭建（ADR-0005），本组件只驱动行为；Board 的 Tile/Slot
// 仍由 BoardView 在运行时动态生成。
import {
  Button,
  Color,
  Component,
  director,
  Label,
  Node,
  SpriteFrame,
  _decorator,
} from 'cc';
import { applySessionMessage, matchSession } from '../core/MatchSession';
import { net } from '../net/NetClient';
import {
  ErrorPayload,
  MSG_ERROR,
  MSG_RECONNECTED,
  MSG_RECONNECT_REJECTED,
  MSG_RESHUFFLE,
  MSG_SETTLEMENT,
  MSG_SWAP_REJECTED,
  MSG_SWAP_RESULT,
  MSG_TURN_STARTED,
  Pos,
  ReconnectedPayload,
  ReconnectRejectedPayload,
  ServerMessage,
  SettlementPayload,
  Snapshot,
  SwapRejectedPayload,
  SwapResultPayload,
  Wave,
  encodeReconnect,
  encodeSwap,
} from '../protocol/Protocol';
import { BoardInput } from './BoardInput';
import { BoardView, Cell } from './BoardView';
import { loadFruitFrames } from './FruitSprites';
import { SettlementView } from './SettlementView';
import { TurnClock } from './TurnClock';

const { ccclass, property } = _decorator;

const SWAP_SECONDS = 0.14;
const COLOR_TEXT = new Color(235, 235, 235, 255);
const COLOR_URGENT = new Color(255, 96, 88, 255);

// 重连节奏：宽限期 60s（服务端配置）内每 2s 一试；带内响应兜底超时后重试；
// 超过宽限期仍失败即放弃（绑定已随判负结算摘除，重试无意义）。
const RECONNECT_INTERVAL_MS = 2_000;
const RECONNECT_RESPONSE_MS = 5_000;
const RECONNECT_GIVEUP_MS = 70_000;

/** 交换请求（坐标对）。 */
interface SwapLike {
  a: Pos;
  b: Pos;
}

/** 时间线队列任务：全部是服务端帧的视觉重放。 */
type Job =
  | { kind: 'replay'; swap: SwapLike; waves: Wave[]; board: number[]; scores: [number, number]; operator: number }
  | { kind: 'bounce'; swap: SwapLike; preview: Promise<void> | null; mine: boolean }
  | { kind: 'reshuffle'; board: number[] };

/** 未决交换（已发送、未收到回执）。 */
interface PendingSwap {
  swap: SwapLike;
  /** 回合边界/重排后失效：不再做乐观预览，仅保留 FIFO 对账。 */
  stale: boolean;
}

function sameSwap(x: SwapLike, y: SwapLike): boolean {
  const direct = x.a.col === y.a.col && x.a.row === y.a.row && x.b.col === y.b.col && x.b.row === y.b.row;
  const reversed = x.a.col === y.b.col && x.a.row === y.b.row && x.b.col === y.a.col && x.b.row === y.a.row;
  return direct || reversed;
}

function asCell(p: Pos): Cell {
  return { col: p.col, row: p.row };
}

function rejectText(reason: string): string {
  switch (reason) {
    case 'not_operator':
      return '交换被拒：未轮到你操作';
    case 'invalid_swap':
      return '交换被拒：位置非法';
    case 'no_clear':
      return '交换被拒：不成连线，已回弹';
    default:
      return `交换被拒：${reason}`;
  }
}

@ccclass('MatchUI')
export class MatchUI extends Component {
  @property(Label)
  private scoreLabel: Label | null = null;

  @property(Label)
  private turnLabel: Label | null = null;

  @property(Label)
  private countdownLabel: Label | null = null;

  @property(Label)
  private statusLabel: Label | null = null;

  /** 棋盘宿主：BoardView/BoardInput 在其下动态生成 Tile/Slot。 */
  @property(Node)
  private boardHost: Node | null = null;

  /** 重连失败出口，默认隐藏，仅在宽限期耗尽/重连被拒时显示。 */
  @property(Button)
  private exitButton: Button | null = null;

  /** 结算面板：SettlementView.prefab 实例，默认隐藏。压缩实例内部的组件
   *  无法被场景 JSON 直接引用（ADR-0005 备注），故引用根节点运行时取组件。 */
  @property(Node)
  private settlementRoot: Node | null = null;

  private settlement: SettlementView | null = null;

  private board: BoardView | null = null;
  private input: BoardInput | null = null;
  private clock = new TurnClock();
  private unsubMessage: (() => void) | null = null;
  private unsubClose: (() => void) | null = null;

  // —— 断线重连（issue 08）：对局中断线即凭 token 自动重连恢复 ——
  private reconnecting = false;
  private reconnectTimer: number | null = null;
  private reconnectGiveUpAt = 0;

  // —— 服务端确认状态（仅随权威帧/快照更新）——
  private scores: [number, number] = [0, 0];
  private turn = 0;
  private operator = 0;
  private settled = false;

  // —— 时间线状态 ——
  private queue: Job[] = [];
  private pending: PendingSwap[] = [];
  private optimistic: { swap: SwapLike; done: Promise<void> } | null = null;
  private playing = false;
  private epoch = 0; // 回合边界硬同步后使旧播放循环失效

  // —— 初始化缓冲：素材异步加载期间到达的消息先入站 ——
  private inbox: ServerMessage[] = [];
  private ready = false;

  onLoad() {
    this.exitButton?.node.on(Button.EventType.CLICK, this.backToLobby);
    this.settlement = this.settlementRoot?.getComponent(SettlementView) ?? null;
    if (this.settlement) {
      this.settlement.hooks = {
        onRematch: this.rematch,
        onLobby: this.backToLobby,
      };
    }
    this.unsubMessage = net.onMessage(this.handleMessage);
    this.unsubClose = net.onClose(this.netClose);
    void this.initBoard();
  }

  onDestroy() {
    this.exitButton?.node.off(Button.EventType.CLICK, this.backToLobby);
    if (this.unsubMessage) {
      this.unsubMessage();
      this.unsubMessage = null;
    }
    if (this.unsubClose) {
      this.unsubClose();
      this.unsubClose = null;
    }
    this.stopReconnect();
  }

  private netClose = () => {
    if (!this.isValid) {
      return;
    }
    // 对局中断线（终局后服务端正常关连不在此列）：凭 token 自动重连恢复。
    if (this.settled || !matchSession.active || !matchSession.token) {
      this.setStatus('连接已断开');
      return;
    }
    this.startReconnect();
  };

  /** 素材加载完成后装配棋盘与输入，再冲刷初始化期间缓存的消息。 */
  private async initBoard() {
    let frames: SpriteFrame[] | null = null;
    try {
      frames = await loadFruitFrames();
    } catch {
      frames = null; // 素材缺失回退占位色块，不阻塞可玩
    }
    if (!this.isValid || !this.boardHost) {
      return;
    }
    this.board = new BoardView(this.boardHost, frames);
    this.input = new BoardInput(this.boardHost, {
      onSwap: this.requestSwap,
      onSelect: (cell) => this.board?.setSelected(cell),
    });

    const snapshot = matchSession.snapshot;
    if (snapshot) {
      this.resync(snapshot, true); // 场景载入即 match_started：窗口刚开始
      if (matchSession.settlement) {
        this.renderSettlement(matchSession.settlement);
      }
    } else {
      this.setStatus('等待对局数据…');
    }

    this.ready = true;
    const buffered = this.inbox;
    this.inbox = [];
    for (const msg of buffered) {
      this.dispatch(msg);
    }
  }

  private setStatus(text: string) {
    if (this.statusLabel) {
      this.statusLabel.string = text;
    }
  }

  // ---- 消息分派 ----

  private handleMessage = (msg: ServerMessage) => {
    if (!this.isValid) {
      return;
    }
    applySessionMessage(msg);
    if (!this.ready) {
      this.inbox.push(msg);
      return;
    }
    this.dispatch(msg);
  };

  private dispatch(msg: ServerMessage) {
    switch (msg.type) {
      case MSG_TURN_STARTED:
        // 回合边界快照：硬对账点（清队列/重置未决/全量重绘/重校倒计时）。
        this.resync(msg.payload.snapshot, true);
        break;
      case MSG_SWAP_RESULT: {
        const p = msg.payload as SwapResultPayload;
        if (this.mySeat !== null && p.operator === this.mySeat) {
          // FIFO 认领未决交换；队列为空说明与服务器失步，帧内终局棋盘兜底对账。
          this.pending.shift();
        }
        this.enqueue({
          kind: 'replay',
          swap: { a: p.swap.a, b: p.swap.b },
          waves: p.waves,
          board: p.board,
          scores: p.scores,
          operator: p.operator,
        });
        break;
      }
      case MSG_SWAP_REJECTED: {
        const p = msg.payload as SwapRejectedPayload;
        const mine = this.mySeat !== null && p.seat === this.mySeat;
        if (mine) {
          this.pending.shift();
          this.setStatus(rejectText(p.reason));
          const op = this.optimistic;
          if (op && sameSwap(op.swap, { a: p.swap.a, b: p.swap.b })) {
            this.optimistic = null;
            this.enqueue({ kind: 'bounce', swap: op.swap, preview: op.done, mine: true });
          }
        } else {
          // 对方的无效尝试：完整呈现交换 + 回弹。
          this.enqueue({ kind: 'bounce', swap: { a: p.swap.a, b: p.swap.b }, preview: null, mine: false });
        }
        break;
      }
      case MSG_RESHUFFLE: {
        const p = msg.payload;
        this.enqueue({ kind: 'reshuffle', board: p.board });
        this.setStatus('死局 · 棋盘已重排');
        break;
      }
      case MSG_RECONNECTED: {
        // 重连恢复（房间广播，对手也会收到）：全量快照硬同步，倒计时走
        // 窗口中途恢复路径（不重估时钟偏移）。
        this.stopReconnect();
        const p = msg.payload as ReconnectedPayload;
        this.resync(p.snapshot, false);
        break;
      }
      case MSG_RECONNECT_REJECTED: {
        const p = msg.payload as ReconnectRejectedPayload;
        this.stopReconnect();
        this.setStatus(`重连被拒：${p.reason}`);
        this.showExitButton();
        break;
      }
      case MSG_SETTLEMENT:
        this.renderSettlement(msg.payload as SettlementPayload);
        break;
      case MSG_ERROR: {
        const p = msg.payload as ErrorPayload;
        // 重连中的错误帧（如旧连接尚未清理时的 bad_token）按可重试处理，
        // 由重连循环的放弃兜底终结；其余正常展示。
        if (this.reconnecting) {
          this.scheduleRetry();
        } else {
          this.setStatus(`错误：${p.message}`);
        }
        break;
      }
      default:
        break;
    }
  }

  // ---- 输入 ----

  private get mySeat(): number | null {
    return matchSession.mySeat;
  }

  private requestSwap = (a: Cell, b: Cell) => {
    if (!this.canAct()) {
      return;
    }
    const swap: SwapLike = { a: { col: a.col, row: a.row }, b: { col: b.col, row: b.row } };
    if (!net.send(encodeSwap(swap.a, swap.b))) {
      this.setStatus('连接已断开');
      return;
    }
    this.pending.push({ swap, stale: false });
    this.maybeOptimistic();
  };

  private canAct(): boolean {
    return !this.settled && this.mySeat !== null && this.operator === this.mySeat && net.isOpen;
  }

  // ---- 时间线队列 ----

  private enqueue(job: Job) {
    this.queue.push(job);
    void this.drain();
  }

  /** 串行播放队列；回合边界（epoch 变更）使旧循环让位。 */
  private async drain() {
    if (this.playing) {
      return;
    }
    this.playing = true;
    const my = this.epoch;
    while (this.queue.length > 0 && my === this.epoch) {
      const job = this.queue.shift()!;
      try {
        await this.runJob(job);
      } catch {
        // 单个任务中断不致失步：任务末尾均以帧内权威数据兜底。
      }
    }
    if (my !== this.epoch) {
      return; // 已被硬同步接管
    }
    this.playing = false;
    this.maybeOptimistic();
  }

  private async runJob(job: Job) {
    const my = this.epoch;
    if (job.kind === 'replay') {
      const claimed = await this.claimOptimisticFor(job.swap);
      if (my !== this.epoch) {
        return; // 回合边界硬同步已接管，停止写状态
      }
      if (!claimed) {
        // 无匹配预览：若仍有在途预览（失步），整体作废后再重放交换。
        if (this.optimistic) {
          this.optimistic = null;
          this.board?.snapHome();
        }
        await (this.board?.animateSwap(asCell(job.swap.a), asCell(job.swap.b)) ?? Promise.resolve());
        if (my !== this.epoch) {
          return;
        }
      }
      // 波次重放：每波单消即时计入操作者显示分（Attribution：全归操作者）。
      for (const wave of job.waves) {
        this.scores[job.operator] += wave.score;
        this.renderHud();
        await (this.board?.replayWave(wave.clear, wave.board, () => my !== this.epoch) ?? Promise.resolve());
        if (my !== this.epoch) {
          return;
        }
      }
      // 对账：帧内终局棋盘与累计分为权威。
      this.scores = [job.scores[0], job.scores[1]];
      this.renderHud();
      return;
    }
    if (job.kind === 'bounce') {
      if (job.preview) {
        await job.preview; // 预演动画先落定（含在途情形）
      } else {
        // 未预演过的尝试（对方被拒等）：纯视觉呈现交换再回弹，不碰簿记。
        await (this.board?.previewSwap(asCell(job.swap.a), asCell(job.swap.b)) ?? Promise.resolve());
      }
      if (my !== this.epoch) {
        return;
      }
      await (this.board?.tweenHome(SWAP_SECONDS) ?? Promise.resolve());
      return;
    }
    // reshuffle：重排帧只可能紧跟某次 swap_result（死局检测），此前的乐观
    // 预览一并作废；未决交换保留 FIFO（其回执按新棋盘重放）。
    this.optimistic = null;
    this.pending = this.pending.map((e) => ({ ...e, stale: true }));
    await (this.board?.crossfade(job.board) ?? Promise.resolve());
  }

  /** 若 swap 恰是当前乐观预览：提交簿记并等待预演动画，返回 true。 */
  private async claimOptimisticFor(swap: SwapLike): Promise<boolean> {
    const op = this.optimistic;
    if (op && sameSwap(op.swap, swap)) {
      this.optimistic = null;
      this.board?.confirmSwap(asCell(swap.a), asCell(swap.b));
      await op.done;
      return true;
    }
    return false;
  }

  /** 队列空闲且存在未决交换时，为最旧的未决交换启动乐观预览。 */
  private maybeOptimistic() {
    if (this.playing || this.optimistic || !this.board) {
      return;
    }
    const head = this.pending[0];
    if (!head || head.stale) {
      return;
    }
    this.optimistic = {
      swap: head.swap,
      done: this.board.previewSwap(asCell(head.swap.a), asCell(head.swap.b)),
    };
  }

  // ---- 快照硬同步 ----

  /** freshWindow：快照是否恰逢回合窗口刚开始（match_started / turn_started）。
   *  重连恢复快照落在本回合中途，倒计时不重估时钟偏移（见 TurnClock）。 */
  private resync(snapshot: Snapshot, freshWindow: boolean) {
    this.epoch++; // 旧播放循环与旧动画全部让位
    this.queue = [];
    this.playing = false;
    this.optimistic = null;
    this.pending = this.pending.map((e) => ({ ...e, stale: true }));
    this.scores = [snapshot.scores[0], snapshot.scores[1]];
    this.turn = snapshot.turn;
    this.operator = snapshot.operator;
    if (freshWindow) {
      this.clock.syncWindowStart(snapshot.deadline);
    } else {
      this.clock.resnap(snapshot.deadline);
    }
    this.board?.build(snapshot.board);
    this.board?.setSelected(null);
    this.input?.clearSelection();
    this.renderHud();
    this.setStatus('');
    void this.drain();
  }

  private renderSettlement(p: SettlementPayload) {
    this.settled = true;
    this.scores = [p.scores[0], p.scores[1]];
    this.renderHud();
    this.stopReconnect();
    this.settlement?.show(p, this.mySeat);
  }

  // ---- 断线重连（issue 08）----

  private startReconnect() {
    if (this.reconnecting) {
      return;
    }
    this.reconnecting = true;
    this.reconnectGiveUpAt = Date.now() + RECONNECT_GIVEUP_MS;
    this.setStatus('连接断开，正在重连…');
    this.attemptReconnect();
  }

  private attemptReconnect = () => {
    if (!this.isValid || !this.reconnecting || this.settled) {
      return;
    }
    if (Date.now() >= this.reconnectGiveUpAt) {
      this.giveUpReconnect();
      return;
    }
    net.connect()
      .then(() => {
        if (!this.isValid || !this.reconnecting || this.settled) {
          return;
        }
        // 服务端校验 token 后回 reconnected（全量快照）或错误帧；本方法
        // 不等待回帧——由 scheduleRetry 兜底重试，帧到达时停止循环。
        if (!net.send(encodeReconnect(matchSession.token))) {
          this.scheduleRetry();
          return;
        }
        this.scheduleRetry(RECONNECT_RESPONSE_MS);
      })
      .catch(() => {
        if (this.isValid && this.reconnecting && !this.settled) {
          this.scheduleRetry();
        }
      });
  };

  /** 安排下一次重连尝试；已有在途定时器时幂等（帧兜底与错误帧竞争无副作用）。 */
  private scheduleRetry(delayMs = RECONNECT_INTERVAL_MS) {
    if (!this.reconnecting || this.reconnectTimer !== null) {
      return;
    }
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null;
      this.attemptReconnect();
    }, delayMs);
  }

  private stopReconnect() {
    this.reconnecting = false;
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  /** 宽限期耗尽仍连不上：绑定已随判负结算摘除，提供返回大厅出口。 */
  private giveUpReconnect() {
    this.stopReconnect();
    this.setStatus('重连失败：对局已结束');
    this.showExitButton();
  }

  private showExitButton() {
    if (this.exitButton) {
      this.exitButton.node.active = true;
    }
  }

  // ---- 结算出口 ----

  private rematch = () => {
    matchSession.autoQueue = true;
    director.loadScene('Lobby');
  };

  private backToLobby = () => {
    matchSession.autoQueue = false;
    director.loadScene('Lobby');
  };

  // ---- HUD ----

  private renderHud() {
    if (this.scoreLabel) {
      const s = this.scores;
      this.scoreLabel.string = this.mySeat === 1 ? `${s[1]} : ${s[0]}` : `${s[0]} : ${s[1]}`;
    }
    if (this.turnLabel) {
      const myTurn = this.mySeat !== null && this.operator === this.mySeat;
      this.turnLabel.string = this.settled
        ? '对局结束'
        : `回合 ${this.turn} · ${myTurn ? '你的回合' : `P${this.operator} 操作中`}`;
    }
  }

  /** 倒计时：由服务端 deadline 本地插值，每帧刷新。 */
  protected update() {
    const label = this.countdownLabel;
    if (!label) {
      return;
    }
    if (this.settled || !this.clock.synced) {
      label.node.active = false;
      return;
    }
    label.node.active = true;
    const remain = this.clock.remainingMs() / 1000;
    label.string = `${remain.toFixed(1)} s`;
    label.color = remain <= 3 ? COLOR_URGENT : COLOR_TEXT;
  }
}
