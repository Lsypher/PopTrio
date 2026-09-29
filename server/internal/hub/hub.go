// Package hub 把 WebSocket 连接接到房间 Actor（ADR-0004 的协议层接线）：
// 每个连接一个 session（读泵 / 写泵 / 主循环三个 goroutine，与房间仅经
// channel 通信），排队经 Matchmaker，配对成功即建 Room 并挂上回合计时器；
// 断线即掉线宽限，凭对局 token 可换绑重连（issue 04 语义）。
package hub

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"poptrio/server/internal/matchmaker"
	"poptrio/server/internal/protocol"
	"poptrio/server/internal/room"
)

// Config 是 hub 配置。
type Config struct {
	Room         room.Config   // 对局参数（回合秒数、总回合数、宽限秒数、棋盘）
	QueueTimeout time.Duration // 排队超时时长（配置项）
}

// matchBinding 是一个对局座位的当前绑定：房间身份 + 占用它的连接。
// 房间的 Sink 固定指向绑定而非具体连接——重连换绑时房间侧零改动。
// session 与 grace 由 hub.mu 保护。
type matchBinding struct {
	rm      roomMatch
	token   string
	session *session    // nil = 座位空置（掉线宽限中或已终局）
	grace   *time.Timer // 掉线宽限定时器；重连或终局时取消
}

// Hub 是进程级单例：持有匹配队列与对局座位绑定表，按连接创建 session。
type Hub struct {
	cfg    Config
	deps   room.Deps // 测试可注入确定性依赖；零值字段用 room 默认实现
	mm     *matchmaker.Matchmaker
	nextID atomic.Int64

	mu       sync.Mutex
	bindings map[string]*matchBinding // 对局 token → 座位绑定；终局即摘除
}

// New 创建 hub。
func New(cfg Config, deps room.Deps) *Hub {
	return &Hub{
		cfg:      cfg,
		deps:     deps,
		mm:       matchmaker.New(cfg.QueueTimeout),
		bindings: make(map[string]*matchBinding),
	}
}

// Handler 返回进程 HTTP 路由：/healthz 健康检查 + /ws WebSocket 端点。
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /ws", h.serveWS)
	return mux
}

// serveWS 升级连接并运行 session 直至连接结束。
//
// 客户端为任意源的静态托管页（端口即与 ws 端点不同源），且端点不携带
// Cookie 等环境凭据、对局 token 逐局私发——CSWSH 无可窃取面，故跳过
// 库默认的 Origin 校验（coder/websocket v1.8.15 起跨源默认 403）。
// 引入凭据或收紧部署拓扑时应改为显式 OriginPatterns 白名单。
func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	s := &session{
		hub:   h,
		conn:  conn,
		id:    fmt.Sprintf("p%d", h.nextID.Add(1)),
		out:   newOutbox(),
		msgCh: make(chan protocol.Inbound, 16),
	}
	s.run()
}

// startMatch 把配对双方接入新房间并启动事件循环。由配对双方经
// Matchup.Start 调用（恰好执行一次），完成后房间就绪、swap 可安全投递。
func (h *Hub) startMatch(match *matchmaker.Matchup) {
	s0 := match.Players[0].(*session)
	s1 := match.Players[1].(*session)
	s0.mu.Lock()
	s1.mu.Lock()
	defer s1.mu.Unlock()
	defer s0.mu.Unlock()
	if s0.state != stateQueued || s1.state != stateQueued {
		// 配对瞬间有玩家已断开：存活的原地重新排队——客户端仍处于
		// 「排队中」，无感，超时计时重新起算。
		for _, s := range []*session{s0, s1} {
			if s.state == stateQueued {
				s.pairOut = h.mm.Join(s)
			}
		}
		return
	}
	clock := &turnClock{}
	tokens := [2]string{newToken(), newToken()}
	b0 := &matchBinding{rm: roomMatch{seat: 0}, token: tokens[0]}
	b1 := &matchBinding{rm: roomMatch{seat: 1}, token: tokens[1]}
	r := room.New([2]string{s0.id, s1.id}, tokens, h.cfg.Room, h.deps, [2]room.Sink{
		h.seatSink(b0, b1, clock),
		h.seatSink(b1, b0, nil),
	})
	clock.bind(r)
	b0.rm.r, b1.rm.r = r, r
	h.mu.Lock()
	h.bindings[tokens[0]] = b0
	h.bindings[tokens[1]] = b1
	b0.session, b1.session = s0, s1
	h.mu.Unlock()
	s0.rm = &b0.rm
	s1.rm = &b1.rm
	// 对局 token 由匹配层签发、经各自连接私发，绝不广播（issue 04 约定）。
	s0.send(protocol.TypeMatchToken, protocol.MatchTokenMsg{Token: tokens[0]})
	s1.send(protocol.TypeMatchToken, protocol.MatchTokenMsg{Token: tokens[1]})
	go r.Run()
}

