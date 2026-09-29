// 棋盘视图：9x9 棋子节点网格 + 动画原语（预览交换、确认/动画交换、消除、
// 下落、补充、重排、归位）。只做表现：棋子颜色完全来自服务端帧数据，本
// 模块零规则计算（ADR-0003：前端零计算、纯重放）。
//
// 簿记语义是关键：tiles 网格始终表示「当前已确认的棋盘状态」——
// - previewSwap 只移动节点视觉，不碰簿记（乐观预览可随时整体作废）；
// - animateSwap / confirmSwap 才提交簿记，供波次差分重放使用。
import {
  Color,
  Graphics,
  Node,
  Sprite,
  SpriteFrame,
  Tween,
  tween,
  UIOpacity,
  UITransform,
  Vec3,
} from 'cc';
import { fruitIndexForColor } from './FruitSprites';

export const BOARD_SIZE = 9;
export const BOARD_CELL = 64;
export const BOARD_GAP = 4;
export const BOARD_STEP = BOARD_CELL + BOARD_GAP;
export const BOARD_SPAN = BOARD_SIZE * BOARD_CELL + (BOARD_SIZE - 1) * BOARD_GAP;

const SWAP_SECONDS = 0.14;
const CLEAR_SECONDS = 0.18;
const FALL_SECONDS = 0.26;
const RESHUFFLE_SECONDS = 0.3;
// 动画 promise 兜底：节点被销毁（回合边界重绘等）时 tween 回调不再触发，
// 超时放行避免播放队列卡死。
const ANIMATION_GRACE_MS = 400;

// 素材缺失时的回退色板（与骨架期一致：红/橙/黄/绿/蓝/紫）。
const FALLBACK_COLORS = [
  new Color(229, 72, 77, 255),
  new Color(247, 107, 21, 255),
  new Color(245, 217, 10, 255),
  new Color(70, 167, 88, 255),
  new Color(0, 144, 255, 255),
  new Color(142, 78, 198, 255),
];
const SLOT_COLOR = new Color(38, 42, 52, 255);
const SELECT_COLOR = new Color(255, 214, 90, 255);

/** 格子坐标（原点左上角，与服务端协议一致）。 */
export interface Cell {
  col: number;
  row: number;
}

/** 格子 → 棋盘局部坐标（节点锚点居中）。 */
export function cellToLocal(col: number, row: number): Vec3 {
  const left = -BOARD_SPAN / 2;
  const top = BOARD_SPAN / 2;
  return new Vec3(
    left + col * BOARD_STEP + BOARD_CELL / 2,
    top - row * BOARD_STEP - BOARD_CELL / 2,
    0,
  );
}

export function sameCell(a: Cell, b: Cell): boolean {
  return a.col === b.col && a.row === b.row;
}

export function adjacentCells(a: Cell, b: Cell): boolean {
  return Math.abs(a.col - b.col) + Math.abs(a.row - b.row) === 1;
}

/** 位置 tween → promise；节点销毁时超时兜底。 */
function tweenTo(node: Node, target: Vec3, seconds: number): Promise<void> {
  return new Promise((resolve) => {
    let done = false;
    const finish = () => {
      if (!done) {
        done = true;
        resolve();
      }
    };
    tween(node).to(seconds, { position: target.clone() }).call(finish).start();
    setTimeout(finish, seconds * 1000 + ANIMATION_GRACE_MS);
  });
}

/** 消除动画：缩放至消失并销毁节点。 */
function scaleOut(node: Node, seconds: number): Promise<void> {
  return new Promise((resolve) => {
    let done = false;
    const finish = () => {
      if (!done) {
        done = true;
        if (node.isValid) {
          node.destroy();
        }
        resolve();
      }
    };
    tween(node).to(seconds, { scale: new Vec3(0.05, 0.05, 1) }).call(finish).start();
    setTimeout(finish, seconds * 1000 + ANIMATION_GRACE_MS);
  });
}

function opacityTo(op: UIOpacity, value: number, seconds: number): Promise<void> {
  return new Promise((resolve) => {
    tween(op).to(seconds, { opacity: value }).call(() => resolve()).start();
  });
}

interface TileView {
  node: Node;
  color: number;
}

export class BoardView {
  private tiles: (TileView | null)[][] = [];
  private tileRoot: Node;
  private slotGraphics: Graphics;
  private selectGraphics: Graphics;

