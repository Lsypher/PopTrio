// Package protocol 实现 PopTrio 线上协议（ADR-0003）：JSON over WebSocket，
// 每条消息为带版本字段的信封 {"v":1,"type":"...","payload":{...}}。
// 全部编解码集中在该包：其余代码只依赖 Parse / Encode / FromRoom 与消息
// 常量，未来替换为二进制编解码时改写本包即可，调用方零改动。
package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"poptrio/server/internal/judge"
	"poptrio/server/internal/room"
)

// Version 是当前协议版本，随每条消息的 v 字段携带。
const Version = 1

// 入站消息类型（客户端 → 服务端）。
const (
	TypeQueueJoin   = "queue_join"
	TypeQueueCancel = "queue_cancel"
	TypeSwap        = "swap"
	TypeReconnect   = "reconnect" // 凭对局 token 重新接入既有对局座位
)

// 出站消息类型（服务端 → 客户端）。
const (
	TypeQueueJoined       = "queue_joined"
	TypeQueueCancelled    = "queue_cancelled"
	TypeQueueTimeout      = "queue_timeout"
	TypeError             = "error"
	TypeMatchToken        = "match_token" // 对局重连凭据：每座位私有下发，绝不广播
	TypeMatchStarted      = "match_started"
	TypeTurnStarted       = "turn_started"
	TypeSwapResult        = "swap_result"
	TypeSwapRejected      = "swap_rejected"
	TypeReshuffle         = "reshuffle"
	TypeSettlement        = "settlement"
	TypeReconnected       = "reconnected"
	TypeReconnectRejected = "reconnect_rejected"
)

// 错误帧错误码。
const (
	CodeBadJSON     = "bad_json"     // 消息不是合法 JSON
	CodeBadVersion  = "bad_version"  // v 缺失或不为 1
	CodeUnknownType = "unknown_type" // type 无法识别
	CodeBadPayload  = "bad_payload"  // payload 与 type 不匹配
	CodeBadState    = "bad_state"    // 消息在当前连接状态下不合法
	CodeBadToken    = "bad_token"    // 对局 token 无法匹配到可重连的座位
	CodeInternal    = "internal"     // 服务端内部错误
)

// Parse 解析失败的原因，按 errors.Is 区分。
var (
	ErrBadJSON     = errors.New("protocol: message is not valid json")
	ErrBadVersion  = errors.New("protocol: unsupported protocol version")
	ErrUnknownType = errors.New("protocol: unknown message type")
	ErrBadPayload  = errors.New("protocol: malformed payload")
)

// Inbound 是解析后的客户端消息。Swap 仅在 Type == TypeSwap 时有效，
// Token 仅在 Type == TypeReconnect 时有效。
type Inbound struct {
	Type  string
	Swap  Swap
	Token string
}

// Swap 是交换请求载荷：相邻两格 A、B 互换。
type Swap struct {
	A Pos `json:"a"`
	B Pos `json:"b"`
}

// Pos 是格子坐标，原点在左上角。
type Pos struct {
	Col int `json:"col"`
	Row int `json:"row"`
}

// Envelope 是线上消息信封。
type Envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Parse 把一条线上消息解析为 Inbound。JSON 非法、版本不符、类型未知、
// 载荷畸形分别返回 ErrBadJSON / ErrBadVersion / ErrUnknownType / ErrBadPayload。
// 坐标是否越界等游戏规则不在此校验，交由房间以 swap_rejected 回绝。
func Parse(data []byte) (Inbound, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Inbound{}, ErrBadJSON
	}
	if env.V != Version {
		return Inbound{}, fmt.Errorf("%w: v=%d want %d", ErrBadVersion, env.V, Version)
	}
	switch env.Type {
	case TypeQueueJoin, TypeQueueCancel:
		return Inbound{Type: env.Type}, nil
	case TypeReconnect:
		var wire struct {
			Token *string `json:"token"`
		}
		if len(env.Payload) == 0 || json.Unmarshal(env.Payload, &wire) != nil || wire.Token == nil || *wire.Token == "" {
			return Inbound{}, ErrBadPayload
		}
		return Inbound{Type: TypeReconnect, Token: *wire.Token}, nil
	case TypeSwap:
		var wire struct {
			A *Pos `json:"a"`
			B *Pos `json:"b"`
		}
		if len(env.Payload) == 0 || json.Unmarshal(env.Payload, &wire) != nil || wire.A == nil || wire.B == nil {
			return Inbound{}, ErrBadPayload
		}
		return Inbound{Type: TypeSwap, Swap: Swap{A: *wire.A, B: *wire.B}}, nil
	default:
		return Inbound{}, fmt.Errorf("%w: %q", ErrUnknownType, env.Type)
	}
}

// Encode 把一条出站消息编码为线上字节：信封 v=1 + 类型 + 载荷。
// payload 为 nil 或空结构体时 payload 字段省略。
func Encode(typ string, payload any) ([]byte, error) {
	env := Envelope{V: Version, Type: typ}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("protocol: encode %s payload: %w", typ, err)
		}
		env.Payload = raw
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("protocol: encode %s envelope: %w", typ, err)
	}
	return data, nil
}

// ---- 出站消息载荷 ----

// ErrorMsg 是错误帧载荷：code 供客户端程序化分派，message 供人读。
type ErrorMsg struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Snapshot 是全量快照（ADR-0003）：回合开始与重连恢复的唯一依据。
type Snapshot struct {
	Board    []int     `json:"board"`    // 行优先颜色 ID，长度 = 宽*高
	Scores   [2]int    `json:"scores"`   // 双方累计得分，按座位序
	Turn     int       `json:"turn"`     // 回合序号，1-based
	Operator int       `json:"operator"` // 当前操作方座位（0/1）
	Deadline time.Time `json:"deadline"` // 操作窗口截止（服务端时钟权威）
}

