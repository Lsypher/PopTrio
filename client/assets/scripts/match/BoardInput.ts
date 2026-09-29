// 棋盘输入：拖拽相邻交换（主输入）+ 两次点选相邻交换（便于桌面与自动化
// 验收）。只识别手势并回调，是否可交换由上层（回合归属、对局状态）裁决。
import { EventTouch, Node, UITransform, Vec3 } from 'cc';
import {
  BOARD_SIZE,
  BOARD_SPAN,
  BOARD_STEP,
  BoardView,
  Cell,
  adjacentCells,
  sameCell,
} from './BoardView';

export class BoardInput {
  private pressed: Cell | null = null;
  private dragFired = false;
  private selected: Cell | null = null;

  constructor(
    private host: Node,
    private hooks: {
      onSwap: (a: Cell, b: Cell) => void;
      onSelect: (cell: Cell | null) => void;
    },
  ) {
    host.on(Node.EventType.TOUCH_START, this.onStart, this);
    host.on(Node.EventType.TOUCH_MOVE, this.onMove, this);
    host.on(Node.EventType.TOUCH_END, this.onEnd, this);
    host.on(Node.EventType.TOUCH_CANCEL, this.onCancel, this);
  }

  private onStart(e: EventTouch) {
    const cell = this.hit(e);
    if (!cell) {
      return;
    }
    this.pressed = cell;
    this.dragFired = false;
  }

  private onMove(e: EventTouch) {
    if (!this.pressed || this.dragFired) {
      return;
    }
    const cell = this.hit(e);
    if (!cell || !adjacentCells(this.pressed, cell)) {
      return;
    }
    // 拖拽越过相邻格即触发一次交换；一次手势至多一次。
    this.dragFired = true;
    const a = this.pressed;
    this.pressed = null;
    this.select(null);
    this.hooks.onSwap(a, cell);
  }

  private onEnd(e: EventTouch) {
    if (!this.pressed) {
      return;
    }
    const from = this.pressed;
    this.pressed = null;
    if (this.dragFired) {
      return;
    }
    // 点选：与既有选中格相邻 → 交换；同格 → 取消；否则改选。
    const cell = this.hit(e) ?? from;
    const sel = this.selected;
    if (sel && !sameCell(sel, cell) && adjacentCells(sel, cell)) {
      this.select(null);
      this.hooks.onSwap(sel, cell);
      return;
    }
    this.select(sel && sameCell(sel, cell) ? null : cell);
  }

  private onCancel() {
    this.pressed = null;
  }

  /** 清除点选（快照硬同步后调用，避免残留过期选中格）。 */
  clearSelection() {
    this.pressed = null;
    this.select(null);
  }

  private select(cell: Cell | null) {
    this.selected = cell;
    this.hooks.onSelect(cell);
  }

  /** 触点 → 格子坐标；棋盘外返回 null。 */
  private hit(e: EventTouch): Cell | null {
    const ut = this.host.getComponent(UITransform);
    if (!ut) {
      return null;
    }
    const ui = e.getUILocation();
    const local = ut.convertToNodeSpaceAR(new Vec3(ui.x, ui.y, 0));
    const col = Math.floor((local.x + BOARD_SPAN / 2) / BOARD_STEP);
    const row = Math.floor((BOARD_SPAN / 2 - local.y) / BOARD_STEP);
    if (col < 0 || col >= BOARD_SIZE || row < 0 || row >= BOARD_SIZE) {
      return null;
    }
    return { col, row };
  }
}
