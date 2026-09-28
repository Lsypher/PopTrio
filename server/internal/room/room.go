// Package room 实现对局房间 Actor：每个房间一个事件循环 goroutine（ADR-0004），
// 全部输入——两名玩家的 Swap 请求、回合窗口截止——统一汇入同一 channel
// 串行处理；对局状态仅该 goroutine 可写，无锁。事件帧向双玩家输出。
// 本包无网络 IO：输入为进程内命令投递，输出为 Sink 回调；WS 泵与真实
// 回合计时器的接线属协议层（issue 05）。
package room

import (
	"math/rand/v2"
	"time"

	"poptrio/server/internal/judge"
)

// Config 是对局参数，均为服务端配置项而非硬编码。
type Config struct {
	Board       judge.Config // 棋盘尺寸与颜色数
	TurnSeconds int          // 每回合操作窗口秒数
	TotalTurns  int          // 一局总回合数，双方交替（12 = 双方各 6）
}

// DefaultConfig 是 MVP 配置：9x9 / 6 色，每回合 10 秒，共 12 回合。
func DefaultConfig() Config {
	return Config{Board: judge.DefaultConfig(), TurnSeconds: 10, TotalTurns: 12}
}

// Deps 注入外部性。零值字段使用默认实现（全局随机源 / time.Now）。
type Deps struct {
	// TileRNG 产出棋子颜色，是棋盘生成与补充的唯一随机源（判定器约定）。
	TileRNG judge.TileRNG
	// FirstCoin 返回 0 或 1，决定先手座位。
	FirstCoin func() int
	// Now 返回服务端当前时间，用于快照中的窗口截止时间戳。
	Now func() time.Time
}

// Sink 接收广播事件帧。真实部署由每玩家的 WS 泵实现；测试用回调收集。
type Sink func(Frame)

// command 是汇入事件循环的全部输入。新增输入（掉线宽限、重连等）时
// 扩展此集合即可，事件循环的串行语义不变。
type command interface{ isCommand() }

type swapCmd struct {
	seat int
	swap judge.Swap
}

type deadlineCmd struct{ turn int }

func (swapCmd) isCommand()     {}
func (deadlineCmd) isCommand() {}

// Room 是一个对局房间：事件循环 goroutine 独占写全部对局状态，无锁。
// 生命周期：Run 发出 MatchStarted，串行处理命令，发出 Settlement 后返回。
type Room struct {
	cfg   Config
	seats [2]string
	rng   judge.TileRNG
	coin  func() int
	now   func() time.Time
	sinks [2]Sink
	cmds  chan command

	// —— 以下状态仅事件循环 goroutine 可见（ADR-0004）——
	board    judge.Board
	scores   [2]int
	turn     int // 1-based；MatchStarted 时置 1
	operator int // 当前操作方座位（0/1）
	deadline time.Time
	finished bool
}

