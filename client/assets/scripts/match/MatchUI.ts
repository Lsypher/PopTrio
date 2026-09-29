// 对局场景（骨架期占位视图）：接收初始全量快照并以 9x9 色块 + 文本呈现
// 棋盘、双方得分与回合信息。棋盘交互与正式表现由后续 issue 实现。
import {
  Color,
  Component,
  Graphics,
  Label,
  Node,
  _decorator,
} from 'cc';
import { ensureStage } from '../core/Stage';
import { applySessionMessage, matchSession } from '../core/MatchSession';
import { net } from '../net/NetClient';
import {
  MSG_RESHUFFLE,
  MSG_SETTLEMENT,
  MSG_SWAP_REJECTED,
  MSG_TURN_STARTED,
  ServerMessage,
  SettlementPayload,
  Snapshot,
  SwapRejectedPayload,
} from '../protocol/Protocol';

const { ccclass } = _decorator;

const BOARD_SIZE = 9;
const CELL = 60;
const GAP = 4;
// 行优先颜色 ID → 占位色板（正式素材接入由后续 issue 完成）。
const TILE_COLORS = [
  new Color(229, 72, 77, 255),   // 红
  new Color(247, 107, 21, 255),  // 橙
  new Color(245, 217, 10, 255),  // 黄
  new Color(70, 167, 88, 255),   // 绿
  new Color(0, 144, 255, 255),   // 蓝
  new Color(142, 78, 198, 255),  // 紫
];
const COLOR_TEXT = new Color(235, 235, 235, 255);
const COLOR_MUTED = new Color(150, 150, 155, 255);

function makeLabel(parent: Node, name: string, text: string, fontSize: number, color: Color): Label {
  const node = new Node(name);
  node.layer = parent.layer;
  node.parent = parent;
  const label = node.addComponent(Label);
  label.string = text;
  label.fontSize = fontSize;
  label.lineHeight = Math.round(fontSize * 1.3);
  label.color = color;
  return label;
}

@ccclass('MatchUI')
export class MatchUI extends Component {
  private scoreLabel: Label | null = null;
  private turnLabel: Label | null = null;
  private statusLabel: Label | null = null;
  private boardGraphics: Graphics | null = null;
  private unsubMessage: (() => void) | null = null;
  private unsubClose: (() => void) | null = null;

  onLoad() {
    const canvas = ensureStage();
    this.scoreLabel = makeLabel(canvas, 'Score', '', 56, COLOR_TEXT);
    this.scoreLabel.node.setPosition(0, 540);
    this.turnLabel = makeLabel(canvas, 'Turn', '', 32, COLOR_MUTED);
    this.turnLabel.node.setPosition(0, 470);

    const boardNode = new Node('Board');
    boardNode.layer = canvas.layer;
    boardNode.parent = canvas;
    boardNode.setPosition(0, -40);
    this.boardGraphics = boardNode.addComponent(Graphics);

    this.statusLabel = makeLabel(canvas, 'Status', '', 36, COLOR_TEXT);
    this.statusLabel.node.setPosition(0, -460);

    const snapshot = matchSession.snapshot;
    if (snapshot) {
      this.renderSnapshot(snapshot);
      if (matchSession.settlement) {
        this.renderSettlement(matchSession.settlement);
      }
    } else {
      this.setStatus('等待对局数据…');
    }
    this.unsubMessage = net.onMessage(this.handleMessage);
    this.unsubClose = this.netClose;
  }

  onDestroy() {
    if (this.unsubMessage) {
      this.unsubMessage();
      this.unsubMessage = null;
    }
    if (this.unsubClose) {
      this.unsubClose();
      this.unsubClose = null;
    }
  }

  private netClose = () => {
    if (this.isValid) {
      this.setStatus('连接已断开');
    }
  };

  private setStatus(text: string) {
    if (this.statusLabel) {
      this.statusLabel.string = text;
    }
  }

  private handleMessage = (msg: ServerMessage) => {
    if (!this.isValid) {
      return;
    }
    applySessionMessage(msg);
    switch (msg.type) {
      case MSG_TURN_STARTED:
        this.renderSnapshot(msg.payload.snapshot);
        break;
      case MSG_RESHUFFLE: {
        // 死局重排：棋盘重画，分数与回合不变（服务端权威）。
        const p = msg.payload;
        if (this.boardGraphics) {
          this.drawBoard(p.board);
        }
        if (this.scoreLabel) {
          this.scoreLabel.string = this.scoreText(p.scores);
        }
        break;
      }
      case MSG_SWAP_REJECTED: {
        const p = msg.payload as SwapRejectedPayload;
        this.setStatus(`交换被拒：${p.reason}`);
        break;
      }
      case MSG_SETTLEMENT:
        this.renderSettlement(msg.payload as SettlementPayload);
        break;
      default:
        break;
    }
  };

  private scoreText(scores: [number, number]): string {
    return `${scores[0]} : ${scores[1]}`;
  }

  private renderSnapshot(snapshot: Snapshot) {
    if (this.scoreLabel) {
      this.scoreLabel.string = this.scoreText(snapshot.scores);
    }
    if (this.turnLabel) {
      this.turnLabel.string = `回合 ${snapshot.turn} · 操作方 P${snapshot.operator}`;
    }
    if (this.boardGraphics) {
      this.drawBoard(snapshot.board);
    }
    this.setStatus('');
  }

  private renderSettlement(p: SettlementPayload) {
    const outcome = p.draw ? '平局' : `P${p.winner} 胜`;
    this.setStatus(`终局 · ${outcome}`);
  }

  /** 全量重画棋盘占位视图：行优先 board → 9x9 色块。 */
  private drawBoard(board: number[]) {
    const g = this.boardGraphics;
    if (!g) {
      return;
    }
    g.clear();
    const span = BOARD_SIZE * CELL + (BOARD_SIZE - 1) * GAP;
    const left = -span / 2;
    const top = span / 2;
    for (let row = 0; row < BOARD_SIZE; row++) {
      for (let col = 0; col < BOARD_SIZE; col++) {
        const tile = board[row * BOARD_SIZE + col] ?? -1;
        const color = tile >= 0 && tile < TILE_COLORS.length ? TILE_COLORS[tile] : COLOR_MUTED;
        g.fillColor = color;
        const x = left + col * (CELL + GAP);
        const y = top - row * (CELL + GAP) - CELL;
        g.rect(x, y, CELL, CELL);
        g.fill();
      }
    }
  }
}
