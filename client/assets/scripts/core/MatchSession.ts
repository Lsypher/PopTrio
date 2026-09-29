// 跨场景对局会话状态：模块单例随场景切换存活。大厅收到 match_started 后
// 存快照并切场景，对局场景从这里取初始快照渲染；回合内的快照更新也在此
// 沉淀，供后进场景读取。
import {
  MatchStartedPayload,
  MatchTokenPayload,
  ReconnectedPayload,
  ServerMessage,
  SettlementPayload,
  Snapshot,
  TurnStartedPayload,
  MSG_MATCH_STARTED,
  MSG_MATCH_TOKEN,
  MSG_RECONNECTED,
  MSG_SETTLEMENT,
  MSG_TURN_STARTED,
} from '../protocol/Protocol';

export interface MatchSessionState {
  /** 最近一次全量快照（match_started / turn_started / reconnected 更新）。 */
  snapshot: Snapshot | null;
  /** 对局重连凭据（match_token 私发，绝不广播）。 */
  token: string;
  /** 本连接的座位（0/1）：match_token / reconnected 私发帧沉淀。 */
  mySeat: number | null;
  /** 已进入对局（match_started 之后为 true）。 */
  active: boolean;
  /** 终局结果（settlement 到达后非空）。 */
  settlement: SettlementPayload | null;
  /** 再战入口：结算画面点「再战」后置位，大厅场景载入即自动重新排队。 */
  autoQueue: boolean;
}

export const matchSession: MatchSessionState = {
  snapshot: null,
  token: '',
  mySeat: null,
  active: false,
  settlement: null,
  autoQueue: false,
};

/** 中心分发：把会话级消息沉淀到单例。场景 UI 另行订阅做界面响应。 */
export function applySessionMessage(msg: ServerMessage): void {
  if (msg.type === MSG_MATCH_TOKEN) {
    const p = msg.payload as MatchTokenPayload;
    matchSession.token = p.token;
    matchSession.mySeat = p.seat;
  } else if (msg.type === MSG_MATCH_STARTED) {
    const p = msg.payload as MatchStartedPayload;
    matchSession.snapshot = p.snapshot;
    matchSession.active = true;
    matchSession.settlement = null;
  } else if (msg.type === MSG_TURN_STARTED) {
    matchSession.snapshot = (msg.payload as TurnStartedPayload).snapshot;
  } else if (msg.type === MSG_SETTLEMENT) {
    matchSession.settlement = msg.payload as SettlementPayload;
  } else if (msg.type === MSG_RECONNECTED) {
    const p = msg.payload as ReconnectedPayload;
    matchSession.snapshot = p.snapshot;
    // reconnected 由房间广播（重连方与对手都会收到）：座位信息在
    // match_token 私发帧已沉淀，这里仅在未知时兜底，防止对手帧覆盖己方座位。
    if (matchSession.mySeat === null) {
      matchSession.mySeat = p.seat;
    }
  }
}
