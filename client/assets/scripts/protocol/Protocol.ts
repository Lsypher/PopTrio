// PopTrio 线上协议（ADR-0003）：JSON over WebSocket，每条消息为带版本
// 字段的信封 {"v":1,"type":"...","payload":{...}}。全部编解码集中在该模块：
// 其余代码只依赖这里的常量、类型与 parse/encode 函数，未来替换编解码
// 方式时改写本文件即可，调用方零改动。与服务端 internal/protocol 一一对齐。
export const PROTOCOL_VERSION = 1;

// ---- 入站消息类型（客户端 → 服务端）----
export const MSG_QUEUE_JOIN = 'queue_join';
export const MSG_QUEUE_CANCEL = 'queue_cancel';
export const MSG_SWAP = 'swap';
export const MSG_RECONNECT = 'reconnect';

// ---- 出站消息类型（服务端 → 客户端）----
export const MSG_QUEUE_JOINED = 'queue_joined';
export const MSG_QUEUE_CANCELLED = 'queue_cancelled';
export const MSG_QUEUE_TIMEOUT = 'queue_timeout';
export const MSG_ERROR = 'error';
export const MSG_MATCH_TOKEN = 'match_token';
export const MSG_MATCH_STARTED = 'match_started';
export const MSG_TURN_STARTED = 'turn_started';
export const MSG_SWAP_RESULT = 'swap_result';
export const MSG_SWAP_REJECTED = 'swap_rejected';
export const MSG_RESHUFFLE = 'reshuffle';
export const MSG_SETTLEMENT = 'settlement';
export const MSG_RECONNECTED = 'reconnected';
export const MSG_RECONNECT_REJECTED = 'reconnect_rejected';

// ---- 错误帧错误码 ----
export const CODE_BAD_JSON = 'bad_json';
export const CODE_BAD_VERSION = 'bad_version';
export const CODE_UNKNOWN_TYPE = 'unknown_type';
export const CODE_BAD_PAYLOAD = 'bad_payload';
export const CODE_BAD_STATE = 'bad_state';
export const CODE_BAD_TOKEN = 'bad_token';
export const CODE_INTERNAL = 'internal';

// ---- 载荷类型 ----

/** 格子坐标，原点在左上角。 */
export interface Pos {
  col: number;
  row: number;
}

export interface SwapPayload {
  a: Pos;
  b: Pos;
}

/** 错误帧：code 供程序化分派，message 供人读。 */
export interface ErrorPayload {
  code: string;
  message: string;
}

/** 全量快照：回合开始与重连恢复的唯一依据。 */
export interface Snapshot {
  /** 行优先颜色 ID，长度 = 宽*高 */
  board: number[];
  /** 双方累计得分，按座位序 */
  scores: [number, number];
  /** 回合序号，1-based */
  turn: number;
  /** 当前操作方座位（0/1） */
  operator: number;
  /** 操作窗口截止（服务端时钟权威，RFC3339） */
  deadline: string;
}

export interface MatchTokenPayload {
  token: string;
  /** 接收方座位（0/1）：私有帧附带，客户端据此区分己方回合。 */
  seat: number;
}

export interface MatchStartedPayload {
  seats: [string, string];
  first_seat: number;
  snapshot: Snapshot;
}

export interface TurnStartedPayload {
  snapshot: Snapshot;
}

/** 一波单消结果视图：board 为本波落定后棋盘，客户端差分重放，零计算。 */
export interface Wave {
  clear: Pos[];
  score: number;
  board: number[];
}

export interface SwapResultPayload {
  operator: number;
  /** 被接受的交换回显：双方据此重放交换动画，零反推（对手交换实时可见）。 */
  swap: SwapPayload;
  waves: Wave[];
  board: number[];
  scores: [number, number];
}

export interface SwapRejectedPayload {
  seat: number;
  swap: SwapPayload;
  reason: string;
}

export interface ReshufflePayload {
  board: number[];
  scores: [number, number];
  turn: number;
}

