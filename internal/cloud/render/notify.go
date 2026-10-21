package render

import (
	"sync"
	"time"
)

// Notifier 把“内容有变化”这件事广播给等待中的对账请求。
//
// 传输是单向请求/响应，服务端无法主动推给代理，所以用长轮询表达实时性（需求 4.2）：
// 代理带着 WaitSeconds 发起 sync，服务端在没有差异时挂起，一旦有提交就唤醒。
//
// 这里刻意不做队列：代理不需要知道“发生了什么变化”，只需要重新对账一次。
// 一次唤醒就等于“请再来一次 sync”，语义比事件流简单得多。
type Notifier struct {
	mu   sync.Mutex
	ver  uint64
	ch   chan struct{}
	last map[string]time.Time
}

// NewNotifier 构造通知器。
func NewNotifier() *Notifier {
	return &Notifier{ch: make(chan struct{}), last: map[string]time.Time{}}
}

// Bump 表示内容发生了变化，唤醒全部等待者。
func (n *Notifier) Bump() {
	n.mu.Lock()
	n.ver++
	if n.ch != nil {
		close(n.ch)
	}
	n.ch = make(chan struct{})
	n.mu.Unlock()
}

// snapshot 返回当前版本号与一个在该版本之后关闭的 channel。
func (n *Notifier) snapshot() (uint64, <-chan struct{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ver, n.ch
}

// Wait 等待内容变化或超时。
//
// 返回值表示“是否真的等到了变化”。超时返回 false，调用方据此回一个空 batch，
// 让代理重新发起下一次长轮询。
func (n *Notifier) Wait(timeout time.Duration) bool {
	if timeout <= 0 {
		return false
	}
	start, ch := n.snapshot()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		// 再确认一次版本号，避免把别人的唤醒当成本次差异。
		cur, _ := n.snapshot()
		return cur != start
	case <-timer.C:
		return false
	}
}

// MarkDevice 记录某台设备最近一次对账的时间，供在线状态与漂移展示使用。
func (n *Notifier) MarkDevice(deviceID string, at time.Time) {
	n.mu.Lock()
	n.last[deviceID] = at
	n.mu.Unlock()
}

// Devices 返回最近活跃的设备时间快照。
func (n *Notifier) Devices() map[string]time.Time {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make(map[string]time.Time, len(n.last))
	for k, v := range n.last {
		out[k] = v
	}
	return out
}
