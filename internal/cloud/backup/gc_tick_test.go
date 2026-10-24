package backup

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// 审计仓库的每日回收。
//
// 背景：审计只追加，定时提交会持续产生松散对象。2026-10-23 那次 .git 涨到 5.7G
// 的直接原因是提交太碎（已由 ADR 0039 修掉），但"没有人主动回收"是另一处缺口：
// 它依赖 git 自己的 gc.auto 阈值，而那个阈值在高频提交下要么长期达不到、
// 要么在某次提交后突然触发并把小内存机器拖住。
//
// 这里锁住三件事：到点会回收、暂停时不回收、回收失败会上报但不影响后续周期。

// TestDailyTickRunsAuditGc 验证每日兜底周期会触发一次回收。
func TestDailyTickRunsAuditGc(t *testing.T) {
	var called atomic.Int32
	svc := NewService(Options{
		DailyTick: 30 * time.Millisecond,
		// 审计周期调到极长：证明回收来自每日兜底，而不是审计推送。
		AuditTick: time.Hour,
		Debounce:  time.Hour,
		GcAudit: func() error {
			called.Add(1)
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	startForTest(t, ctx, cancel, svc)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if called.Load() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("每日兜底周期没有触发审计仓库回收：松散对象会长期累积")
}

// TestAuditGcSkippedWhilePaused 验证暂停期间不做回收。
//
// 部署期间暂停的是整个维护动作。回收虽然不改变记录内容，但它会重写对象库；
// 部署失败要回滚时，让对象库在这段时间里保持原样更省心。
func TestAuditGcSkippedWhilePaused(t *testing.T) {
	var called atomic.Int32
	svc := NewService(Options{
		DailyTick: 20 * time.Millisecond,
		AuditTick: time.Hour,
		Debounce:  time.Hour,
		GcAudit: func() error {
			called.Add(1)
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	startForTest(t, ctx, cancel, svc)

	svc.Pause()
	time.Sleep(200 * time.Millisecond)
	if n := called.Load(); n != 0 {
		t.Fatalf("暂停期间不应回收，实际调用了 %d 次", n)
	}
}

// TestAuditGcFailureReported 验证回收失败会上报，且不阻断后续周期。
//
// 回收是维护动作：这次没收成不该影响推送，明天还会再试。
// 但也不能静默——"审计仓库一直在长但没人回收"正是要避免的静默失效。
func TestAuditGcFailureReported(t *testing.T) {
	rep := &fakeReporter{}
	var calls atomic.Int32
	svc := NewService(Options{
		DailyTick: 20 * time.Millisecond,
		AuditTick: time.Hour,
		Debounce:  time.Hour,
		Reporter:  rep,
		GcAudit: func() error {
			calls.Add(1)
			return errors.New("git 不存在")
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	startForTest(t, ctx, cancel, svc)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if failed, _, _ := rep.counts(); failed > 0 {
			// 失败被上报之后，后面的周期仍在继续。
			before := calls.Load()
			time.Sleep(80 * time.Millisecond)
			if calls.Load() <= before {
				t.Fatal("一次失败后不应停止后续回收：明天还要再试")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("回收失败没有上报：审计仓库会静默地越长越大")
}