// seatSink 返回一个座位的房间事件帧出口：帧投递给该座位当前绑定的连接
// （重连换绑后自然流向新连接）。座位 0 的 Sink 同时驱动回合计时器；
// Settlement 时摘除双方绑定（对局结束，token 作废）。
func (h *Hub) seatSink(b, other *matchBinding, clock *turnClock) room.Sink {
	return func(f room.Frame) {
		if clock != nil {
			clock.observe(f)
		}
		h.mu.Lock()
		s := b.session
		h.mu.Unlock()
		if s != nil {
			s.deliver(f)
		}
		if _, last := f.(room.SettlementFrame); last {
			h.endMatch(b, other)
		}
	}
}

// endMatch 摘除对局绑定（token 作废）并停掉宽限定时器。仅终局路径调用。
// 不清 session 引用：同一结算帧还要经两个座位 Sink 各自送达，clear 掉
// 座位 0 之外的引用会让后送的座位收不到 Settlement。
func (h *Hub) endMatch(b, other *matchBinding) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.bindings, b.token)
	delete(h.bindings, other.token)
	if b.grace != nil {
		b.grace.Stop()
		b.grace = nil
	}
	if other.grace != nil {
		other.grace.Stop()
		other.grace = nil
	}
}

// detach 把断开的连接从其座位绑定上摘除：对局仍活时标记掉线并武装宽限
// 定时器，届满未重连由房间判负（issue 04 语义）；已终局（绑定已摘除）
// 则无操作。重连成功时由 reconnect 停掉定时器并换绑新连接。
func (h *Hub) detach(s *session) {
	h.mu.Lock()
	var b *matchBinding
	for _, x := range h.bindings {
		if x.session == s {
			b = x
		}
	}
	if b == nil {
		h.mu.Unlock()
		return
	}
	b.session = nil
	grace := time.Duration(h.cfg.Room.GraceSeconds) * time.Second
	b.grace = time.AfterFunc(grace, func() { b.rm.r.SubmitGraceExpiry(b.rm.seat) })
	h.mu.Unlock()
	// 锁外投递：房间事件循环可能正阻塞在 hub.mu 上取 session。
	b.rm.r.Disconnect(b.rm.seat)
}

// reconnect 把一条新连接换绑到既有对局座位：凭 token 查绑定，座位空置即
// 换绑、停宽限定时器，再由房间校验 token 并广播全量快照（issue 04 语义）。
// 已终局对局的绑定已被摘除，返回 bad_token。绑定与结算瞬间竞争的窗口极小，
// 重连方最多收不到后续帧，由客户端重试超时兜底（MVP 边界）。
func (h *Hub) reconnect(s *session, token string) {
	s.mu.Lock()
	idle := s.state == stateIdle
	if idle {
		s.state = stateInRoom
	}
	s.mu.Unlock()
	if !idle {
		s.sendError(protocol.CodeBadState, "cannot reconnect from current state")
		return
	}
	h.mu.Lock()
	b := h.bindings[token]
	if b == nil || b.session != nil {
		h.mu.Unlock()
		s.mu.Lock()
		s.state = stateIdle
		s.mu.Unlock()
		s.sendError(protocol.CodeBadToken, "unknown or occupied match token")
		return
	}
	b.session = s
	s.mu.Lock()
	s.rm = &b.rm
	s.mu.Unlock()
	if b.grace != nil {
		b.grace.Stop()
		b.grace = nil
	}
	h.mu.Unlock()
	// 锁外投递：房间校验 token 后广播 reconnected + 全量快照。
	b.rm.r.Reconnect(b.rm.seat, token)
}

// newToken 生成 128bit 随机对局凭据。crypto/rand 失败即进程熵源损坏，
// panic 是唯一诚实的响应。
func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("hub: generate match token: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
