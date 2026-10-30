package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// 检查间隔由服务端决定：代理只接受外部下发，本地不落盘。
// 这几个用例守住两件事：下发能生效，且重复下发不会把等待无限推迟。

// TestUpdaterSetIntervalChangesCurrent 验证下发的间隔确实成为当前间隔。
func TestUpdaterSetIntervalChangesCurrent(t *testing.T) {
	u := &Updater{}
	if got := u.currentInterval(); got != time.Hour {
		t.Fatalf("默认间隔应为 1 小时，实际 %v", got)
	}
	u.SetInterval(15 * time.Minute)
	if got := u.currentInterval(); got != 15*time.Minute {
		t.Fatalf("间隔应为 15 分钟，实际 %v", got)
	}
	// 非法值忽略：服务端下发了 0 或负数时不能被当成"禁用"，
	// 否则一次坏配置就让这台设备再也不检查更新。
	u.SetInterval(0)
	u.SetInterval(-time.Minute)
	if got := u.currentInterval(); got != 15*time.Minute {
		t.Fatalf("非法间隔不该覆盖当前值，实际 %v", got)
	}
}

// TestUpdaterSetIntervalIsIdempotent 验证重复下发同一个值不触发重排。
//
// 握手每 30 秒一轮，每轮都会把服务端的间隔传进来。若每次都当成"变化"，
// 定时器会被反复重置，实际检查间隔就变成了握手间隔——看起来功能正常，
// 实际是每 30 秒拉一次发布清单。
func TestUpdaterSetIntervalIsIdempotent(t *testing.T) {
	u := &Updater{}
	u.SetInterval(time.Hour)
	select {
	case <-u.retuned:
		t.Fatal("首次设置与默认值相同，不该触发重排")
	case <-time.After(20 * time.Millisecond):
	}
	u.SetInterval(2 * time.Hour)
	select {
	case <-u.retuned:
	case <-time.After(time.Second):
		t.Fatal("值变化后应唤醒更新循环重排定时器")
	}
	// 再发一次相同的值不该再触发。
	u.SetInterval(2 * time.Hour)
	select {
	case <-u.retuned:
		t.Fatal("相同值不该再次唤醒更新循环")
	case <-time.After(20 * time.Millisecond):
	}
}

// TestUpdaterRunRetunesOnIntervalChange 验证沉睡的循环会被新间隔唤醒。
//
// 起点是 1 小时的首次等待：若 SetInterval 只改了字段而没有叫醒循环，
// 这里会一直睡到超时。服务端把间隔调小时，"下一次检查"必须在新的短间隔
// 之后发生，而不是等完原来那一小时。
func TestUpdaterRunRetunesOnIntervalChange(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	u := &Updater{
		// 指向必然连不上的地址：这里只关心"有没有去检查"，不关心结果。
		ServerURL:    "http://127.0.0.1:1",
		Version:      "v2.1.48",
		StateDir:     t.TempDir(),
		Interval:     time.Hour,
		InitialDelay: time.Hour,
		Logf: func(format string, args ...any) {
			mu.Lock()
			logs = append(logs, format)
			mu.Unlock()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		u.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Run 未在 ctx 结束后退出")
		}
	})

	// 等循环进入 1 小时的首次等待，再把间隔调成 50ms。
	time.Sleep(50 * time.Millisecond)
	u.SetInterval(50 * time.Millisecond)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		hit := false
		for _, line := range logs {
			if strings.Contains(line, "自动更新检查失败") {
				hit = true
				break
			}
		}
		mu.Unlock()
		if hit {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("调整间隔后循环没有按新间隔醒来检查一次")
}