  constructor(host: Node, private frames: SpriteFrame[] | null) {
    let ut = host.getComponent(UITransform);
    if (!ut) {
      ut = host.addComponent(UITransform);
    }
    ut.setContentSize(BOARD_SPAN, BOARD_SPAN);

    const slotNode = new Node('Slots');
    slotNode.layer = host.layer;
    slotNode.parent = host;
    this.slotGraphics = slotNode.addComponent(Graphics);
    this.drawSlots();

    this.tileRoot = new Node('Tiles');
    this.tileRoot.layer = host.layer;
    this.tileRoot.parent = host;

    const selectNode = new Node('Select');
    selectNode.layer = host.layer;
    selectNode.parent = host;
    this.selectGraphics = selectNode.addComponent(Graphics);
  }

  /** 全量重建：以服务端棋盘数据重绘全部棋子（resync / 初始渲染 / 重排）。 */
  build(board: number[]) {
    this.tileRoot.destroyAllChildren();
    this.tiles = [];
    for (let row = 0; row < BOARD_SIZE; row++) {
      const line: (TileView | null)[] = [];
      for (let col = 0; col < BOARD_SIZE; col++) {
        line.push(this.createTile(board[row * BOARD_SIZE + col] ?? 0, col, row));
      }
      this.tiles.push(line);
    }
    this.selectGraphics.clear();
  }

  /** 点选高亮（null 清除）。 */
  setSelected(cell: Cell | null) {
    const g = this.selectGraphics;
    g.clear();
    if (!cell) {
      return;
    }
    const p = cellToLocal(cell.col, cell.row);
    g.lineWidth = 5;
    g.strokeColor = SELECT_COLOR;
    g.roundRect(p.x - BOARD_CELL / 2 + 3, p.y - BOARD_CELL / 2 + 3, BOARD_CELL - 6, BOARD_CELL - 6, 10);
    g.stroke();
  }

  /**
   * 乐观交换预览：仅移动两个节点的视觉位置，不改簿记——任何服务端帧
   * 到达时都可整体作废（snapHome），不影响波次差分重放的坐标系。
   */
  previewSwap(a: Cell, b: Cell): Promise<void> {
    const ta = this.tiles[a.row][a.col];
    const tb = this.tiles[b.row][b.col];
    if (!ta || !tb) {
      return Promise.resolve();
    }
    return Promise.all([
      tweenTo(ta.node, cellToLocal(b.col, b.row), SWAP_SECONDS),
      tweenTo(tb.node, cellToLocal(a.col, a.row), SWAP_SECONDS),
    ]).then(() => undefined);
  }

  /** 交换簿记（无动画）：乐观预览被结果帧认领时提交。 */
  confirmSwap(a: Cell, b: Cell) {
    const ta = this.tiles[a.row][a.col];
    const tb = this.tiles[b.row][b.col];
    if (!ta || !tb) {
      return;
    }
    this.tiles[a.row][a.col] = tb;
    this.tiles[b.row][b.col] = ta;
  }

  /** 簿记交换 + 双向 tween 动画：结果帧重放的交换路径。 */
  async animateSwap(a: Cell, b: Cell): Promise<void> {
    this.confirmSwap(a, b);
    const ta = this.tiles[a.row][a.col];
    const tb = this.tiles[b.row][b.col];
    if (!ta || !tb) {
      return;
    }
    await Promise.all([
      tweenTo(ta.node, cellToLocal(b.col, b.row), SWAP_SECONDS),
      tweenTo(tb.node, cellToLocal(a.col, a.row), SWAP_SECONDS),
    ]);
  }

  /** 取消一切在途预览：全部棋子瞬间归位到簿记位置。 */
  snapHome() {
    this.eachTile((t, col, row) => {
      Tween.stopAllByTarget(t.node);
      if (t.node.isValid) {
        t.node.setPosition(cellToLocal(col, row));
      }
    });
  }

  /** 全部棋子平滑归位到簿记位置（否决回弹动画）。 */
  async tweenHome(seconds: number): Promise<void> {
    const moves: Promise<void>[] = [];
    this.eachTile((t, col, row) => {
      Tween.stopAllByTarget(t.node);
      if (t.node.isValid) {
        moves.push(tweenTo(t.node, cellToLocal(col, row), seconds));
      }
    });
    await Promise.all(moves);
  }

