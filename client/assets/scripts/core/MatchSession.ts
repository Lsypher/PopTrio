// 跨场景对局会话状态：模块单例随场景切换存活。大厅收到 match_started 后
// 存快照并切场景，对局场景从这里取初始快照渲染；回合内的快照更新也在此
// 沉淀，供后进场景读取。
import {
  MatchStartedPayload,
  MatchTokenPayload,
  ServerMessage,
  SettlementPayload,
  Snapshot,
  TurnStartedPayload,
  MSG_MATCH_STARTED,
  MSG_MATCH_TOKEN,
  MSG_SETTLEMENT,
  MSG_TURN_STARTED,
} from '../protocol/Protocol';

export interface MatchSessionState {
  /** 最近一次全量快照（match_started / turn_started / reconnected 更新）。 */
  snapshot: Snapshot | null;
  /** 对局重连凭据（match_token 私发，绝不广播）。 */
  token: string;
  /** 已进入对局（match_started 之后为 true）。 */
  active: boolean;
  /** 终局结果（settlement 到达后非空）。 */
  settlement: SettlementPayload | null;
}

export const matchSession: MatchSessionState = {
  snapshot: null,
  token: '',
  active: false,
  settlement: null,
};

/** 中心分发：把会话级消息沉淀到单例。场景 UI 另行订阅做界面响应。 */
export function applySessionMessage(msg: ServerMessage): void {
  if (msg.type === MSG_MATCH_TOKEN) {
    matchSession.token = (msg.payload as MatchTokenPayload).token;
  } else if (msg.type === MSG_MATCH_STARTED) {
    const p = msg.payload as MatchStartedPayload;
    matchSession.snapshot = p.snapshot;
    matchSession.active = true;
    matchSession.settlement = null;
  } else if (msg.type === MSG_TURN_STARTED) {
    matchSession.snapshot = (msg.payload as TurnStartedPayload).snapshot;
  } else if (msg.type === MSG_SETTLEMENT) {
    matchSession.settlement = msg.payload as SettlementPayload;
  }
}
