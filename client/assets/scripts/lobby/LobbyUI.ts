// 大厅场景：开始匹配 / 取消匹配、排队等待与超时提示、匹配成功切换对局
// 场景。UI 由代码搭建（骨架期占位实现）。
import {
  Button,
  Color,
  Component,
  director,
  Graphics,
  Label,
  Node,
  UITransform,
  _decorator,
} from 'cc';
import { ensureStage } from '../core/Stage';
import { applySessionMessage } from '../core/MatchSession';
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

const { ccclass } = _decorator;

type LobbyState = 'idle' | 'connecting' | 'queued';

const COLOR_TEXT = new Color(235, 235, 235, 255);
const COLOR_MUTED = new Color(150, 150, 155, 255);
const COLOR_BTN = new Color(0, 144, 255, 255);
const COLOR_BTN_TEXT = new Color(255, 255, 255, 255);

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

function makeButton(parent: Node, name: string, text: string, width: number, height: number, onClick: () => void): Button {
  const node = new Node(name);
  node.layer = parent.layer;
  node.parent = parent;
  node.addComponent(UITransform).setContentSize(width, height);
  const g = node.addComponent(Graphics);
  g.fillColor = COLOR_BTN;
  g.roundRect(-width / 2, -height / 2, width, height, height / 2);
  g.fill();
  const label = makeLabel(node, 'Label', text, 36, COLOR_BTN_TEXT);
  label.node.setPosition(0, 0);
  const button = node.addComponent(Button);
  button.target = node;
  button.transition = Button.Transition.NONE;
  node.on(Button.EventType.CLICK, onClick);
  return button;
}

@ccclass('LobbyUI')
export class LobbyUI extends Component {
  private state: LobbyState = 'idle';
  private statusLabel: Label | null = null;
  private buttonLabel: Label | null = null;
  private unsubMessage: (() => void) | null = null;
  private unsubClose: (() => void) | null = null;

  onLoad() {
    const canvas = ensureStage();
    makeLabel(canvas, 'Title', 'PopTrio', 80, COLOR_TEXT).node.setPosition(0, 500);
    makeLabel(canvas, 'Subtitle', '三消 1v1 对战', 32, COLOR_MUTED).node.setPosition(0, 410);
    this.statusLabel = makeLabel(canvas, 'Status', '', 36, COLOR_TEXT);
    this.statusLabel.node.setPosition(0, 80);
    const button = makeButton(canvas, 'Action', '', 400, 120, this.onAction);
    button.node.setPosition(0, -100);
    this.buttonLabel = button.node.getChildByName('Label')!.getComponent(Label)!;
    this.enterIdle('点击「开始匹配」寻找对手');
    this.unsubMessage = net.onMessage(this.handleMessage);
    this.unsubClose = net.onClose(this.handleClose);
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