// MatchStartedMsg 宣告对局开始。
type MatchStartedMsg struct {
	Seats     [2]string `json:"seats"`
	FirstSeat int       `json:"first_seat"`
	Snapshot  Snapshot  `json:"snapshot"`
}

// TurnStartedMsg 宣告回合流转。
type TurnStartedMsg struct {
	Snapshot Snapshot `json:"snapshot"`
}

// Wave 是一波单消结果视图：Board 为本波落定后棋盘，前端与上一波差分
// 重放下落与补充，零计算。
type Wave struct {
	Clear []Pos `json:"clear"`
	Score int   `json:"score"`
	Board []int `json:"board"`
}

// SwapResultMsg 是一次有效交换的结果。
type SwapResultMsg struct {
	Operator int    `json:"operator"`
	Swap     Swap   `json:"swap"` // 被接受的交换回显：客户端据此重放交换动画（双方一致，零反推）
	Waves    []Wave `json:"waves"`
	Board    []int  `json:"board"`
	Scores   [2]int `json:"scores"`
}

// SwapRejectedMsg 是被拒交换的回执。
type SwapRejectedMsg struct {
	Seat   int    `json:"seat"`
	Swap   Swap   `json:"swap"`
	Reason string `json:"reason"`
}

// ReshuffleMsg 是死局重排广播。
type ReshuffleMsg struct {
	Board  []int  `json:"board"`
	Scores [2]int `json:"scores"`
	Turn   int    `json:"turn"`
}

// SettlementMsg 是终局结算。Winner 在平局时为 -1。
type SettlementMsg struct {
	Draw   bool   `json:"draw"`
	Winner int    `json:"winner"`
	Scores [2]int `json:"scores"`
}

// MatchTokenMsg 是对局重连凭据，经各自连接私发（不随广播帧下发）。
// Seat 是接收方座位：客户端据此区分己方/对方，禁用非本方回合的输入。
type MatchTokenMsg struct {
	Token string `json:"token"`
	Seat  int    `json:"seat"`
}

// ReconnectedMsg 是重连成功回执：全量快照恢复对局（ADR-0003）。
type ReconnectedMsg struct {
	Seat     int      `json:"seat"`
	Snapshot Snapshot `json:"snapshot"`
}

// ReconnectRejectedMsg 是被拒重连的回执（token 不匹配）。
type ReconnectRejectedMsg struct {
	Seat   int    `json:"seat"`
	Reason string `json:"reason"`
}

// FromRoom 把房间事件帧映射为出站消息（类型 + 载荷）。room.Frame 的词汇表
// 与线上消息一一对应；未知帧（理论不可达）映射为 internal 错误帧。
func FromRoom(f room.Frame) (string, any) {
	switch v := f.(type) {
	case room.MatchStartedFrame:
		return TypeMatchStarted, MatchStartedMsg{
			Seats:     v.Seats,
			FirstSeat: v.FirstSeat,
			Snapshot:  snapshotMsg(v.Snapshot),
		}
	case room.TurnStartedFrame:
		return TypeTurnStarted, TurnStartedMsg{Snapshot: snapshotMsg(v.Snapshot)}
	case room.SwapResultFrame:
		waves := make([]Wave, len(v.Waves))
		for i, w := range v.Waves {
			waves[i] = Wave{Clear: posMsgs(w.Clear), Score: w.Score, Board: tileMsgs(w.Board)}
		}
		return TypeSwapResult, SwapResultMsg{
			Operator: v.Operator,
			Swap:     Swap{A: Pos{Col: v.Swap.A.Col, Row: v.Swap.A.Row}, B: Pos{Col: v.Swap.B.Col, Row: v.Swap.B.Row}},
			Waves:    waves,
			Board:    tileMsgs(v.Board),
			Scores:   v.Scores,
		}
	case room.SwapRejectedFrame:
		return TypeSwapRejected, SwapRejectedMsg{
			Seat:   v.Seat,
			Swap:   Swap{A: Pos{Col: v.Swap.A.Col, Row: v.Swap.A.Row}, B: Pos{Col: v.Swap.B.Col, Row: v.Swap.B.Row}},
			Reason: string(v.Reason),
		}
	case room.ReshuffleFrame:
		return TypeReshuffle, ReshuffleMsg{Board: tileMsgs(v.Board), Scores: v.Scores, Turn: v.Turn}
	case room.SettlementFrame:
		return TypeSettlement, SettlementMsg{Draw: v.Draw, Winner: v.Winner, Scores: v.Scores}
	case room.ReconnectedFrame:
		return TypeReconnected, ReconnectedMsg{Seat: v.Seat, Snapshot: snapshotMsg(v.Snapshot)}
	case room.ReconnectRejectedFrame:
		return TypeReconnectRejected, ReconnectRejectedMsg{Seat: v.Seat, Reason: string(v.Reason)}
	default:
		return TypeError, ErrorMsg{Code: CodeInternal, Message: fmt.Sprintf("unknown frame %v", f.Kind())}
	}
}

func snapshotMsg(s room.Snapshot) Snapshot {
	return Snapshot{
		Board:    tileMsgs(s.Board),
		Scores:   s.Scores,
		Turn:     s.Turn,
		Operator: s.Operator,
		Deadline: s.Deadline,
	}
}

func tileMsgs(tiles []judge.Tile) []int {
	out := make([]int, len(tiles))
	for i, t := range tiles {
		out[i] = int(t)
	}
	return out
}

func posMsgs(ps []judge.Pos) []Pos {
	out := make([]Pos, len(ps))
	for i, p := range ps {
		out[i] = Pos{Col: p.Col, Row: p.Row}
	}
	return out
}
