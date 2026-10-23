package backup

import (
	"context"
	"testing"
	"time"
)

// 审计的推送节奏必须独立于"有没有人改内容"。
//
// 背景（2026-10-23 之后发现）：auditTick 之前，审计只有两条上远端的路径——
// 内容变更后由 recordRevision 调 Trigger，以及 24 小时兜底。而审计里最该留痕的
// 一批事件恰恰不产生内容变更：密钥明文查看、下发、登记尝试、客户端下载。
// 于是"一整天只读不写"的窗口里，这些记录全躺在本地，冷备却是齐的。
//
// 这个测试锁住两件事：审计会自己按周期推上去；这个周期不依赖 Trigger。

// TestAuditPushesWithoutTrigger 验证没有任何 Trigger 时审计也会上远端。
func TestAuditPushesWithoutTrigger(t *testing.T) {
	r := newRemote(t)
	w := newWork(t)
	w.commit(t, "events/2026-10/x.jsonl", "{\"event\":\"secret.read\"}\n")

	rep := &fakeReporter{}
	svc := NewService(Options{
		// 只配审计，冷备目标留空：本用例只关心审计自己的节奏。
		AuditTarget: Target{Name: "审计", Dir: w.dir, URL: r.dir, Branch: "master"},
		Reporter:    rep,
		AuditTick:   50 * time.Millisecond,
		// 兜底调到极长：证明推送确实来自 auditTick，而不是每日兜底。
		DailyTick: time.Hour,
		Debounce:  time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	startForTest(t, ctx, cancel, svc)

	// 全程不调用 Trigger。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := r.repo.Reference("refs/heads/master", false); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("审计没有按自己的周期推送：纯读窗口里的审计记录会在机器损坏时一起丢失")
}

// TestAuditTickSkippedWhilePaused 验证暂停期间审计也不推。
//
// 部署期间暂停的是"整个推送"，不只是冷备：部署中的审计同样可能对应一次
// 随后被回滚的操作，提前推上去只会污染远端历史（需求 21.2）。
func TestAuditTickSkippedWhilePaused(t *testing.T) {
	r := newRemote(t)
	w := newWork(t)
	w.commit(t, "events/2026-10/x.jsonl", "{\"event\":\"deploy\"}\n")

	svc := NewService(Options{
		AuditTarget: Target{Name: "审计", Dir: w.dir, URL: r.dir, Branch: "master"},
		AuditTick:   30 * time.Millisecond,
		DailyTick:   time.Hour,
		Debounce:    time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	startForTest(t, ctx, cancel, svc)

	svc.Pause()
	time.Sleep(200 * time.Millisecond)
	if _, err := r.repo.Reference("refs/heads/master", false); err == nil {
		t.Fatal("暂停期间审计不应推送")
	}

	svc.Resume()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := r.repo.Reference("refs/heads/master", false); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("恢复后审计应补推一次")
}

// TestAuditTickOnlyPushesAudit 验证审计周期不顺手推冷备。
//
// 冷备与审计共用一个 running 标记，如果审计周期去做全量 flush，
// 每 5 分钟就会白跑一次冷备的 fetch——这台机器只有 1 核 1G，
// 无变更的往返也该省掉。
func TestAuditTickOnlyPushesAudit(t *testing.T) {
	auditRemote := newRemote(t)
	backupRemote := newRemote(t)

	auditWork := newWork(t)
	auditWork.commit(t, "events/2026-10/x.jsonl", "{\"event\":\"sync\"}\n")
	backupWork := newWork(t)
	backupWork.commit(t, "ssot/a.json", "{}")

	svc := NewService(Options{
		BackupTarget: Target{Name: "冷备", Dir: backupWork.dir, URL: backupRemote.dir, Branch: "master"},
		AuditTarget:  Target{Name: "审计", Dir: auditWork.dir, URL: auditRemote.dir, Branch: "master"},
		AuditTick:    40 * time.Millisecond,
		DailyTick:    time.Hour,
		Debounce:     time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	startForTest(t, ctx, cancel, svc)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := auditRemote.repo.Reference("refs/heads/master", false); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := auditRemote.repo.Reference("refs/heads/master", false); err != nil {
		t.Fatal("审计没有推上去")
	}
	if _, err := backupRemote.repo.Reference("refs/heads/master", false); err == nil {
		t.Fatal("审计周期不应顺带推送冷备")
	}
}

// TestAuditTickDefault 验证默认周期是 5 分钟而不是 24 小时。
//
// 这个断言看着脆（改个常量就红），但它锁的是一个判断而不是数字：
// 审计的上界必须比每日兜底短得多，否则"机器坏了审计全丢"这个缺口会回来。
func TestAuditTickDefault(t *testing.T) {
	svc := NewService(Options{})
	if svc.auditTick != 5*time.Minute {
		t.Fatalf("审计默认周期 = %v，期望 5m；若确要改，请先想清楚纯读窗口的审计还要不要在当天上远端", svc.auditTick)
	}
}
