// 结算画面（issue 08）：终局全屏遮罩——胜/负/Draw 判定（本方视角）、双方
// 终分、再战（重新进入 Queue）与返回大厅入口。UI 骨架为 SettlementView.prefab
// （ADR-0005），本组件只驱动行为：结果与分数全部来自服务端 settlement 帧，
// 由 MatchUI 在收到该帧时唤起（ADR-0001）。
import {
  Button,
  Color,
  Component,
  Label,
  _decorator,
} from 'cc';
import { SettlementPayload } from '../protocol/Protocol';

const { ccclass, property } = _decorator;

const COLOR_WIN = new Color(255, 214, 90, 255);
const COLOR_LOSE = new Color(170, 170, 178, 255);
const COLOR_DRAW = new Color(235, 235, 235, 255);

export interface SettlementHooks {
  onRematch: () => void;
  onLobby: () => void;
}

@ccclass('SettlementView')
export class SettlementView extends Component {
  @property(Label)
  private outcomeLabel: Label | null = null;

  @property(Label)
  private scoresLabel: Label | null = null;

  @property(Button)
  private rematchButton: Button | null = null;

  @property(Button)
  private lobbyButton: Button | null = null;

  /** 出口回调由 MatchUI 注入：场景跳转策略归对局所有，表现组件不持有。 */
  hooks: SettlementHooks | null = null;

  onLoad() {
    this.rematchButton?.node.on(Button.EventType.CLICK, this.emitRematch);
    this.lobbyButton?.node.on(Button.EventType.CLICK, this.emitLobby);
  }

  onDestroy() {
    this.rematchButton?.node.off(Button.EventType.CLICK, this.emitRematch);
    this.lobbyButton?.node.off(Button.EventType.CLICK, this.emitLobby);
  }

  private emitRematch = () => {
    this.hooks?.onRematch();
  };

  private emitLobby = () => {
    this.hooks?.onLobby();
  };

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
    if (this.outcomeLabel) {
      this.outcomeLabel.string = title;
      this.outcomeLabel.color = color;
    }
    if (this.scoresLabel) {
      this.scoresLabel.string =
        mySeat === 1 ? `你 ${p.scores[1]} : ${p.scores[0]} 对手` : `你 ${p.scores[0]} : ${p.scores[1]} 对手`;
    }
    this.node.active = true;
  }
}
