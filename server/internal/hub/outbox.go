package hub

import "sync"

// outbox 是会话出站缓冲：任意 goroutine 并发投递（非阻塞，满则丢弃），
// 关闭幂等且关闭后投递安全跳过。写泵排空后退出——settlement 先入队后
// 关闭，保证最后一帧必被写出。
// 丢弃策略（ADR-0004 留给实施定的决定）：连接健康时缓冲不会满；满即意味
// 写泵卡死（对端失联），丢帧让房间事件循环不被单个慢消费者阻塞。
type outbox struct {
	mu     sync.Mutex
	ch     chan []byte
	closed bool
}

func newOutbox() *outbox { return &outbox{ch: make(chan []byte, 128)} }

func (o *outbox) send(data []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	select {
	case o.ch <- data:
	default:
	}
}

func (o *outbox) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	o.closed = true
	close(o.ch)
}