/** 终局结算。winner 在平局时为 -1。 */
export interface SettlementPayload {
  draw: boolean;
  winner: number;
  scores: [number, number];
}

export interface ReconnectedPayload {
  seat: number;
  snapshot: Snapshot;
}

export interface ReconnectRejectedPayload {
  seat: number;
  reason: string;
}

/** 无载荷消息（payload 字段缺省）。 */
export interface EmptyPayload { }

// ---- 服务端消息判别联合 ----

export type ServerMessage =
  | { type: typeof MSG_QUEUE_JOINED; payload?: EmptyPayload }
  | { type: typeof MSG_QUEUE_CANCELLED; payload?: EmptyPayload }
  | { type: typeof MSG_QUEUE_TIMEOUT; payload?: EmptyPayload }
  | { type: typeof MSG_ERROR; payload: ErrorPayload }
  | { type: typeof MSG_MATCH_TOKEN; payload: MatchTokenPayload }
  | { type: typeof MSG_MATCH_STARTED; payload: MatchStartedPayload }
  | { type: typeof MSG_TURN_STARTED; payload: TurnStartedPayload }
  | { type: typeof MSG_SWAP_RESULT; payload: SwapResultPayload }
  | { type: typeof MSG_SWAP_REJECTED; payload: SwapRejectedPayload }
  | { type: typeof MSG_RESHUFFLE; payload: ReshufflePayload }
  | { type: typeof MSG_SETTLEMENT; payload: SettlementPayload }
  | { type: typeof MSG_RECONNECTED; payload: ReconnectedPayload }
  | { type: typeof MSG_RECONNECT_REJECTED; payload: ReconnectRejectedPayload };

const KNOWN_TYPES: ReadonlySet<string> = new Set([
  MSG_QUEUE_JOINED, MSG_QUEUE_CANCELLED, MSG_QUEUE_TIMEOUT, MSG_ERROR,
  MSG_MATCH_TOKEN, MSG_MATCH_STARTED, MSG_TURN_STARTED, MSG_SWAP_RESULT,
  MSG_SWAP_REJECTED, MSG_RESHUFFLE, MSG_SETTLEMENT, MSG_RECONNECTED,
  MSG_RECONNECT_REJECTED,
]);

export class ProtocolError extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

/** 解析一条服务端消息。非法 JSON / 版本不符 / 类型未知分别抛 ProtocolError。 */
export function parseServerMessage(text: string): ServerMessage {
  let env: { v?: unknown; type?: unknown; payload?: unknown };
  try {
    env = JSON.parse(text);
  } catch {
    throw new ProtocolError(CODE_BAD_JSON, 'message is not valid json');
  }
  if (typeof env !== 'object' || env === null || env.v !== PROTOCOL_VERSION) {
    throw new ProtocolError(CODE_BAD_VERSION, `unsupported protocol version`);
  }
  if (typeof env.type !== 'string' || !KNOWN_TYPES.has(env.type)) {
    throw new ProtocolError(CODE_UNKNOWN_TYPE, `unknown message type`);
  }
  const payload = (env.payload === undefined ? {} : env.payload) as never;
  return { type: env.type, payload } as ServerMessage;
}

interface OutgoingEnvelope {
  v: number;
  type: string;
  payload?: unknown;
}

function encode(type: string, payload?: unknown): string {
  const env: OutgoingEnvelope = { v: PROTOCOL_VERSION, type };
  if (payload !== undefined) {
    env.payload = payload;
  }
  return JSON.stringify(env);
}

export function encodeQueueJoin(): string {
  return encode(MSG_QUEUE_JOIN);
}

export function encodeQueueCancel(): string {
  return encode(MSG_QUEUE_CANCEL);
}

export function encodeSwap(a: Pos, b: Pos): string {
  return encode(MSG_SWAP, { a, b });
}

export function encodeReconnect(token: string): string {
  return encode(MSG_RECONNECT, { token });
}
