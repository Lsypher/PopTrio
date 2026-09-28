package room

import (
	"time"

	"poptrio/server/internal/judge"
)

// FrameKind 标识事件帧类别。
type FrameKind string

const (
	KindMatchStarted      FrameKind = "match_started"      // 对局开始：先手 + 初始快照（即回合 1 快照）
	KindTurnStarted       FrameKind = "turn_started"       // 回合开始：全量快照
	KindSwapResult        FrameKind = "swap_result"        // 有效交换：连锁波次 + 最终棋盘
	KindSwapRejected      FrameKind = "swap_rejected"      // 无效/越权交换：回弹或拒绝
	KindReshuffle         FrameKind = "reshuffle"          // 死局重排：棋盘更新，分数与回合不变
	KindSettlement        FrameKind = "settlement"         // 终局结算：胜/负/Draw
	KindReconnected       FrameKind = "reconnected"        // 重连成功：全量快照恢复对局
	KindReconnectRejected FrameKind = "reconnect_rejected" // 重连被拒：token 校验失败
)

// Frame 是房间向双玩家广播的事件帧（ADR-0003：快照帧 + 结果帧）。
// 同一帧内容对所有接收者一致；客户端按 Kind 自行解释。
type Frame interface {
	Kind() FrameKind
}

// Snapshot 是全量快照：MatchStarted 与 TurnStarted 携带（ADR-0003），
// 重连恢复同样以此为准（issue 04）。
type Snapshot struct {
	Board    []judge.Tile // 行优先，长度 = Width*Height；切片共享但此后不可变
	Scores   [2]int       // 双方累计得分，按座位序
	Turn     int          // 回合序号，1-based
	Operator int          // 当前操作方座位（0/1）
	Deadline time.Time    // 当前操作窗口截止时间戳（服务端时钟为唯一权威）
}

// MatchStartedFrame 宣告对局开始：座位表、先手与回合 1 全量快照。
type MatchStartedFrame struct {
	Seats     [2]string // 座位 0/1 的玩家标识
	FirstSeat int       // 先手座位（0/1，先手随机）
	Snapshot            // 内嵌回合 1 快照
}

func (MatchStartedFrame) Kind() FrameKind { return KindMatchStarted }

// TurnStartedFrame 宣告回合流转：新回合的全量快照。
type TurnStartedFrame struct {
	Snapshot
}

func (TurnStartedFrame) Kind() FrameKind { return KindTurnStarted }

// WaveView 是一波单消的结果视图。Board 为本波落定（下落 + 补充）后的棋盘：
// 首波与快照棋盘、后续波与上一波棋盘做差分，即得下落移动与补充棋子——
// 前端纯重放，零计算、零反推（ADR-0003）。
type WaveView struct {
	Clear []judge.Pos  // 本波消除的格子（去重、行优先）
	Score int          // 本波得分（线性：N 格 = N 分）
	Board []judge.Tile // 本波结算后的棋盘（行优先）
}

// SwapResultFrame 是一次有效交换的结果帧（ADR-0003：交换结果帧）。
type SwapResultFrame struct {
	Operator int          // 得分归属方座位（Attribution：全部波次归操作者）
	Waves    []WaveView   // 按结算顺序排列的全部连锁波次
	Board    []judge.Tile // 全部波次结算后的最终棋盘
	Scores   [2]int       // 结算后双方累计得分
}

func (SwapResultFrame) Kind() FrameKind { return KindSwapResult }

// RejectReason 是交换被拒的原因。
type RejectReason string

const (
	RejectNotOperator RejectReason = "not_operator" // 非当前操作方（含窗口截止后迟到请求）
	RejectInvalidSwap RejectReason = "invalid_swap" // 几何非法：越界 / 不相邻 / 同格
	RejectNoClear     RejectReason = "no_clear"     // 交换不成线：回弹，无惩罚
)

// SwapRejectedFrame 是被拒交换的回执。无效交换不改变棋盘、不结束回合。
type SwapRejectedFrame struct {
	Seat   int        // 发起方座位
	Swap   judge.Swap // 原样回显
	Reason RejectReason
}

func (SwapRejectedFrame) Kind() FrameKind { return KindSwapRejected }

// ReshuffleFrame 是死局重排广播：棋盘全量更新，分数与剩余回合不变。
type ReshuffleFrame struct {
	Board  []judge.Tile
	Scores [2]int // 不变，随帧携带便于客户端对账
	Turn   int    // 不变
}

func (ReshuffleFrame) Kind() FrameKind { return KindReshuffle }

// SettlementFrame 是终局结算：胜/负/Draw 附双方最终得分，仅通知，无奖励。
type SettlementFrame struct {
	Draw   bool
	Winner int    // 胜方座位；Draw 时为 -1
	Scores [2]int // 双方最终得分
}

func (SettlementFrame) Kind() FrameKind { return KindSettlement }

// ReconnectedFrame 是重连成功回执：凭对局 token 校验通过后下发全量快照，
// 客户端据此恢复棋盘、得分、回合与倒计时（ADR-0003：重连 = 拉一次快照）。
type ReconnectedFrame struct {
	Seat int // 重连方座位
	Snapshot
}

func (ReconnectedFrame) Kind() FrameKind { return KindReconnected }

// ReconnectRejectReason 是重连被拒的原因。
type ReconnectRejectReason string

const (
	ReconnectBadToken ReconnectRejectReason = "bad_token" // 对局 token 不匹配
)

// ReconnectRejectedFrame 是被拒重连的回执。掉线状态保留：宽限期照常计时。
type ReconnectRejectedFrame struct {
	Seat   int
	Reason ReconnectRejectReason
}

func (ReconnectRejectedFrame) Kind() FrameKind { return KindReconnectRejected }
