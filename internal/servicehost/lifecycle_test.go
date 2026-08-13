package servicehost

import (
	"testing"
	"time"
)

func TestPresenceTrackerStartsIdleTimerOnlyAtZeroConnections(t *testing.T) {
	stopped := make(chan struct{}, 1)
	tracker := newPresenceTracker(40*time.Millisecond, func() { stopped <- struct{}{} })
	tracker.connected()

	select {
	case <-stopped:
		t.Fatal("存在门面连接时服务不应空闲退出")
	case <-time.After(70 * time.Millisecond):
	}

	tracker.disconnected()
	select {
	case <-stopped:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("最后一个门面断开后应按空闲超时退出")
	}
}

func TestPresenceTrackerAppliesUpdatedTimeout(t *testing.T) {
	stopped := make(chan struct{}, 1)
	tracker := newPresenceTracker(time.Hour, func() { stopped <- struct{}{} })
	tracker.setTimeout(30 * time.Millisecond)
	select {
	case <-stopped:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("更新后的 Settings 超时未生效")
	}
}