  /**
   * 重放一波单消：clear 为本波消除格（本波结算前坐标系），board 为本波
   * 落定后棋盘。棋子缩放消失 → 逐列差分（存活棋子沉底 + 顶部补充落下）。
   * stale 返回 true 时提前中止（回合边界硬同步已重建棋盘）。
   */
  async replayWave(clear: Cell[], board: number[], stale?: () => boolean): Promise<void> {
    const cleared = new Set(clear.map((p) => p.row * BOARD_SIZE + p.col));
    const fades: Promise<void>[] = [];
    for (const p of clear) {
      const t = this.tiles[p.row][p.col];
      if (!t) {
        continue;
      }
      this.tiles[p.row][p.col] = null;
      fades.push(scaleOut(t.node, CLEAR_SECONDS));
    }
    await Promise.all(fades);
    if (stale?.()) {
      return;
    }

    const moves: Promise<void>[] = [];
    for (let col = 0; col < BOARD_SIZE; col++) {
      const survivors: TileView[] = [];
      for (let row = 0; row < BOARD_SIZE; row++) {
        const t = this.tiles[row][col];
        if (t && !cleared.has(row * BOARD_SIZE + col)) {
          survivors.push(t);
        }
        this.tiles[row][col] = null;
      }
      const refills = BOARD_SIZE - survivors.length;
      // 存活棋子保持相对顺序沉底。
      survivors.forEach((t, i) => {
        const toRow = refills + i;
        this.tiles[toRow][col] = t;
        moves.push(tweenTo(t.node, cellToLocal(col, toRow), FALL_SECONDS));
      });
      // 补充棋子自列顶上方依次落下（虚拟行 row - refills 起步）。
      for (let row = refills - 1; row >= 0; row--) {
        const t = this.createTile(board[row * BOARD_SIZE + col] ?? 0, col, row - refills);
        this.tiles[row][col] = t;
        moves.push(tweenTo(t.node, cellToLocal(col, row), FALL_SECONDS));
      }
    }
    await Promise.all(moves);
  }

  /** 死局重排：整板淡出重建再淡入。 */
  async crossfade(board: number[]): Promise<void> {
    let op = this.tileRoot.getComponent(UIOpacity);
    if (!op) {
      op = this.tileRoot.addComponent(UIOpacity);
    }
    await opacityTo(op, 0, RESHUFFLE_SECONDS);
    this.build(board);
    await opacityTo(op, 255, RESHUFFLE_SECONDS);
  }

  private eachTile(fn: (t: TileView, col: number, row: number) => void) {
    for (let row = 0; row < BOARD_SIZE; row++) {
      for (let col = 0; col < BOARD_SIZE; col++) {
        const t = this.tiles[row][col];
        if (t) {
          fn(t, col, row);
        }
      }
    }
  }

  private createTile(color: number, col: number, row: number): TileView {
    const node = new Node(`tile-${col}-${row}`);
    node.layer = this.tileRoot.layer;
    node.parent = this.tileRoot;
    node.setPosition(cellToLocal(col, row));
    node.addComponent(UITransform).setContentSize(BOARD_CELL, BOARD_CELL);
    const frame = this.frames ? this.frames[fruitIndexForColor(color)] ?? null : null;
    if (frame) {
      const sp = node.addComponent(Sprite);
      sp.type = Sprite.Type.SIMPLE;
      sp.sizeMode = Sprite.SizeMode.CUSTOM;
      sp.spriteFrame = frame;
    } else {
      const g = node.addComponent(Graphics);
      g.fillColor = FALLBACK_COLORS[color] ?? FALLBACK_COLORS[0];
      g.roundRect(-BOARD_CELL / 2, -BOARD_CELL / 2, BOARD_CELL, BOARD_CELL, 10);
      g.fill();
    }
    return { node, color };
  }

  private drawSlots() {
    const g = this.slotGraphics;
    g.fillColor = SLOT_COLOR;
    for (let row = 0; row < BOARD_SIZE; row++) {
      for (let col = 0; col < BOARD_SIZE; col++) {
        const p = cellToLocal(col, row);
        g.roundRect(p.x - BOARD_CELL / 2, p.y - BOARD_CELL / 2, BOARD_CELL, BOARD_CELL, 10);
        g.fill();
      }
    }
  }
}
