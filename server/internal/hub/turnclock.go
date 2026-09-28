package hub

import (
	"time"

	"poptrio/server/internal/room"
)

// turnClock 把回合窗口截止换算为真实定时器：快照帧携带的截止时间戳到期时
// 向房间投递截止命令。observe 由座位 0 的 Sink 在房间事件循环 goroutine 内
// 调用，bind 发生在 Run 启动之前——全部字段访问有明确的 happens-before，
// 无需加锁。迟到的截止命令由房间自行忽略（issue 03 语义），竞态无害。
type turnClock struct {
	r     *room.Room
	timer *time.Timer
}

func (c *turnClock) bind(r *room.Room) { c.r = r }

func (c *turnClock) observe(f room.Frame) {
	switch v := f.(type) {
	case room.MatchStartedFrame:
		c.schedule(v.Snapshot)
	case room.TurnStartedFrame:
		c.schedule(v.Snapshot)
	case room.SettlementFrame:
		if c.timer != nil {
			c.timer.Stop() // 终局后停止投递截止命令（issue 03 交接）
		}
	}
}

// schedule 以快照截止时间戳重设定时器；已过期的截止立即触发。
func (c *turnClock) schedule(snap room.Snapshot) {
	if c.timer != nil {
		c.timer.Stop()
	}
	d := time.Until(snap.Deadline)
	turn := snap.Turn
	c.timer = time.AfterFunc(d, func() { c.r.SubmitDeadline(turn) })
}