// New 创建房间。sinks 按座位序接收广播帧；seats 为双方玩家标识。
// 参数非法时 panic（编程错误）。
func New(seats [2]string, cfg Config, deps Deps, sinks [2]Sink) *Room {
	if cfg.Board.Width <= 0 || cfg.Board.Height <= 0 || cfg.Board.Colors <= 0 {
		panic("room: invalid board config")
	}
	if cfg.TurnSeconds <= 0 || cfg.TotalTurns <= 0 {
		panic("room: TurnSeconds and TotalTurns must be positive")
	}
	if seats[0] == "" || seats[1] == "" || seats[0] == seats[1] {
		panic("room: seat ids must be non-empty and distinct")
	}
	for i, sink := range sinks {
		if sink == nil {
			panic("room: nil sink at seat " + string(rune('0'+i)))
		}
	}
	rng := deps.TileRNG
	if rng == nil {
		rng = func(colors int) judge.Tile { return judge.Tile(rand.IntN(colors)) }
	}
	coin := deps.FirstCoin
	if coin == nil {
		coin = func() int { return rand.IntN(2) }
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &Room{
		cfg:   cfg,
		seats: seats,
		rng:   rng,
		coin:  coin,
		now:   now,
		sinks: sinks,
		cmds:  make(chan command, 64),
	}
}

// SubmitSwap 投递一次玩家交换请求，按到达顺序串行判定。窗口截止命令
// 先入队时，本请求按新回合归属判定——截止后必拒，结果确定（ADR-0004）。
func (r *Room) SubmitSwap(seat int, s judge.Swap) {
	r.cmds <- swapCmd{seat: seat, swap: s}
}

// SubmitDeadline 投递回合窗口截止命令。真实部署由回合计时器在
// cfg.TurnSeconds 到期时调用；测试直接注入以确定性驱动窗口竞态。
// 非当前回合的截止命令被忽略。
func (r *Room) SubmitDeadline(turn int) {
	r.cmds <- deadlineCmd{turn: turn}
}

// Run 运行事件循环直至 Settlement：先发对局开始帧，再串行处理全部命令。
// 终局后返回；此后 SubmitSwap/SubmitDeadline 仅入队缓冲，不再被消费。
func (r *Room) Run() {
	r.startMatch()
	for cmd := range r.cmds {
		switch c := cmd.(type) {
		case swapCmd:
			r.handleSwap(c)
		case deadlineCmd:
			if !r.finished && c.turn == r.turn {
				r.closeWindow()
			}
		}
		if r.finished {
			return
		}
	}
}

// startMatch 生成初始棋盘、掷先手并广播对局开始（含回合 1 快照）。
func (r *Room) startMatch() {
	r.board = r.mustGenerate()
	r.turn = 1
	r.operator = r.coin()
	r.deadline = r.now().Add(r.window())
	r.emit(MatchStartedFrame{
		Seats:     r.seats,
		FirstSeat: r.operator,
		Snapshot:  r.snapshot(),
	})
}

// handleSwap 处理交换请求：越权即拒；有效则连锁结算并记账；
// 无效回弹——不改变棋盘、不结束回合。棋盘落定后检测死局并重排。
func (r *Room) handleSwap(c swapCmd) {
	if c.seat != r.operator {
		r.emit(SwapRejectedFrame{Seat: c.seat, Swap: c.swap, Reason: RejectNotOperator})
		return
	}
	res, ok := judge.ApplySwap(r.board, c.swap, r.seats[c.seat], r.rng)
	if !ok {
		reason := RejectNoClear
		if !r.board.ValidSwap(c.swap) {
			reason = RejectInvalidSwap
		}
		r.emit(SwapRejectedFrame{Seat: c.seat, Swap: c.swap, Reason: reason})
		return
	}
	r.board = res.Board
	r.scores[c.seat] += res.TotalScore // Attribution：全部波次归操作者
	r.emit(SwapResultFrame{
		Operator: c.seat,
		Waves:    waveViews(res.Waves),
		Board:    res.Board.Tiles,
		Scores:   r.scores,
	})
	if r.board.IsDeadBoard() {
		r.board = r.mustGenerate() // Reshuffle：分数与剩余回合不变
		r.emit(ReshuffleFrame{Board: r.board.Tiles, Scores: r.scores, Turn: r.turn})
	}
}

// closeWindow 处理当前回合窗口截止：流转下一回合（交替操作方）或终局结算。
func (r *Room) closeWindow() {
	if r.turn >= r.cfg.TotalTurns {
		r.finished = true
		r.emit(SettlementFrame{
			Draw:   r.scores[0] == r.scores[1],
			Winner: winnerOf(r.scores),
			Scores: r.scores,
		})
		return
	}
	r.turn++
	r.operator = 1 - r.operator
	r.deadline = r.now().Add(r.window())
	r.emit(TurnStartedFrame{Snapshot: r.snapshot()})
}

func winnerOf(scores [2]int) int {
	switch {
	case scores[0] > scores[1]:
		return 0
	case scores[1] > scores[0]:
		return 1
	default:
		return -1
	}
}

// window 返回回合操作窗口时长。
func (r *Room) window() time.Duration {
	return time.Duration(r.cfg.TurnSeconds) * time.Second
}

// mustGenerate 生成满足两约束的棋盘（开局与 Reshuffle 共用）。
// 配置已在 New 校验，错误不可达。
func (r *Room) mustGenerate() judge.Board {
	b, err := judge.GenerateBoard(r.cfg.Board, r.rng)
	if err != nil {
		panic("room: generate board: " + err.Error())
	}
	return b
}

func (r *Room) snapshot() Snapshot {
	return Snapshot{
		Board:    r.board.Tiles,
		Scores:   r.scores,
		Turn:     r.turn,
		Operator: r.operator,
		Deadline: r.deadline,
	}
}

// emit 向双玩家广播同一帧。仅事件循环 goroutine 调用；Sink 不得阻塞
// （慢消费者的丢弃策略由协议层 Sink 实现决定，见 ADR-0004）。
func (r *Room) emit(f Frame) {
	r.sinks[0](f)
	r.sinks[1](f)
}

func waveViews(ws []judge.Wave) []WaveView {
	out := make([]WaveView, len(ws))
	for i, w := range ws {
		out[i] = WaveView{Clear: w.Clear.Cells, Score: w.Score, Board: w.Board.Tiles}
	}
	return out
}
