// Package matchmaker 实现全局匹配队列：先到先配凑齐两人即组成对局。
// MVP 无段位等匹配条件，任意两个排队者即可配对（"随机配对"即不设条件）；
// 排队超时自动退出并通知；手动取消立即生效。并发安全：全部队列操作由
// 互斥锁串行化，超时定时器与取消的竞争由锁内摘除保证结果恰好投递一次。
package matchmaker

import (
	"slices"
	"sync"
	"time"
)

// OutcomeKind 是排队结果类别。
type OutcomeKind int

const (
	OutcomeMatched   OutcomeKind = iota // 配对成功，Pair 非 nil
	OutcomeTimeout                      // 排队超时，自动退出队列
	OutcomeCancelled                    // 手动取消，已退出队列
)

// Outcome 是一次排队的结果，经 buffered-1 通道恰好投递一次。
type Outcome struct {
	Kind  OutcomeKind
	Match *Matchup
}

// Matchup 是一次配对：两名排队者 + 恰好一次的开局动作。
// Start 供双方各自调用：谁先到谁执行，另一方等待其完成后再返回——
// 调用方以此保证开局副作用（建房间、挂连接）全局恰好一次且完成后才继续。
type Matchup struct {
	Players [2]any // 排队者原样带回，顺序即入队顺序（座位 0/1）
	once    sync.Once
}

// Start 执行开局动作，全局恰好一次。
func (m *Matchup) Start(start func()) { m.once.Do(start) }

type waiter struct {
	player any
	out    chan Outcome
	timer  *time.Timer
}

// Matchmaker 是匹配队列。
type Matchmaker struct {
	timeout time.Duration // 排队超时时长（配置项）

	mu      sync.Mutex
	waiting []*waiter // FIFO，队首最先配对
}

// New 创建匹配队列，timeout 为排队超时时长。
func New(timeout time.Duration) *Matchmaker {
	return &Matchmaker{timeout: timeout}
}

// Join 加入队列并返回结果通道。配对成功时双方各自收到同一 *Pairing。
func (m *Matchmaker) Join(player any) <-chan Outcome {
	out := make(chan Outcome, 1)
	m.mu.Lock()
	w := &waiter{player: player, out: out}
	w.timer = time.AfterFunc(m.timeout, func() { m.expire(w) })
	m.waiting = append(m.waiting, w)
	if len(m.waiting) < 2 {
		m.mu.Unlock()
		return out
	}
	a, b := m.waiting[0], m.waiting[1]
	m.waiting = m.waiting[2:]
	a.timer.Stop()
	b.timer.Stop()
	match := &Matchup{Players: [2]any{a.player, b.player}}
	a.out <- Outcome{Kind: OutcomeMatched, Match: match}
	b.out <- Outcome{Kind: OutcomeMatched, Match: match}
	m.mu.Unlock()
	return out
}

// Cancel 把 player 移出队列并投递取消结果，立即生效。
// player 不在队列时无副作用并返回 false。
func (m *Matchmaker) Cancel(player any) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.waiting, func(w *waiter) bool { return w.player == player })
	if i < 0 {
		return false
	}
	w := m.waiting[i]
	m.waiting = slices.Delete(m.waiting, i, i+1)
	w.timer.Stop()
	w.out <- Outcome{Kind: OutcomeCancelled}
	return true
}

// expire 是排队超时回调：仍在队列则摘除并投递超时结果；已被配对或取消
// （waiter 已摘除）则无操作。与 Join/Cancel 由互斥锁串行化，结果恰好一条。
func (m *Matchmaker) expire(w *waiter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.waiting, func(x *waiter) bool { return x == w })
	if i < 0 {
		return
	}
	m.waiting = slices.Delete(m.waiting, i, i+1)
	w.out <- Outcome{Kind: OutcomeTimeout}
}
