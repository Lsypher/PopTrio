package hub

import (
	"context"
	"errors"
	"sync"

	"github.com/coder/websocket"

	"poptrio/server/internal/judge"
	"poptrio/server/internal/matchmaker"
	"poptrio/server/internal/protocol"
	"poptrio/server/internal/room"
)

// sessionState 是连接的排队/对局状态。
type sessionState int

const (
	stateIdle   sessionState = iota // 未排队
	stateQueued                     // 排队中
	stateInRoom                     // 对局中
	stateClosed                     // 已断开（cleanup 之后）
)

// roomMatch 是 session 在房间内的身份：房间句柄 + 座位号。
type roomMatch struct {
	r    *room.Room
	seat int
}

// session 是一条 WS 连接的完整生命周期：主循环（状态机）+ 读泵 + 写泵。
// 与房间仅经 channel 通信（ADR-0004）：出站为 Sink 入出站缓冲，入站为
// SubmitSwap 直投房间命令 channel。
type session struct {
	hub   *Hub
	conn  *websocket.Conn
	id    string
	out   *outbox
	msgCh chan protocol.Inbound // 读泵产出，关闭即连接已断

	mu       sync.Mutex
	state    sessionState
	rm       *roomMatch
	finished bool // 终局标志：房间 Sink 置位（房间事件循环 goroutine），主循环读

	pairOut <-chan matchmaker.Outcome // 排队结果；未排队时为 nil（select 永久阻塞）
}

// run 是主循环：分派入站消息与排队结果，直至连接断开。
func (s *session) run() {
	go s.writeLoop()
	go s.readLoop()
	defer s.cleanup()
	for {
		select {
		case in, ok := <-s.msgCh:
			if !ok {
				return
			}
			s.dispatch(in)
		case o := <-s.pairOut:
			s.onOutcome(o)
		}
	}
}

// dispatch 按当前状态分派入站消息；状态外的消息一律回错误帧。
func (s *session) dispatch(in protocol.Inbound) {
	switch in.Type {
	case protocol.TypeQueueJoin:
		s.mu.Lock()
		idle := s.state == stateIdle
		if idle {
			s.state = stateQueued
		}
		s.mu.Unlock()
		if !idle {
			s.sendError(protocol.CodeBadState, "already queued or in match")
			return
		}
		s.pairOut = s.hub.mm.Join(s)
		s.send(protocol.TypeQueueJoined, struct{}{})

	case protocol.TypeQueueCancel:
		s.mu.Lock()
		queued := s.state == stateQueued
		s.mu.Unlock()
		if !queued {
			s.sendError(protocol.CodeBadState, "not in queue")
			return
		}
		// 取消结果经 pairOut 回流；若恰与配对竞争（waiter 已摘除），
		// 配对结果即将送达，此处静默。
		s.hub.mm.Cancel(s)

	case protocol.TypeSwap:
		s.mu.Lock()
		rm := s.rm
		finished := s.finished
		s.mu.Unlock()
		// 终局后 room.Run 已返回、cmds 不再被消费（issue 03 交接）：
		// 停止向房间投递，回错误帧，避免结算瞬间连发消息卡死读泵。
		// 终局瞬间的 TOCTOU 残余投递有界无害：msgs 入站缓冲 16 + 每回合
		// 至多一条截止命令，远小于 room cmds 缓冲 64，不会阻塞投递方。
		if rm == nil || finished {
			s.sendError(protocol.CodeBadState, "no active match")
			return
		}
		rm.r.SubmitSwap(rm.seat, judge.Swap{
			A: judge.Pos{Col: in.Swap.A.Col, Row: in.Swap.A.Row},
			B: judge.Pos{Col: in.Swap.B.Col, Row: in.Swap.B.Row},
		})

	case protocol.TypeReconnect:
		s.hub.reconnect(s, in.Token)
	}
}

// onOutcome 处理排队结果。
func (s *session) onOutcome(o matchmaker.Outcome) {
	s.pairOut = nil
	switch o.Kind {
	case matchmaker.OutcomeMatched:
		// 双方主循环并发到达：谁先到谁开局（Matchup.Start 全局恰好一次），
		// 另一方等待开局完成后才继续，此后 swap 必然能安全投递。
		o.Match.Start(func() { s.hub.startMatch(o.Match) })
	case matchmaker.OutcomeTimeout:
		s.mu.Lock()
		s.state = stateIdle
		s.mu.Unlock()
		s.send(protocol.TypeQueueTimeout, struct{}{})
	case matchmaker.OutcomeCancelled:
		s.mu.Lock()
		s.state = stateIdle
		s.mu.Unlock()
		s.send(protocol.TypeQueueCancelled, struct{}{})
	}
}

// deliver 把房间事件帧编码后送入出站缓冲（hub 的座位 Sink 调用）。
// Settlement 置终局标志并关闭出站——writer 排空最后一帧后正常关闭连接。
func (s *session) deliver(f room.Frame) {
	typ, payload := protocol.FromRoom(f)
	s.send(typ, payload)
	if _, last := f.(room.SettlementFrame); last {
		s.mu.Lock()
		s.finished = true
		s.mu.Unlock()
		s.out.close()
	}
}

// readLoop 持续读取 WS 消息并解析；协议级错误直接回错误帧（连接不断），
// 连接级错误（对端断开、CloseNow）关闭 msgCh 结束主循环。进程不崩溃。
func (s *session) readLoop() {
	defer close(s.msgCh)
	ctx := context.Background()
	for {
		typ, data, err := s.conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			s.sendError(protocol.CodeBadPayload, "text messages only")
			continue
		}
		in, err := protocol.Parse(data)
		if err != nil {
			s.sendParseError(err)
			continue
		}
		select {
		case s.msgCh <- in:
		default:
			s.sendError(protocol.CodeInternal, "server busy")
		}
	}
}

func (s *session) sendParseError(err error) {
	switch {
	case errors.Is(err, protocol.ErrBadJSON):
		s.sendError(protocol.CodeBadJSON, "message is not valid json")
	case errors.Is(err, protocol.ErrBadVersion):
		s.sendError(protocol.CodeBadVersion, "unsupported protocol version")
	case errors.Is(err, protocol.ErrUnknownType):
		s.sendError(protocol.CodeUnknownType, "unknown message type")
	default:
		s.sendError(protocol.CodeBadPayload, "malformed payload")
	}
}

// writeLoop 排空出站缓冲并逐帧写 WS。写出失败即关闭连接（读泵随之结束）；
// out 关闭（终局或断连清理）后排空完退出并正常关闭连接。
func (s *session) writeLoop() {
	ctx := context.Background()
	for data := range s.out.ch {
		if err := s.conn.Write(ctx, websocket.MessageText, data); err != nil {
			s.conn.CloseNow()
			return
		}
	}
	_ = s.conn.Close(websocket.StatusNormalClosure, "")
}

// cleanup 幂等清理：退出队列或从座位绑定摘除（对局仍活即掉线宽限，
// issue 04 语义），关出站缓冲、断连接。
func (s *session) cleanup() {
	s.hub.mm.Cancel(s) // 排队中则退出队列，否则无操作
	s.hub.detach(s)
	s.out.close()
	s.conn.CloseNow()
}

// send 编码并入站出站缓冲；编码失败仅存在于编程错误（载荷类型固定）。
func (s *session) send(typ string, payload any) {
	data, err := protocol.Encode(typ, payload)
	if err != nil {
		return
	}
	s.out.send(data)
}

func (s *session) sendError(code, msg string) {
	s.send(protocol.TypeError, protocol.ErrorMsg{Code: code, Message: msg})
}
