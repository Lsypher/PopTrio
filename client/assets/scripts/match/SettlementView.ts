// 结算画面（issue 08）：终局全屏遮罩——胜/负/Draw 判定（本方视角）、双方
// 终分、再战（重新进入 Queue）与返回大厅入口。纯表现组件：结果与分数全部
// 来自服务端 settlement 帧，由 MatchUI 在收到该帧时唤起（ADR-0001）。
import {
  BlockInputEvents,
  Button,
  Color,
  Graphics,
  Label,
  Node,
  UITransform,
} from 'cc';
import { SettlementPayload } from '../protocol/Protocol';

const COLOR_WIN = new Color(255, 214, 90, 255);
const COLOR_LOSE = new Color(170, 170, 178, 255);
const COLOR_DRAW = new Color(235, 235, 235, 255);
const COLOR_TEXT = new Color(235, 235, 235, 255);
const COLOR_MUTED = new Color(150, 150, 155, 255);
const COLOR_BTN = new Color(0, 144, 255, 255);
const COLOR_BTN_TEXT = new Color(255, 255, 255, 255);

export interface SettlementHooks {
  onRematch: () => void;
  onLobby: () => void;
}

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

function makeButton(parent: Node, name: string, text: string, onClick: () => void): Node {
  const width = 280;
  const height = 104;
  const node = new Node(name);
  node.layer = parent.layer;
  node.parent = parent;
  node.addComponent(UITransform).setContentSize(width, height);
  const g = node.addComponent(Graphics);
  g.fillColor = COLOR_BTN;
  g.roundRect(-width / 2, -height / 2, width, height, height / 2);
  g.fill();
  makeLabel(node, 'Label', text, 36, COLOR_BTN_TEXT);
  const button = node.addComponent(Button);
  button.target = node;
  button.transition = Button.Transition.NONE;
  node.on(Button.EventType.CLICK, onClick);
  return node;
}

export class SettlementView {
  private root: Node;
  private titleLabel: Label;
  private scoreLabel: Label;

  constructor(parent: Node, hooks: SettlementHooks) {
    this.root = new Node('Settlement');
    this.root.layer = parent.layer;
    this.root.parent = parent;
    // 全屏遮罩：吞掉触摸，防止结算后仍操作棋盘。
    this.root.addComponent(UITransform);
    this.root.addComponent(BlockInputEvents);
    const dim = this.root.addComponent(Graphics);
    dim.fillColor = new Color(0, 0, 0, 170);
    dim.roundRect(-2400, -2400, 4800, 4800, 0);
    dim.fill();

    this.titleLabel = makeLabel(this.root, 'Outcome', '', 96, COLOR_DRAW);
    this.titleLabel.node.setPosition(0, 200);
    this.scoreLabel = makeLabel(this.root, 'Scores', '', 52, COLOR_TEXT);
    this.scoreLabel.node.setPosition(0, 60);
    makeLabel(this.root, 'Hint', '双方终分', 30, COLOR_MUTED).node.setPosition(0, -16);

    const rematch = makeButton(this.root, 'Rematch', '再战', hooks.onRematch);
    rematch.setPosition(-170, -180);
    const lobby = makeButton(this.root, 'Lobby', '返回大厅', hooks.onLobby);
    lobby.setPosition(170, -180);

    this.root.active = false;
  }

  /** 展示结算：结果与双方终分按本方座位呈现（座位未知时退化为中立视角）。 */
  show(p: SettlementPayload, mySeat: number | null) {
    let title: string;
    let color: Color;
    if (p.draw) {
      title = '平局';
      color = COLOR_DRAW;
    } else if (mySeat === null) {
      title = `P${p.winner} 胜`;
      color = COLOR_DRAW;
    } else if (p.winner === mySeat) {
      title = '胜利';
      color = COLOR_WIN;
    } else {
      title = '失败';
      color = COLOR_LOSE;
    }
    this.titleLabel.string = title;
    this.titleLabel.color = color;
    this.scoreLabel.string =
      mySeat === 1 ? `你 ${p.scores[1]} : ${p.scores[0]} 对手` : `你 ${p.scores[0]} : ${p.scores[1]} 对手`;
    this.root.active = true;
  }
}
