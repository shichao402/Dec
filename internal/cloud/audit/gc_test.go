package audit

import (
	"testing"
	"time"
)

// 审计仓库的体积维护测试。
//
// 背景：审计只追加，定时 commit 会产生大量松散对象。2026-10-23 那次故障里
// .git 一度涨到 5.7G，虽然主因是提交太碎（已由 ADR 0039 修掉），但"仓库体积
// 没有任何主动维护"这件事本身也是一处设计缺口——它依赖 git 自己的自动 gc
// 阈值，而那个阈值在高频提交下要么长期达不到、要么突然触发。
//
// 所以这里验证的是：Gc 能真的把松散对象收进 packfile。

// writeForGc 造 n 条业务事件并逐条提交，返回一个已有松散对象的审计仓库。
//
// 用业务事件（不会被聚合）确保每条都真的落盘；
// 每条后 Flush 一次，让每条都产生独立的 commit 与对象。
func writeForGc(t *testing.T, n int) (*Log, time.Time) {
	t.Helper()
	log, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	at := time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		if err := log.Append(Entry{
			At:      at.Add(time.Duration(i) * time.Minute),
			Event:   EventAssetWrite,
			Subject: "core",
			Detail:  "写入第 " + time.Duration(i).String() + " 条",
			OK:      true,
		}); err != nil {
			t.Fatalf("第 %d 条写入失败: %v", i, err)
		}
		if err := log.Flush(); err != nil {
			t.Fatalf("第 %d 次冲写失败: %v", i, err)
		}
	}
	return log, at
}

// TestGcPacksLooseObjects 验证达到阈值时 gc 会真的减少松散对象。
func TestGcPacksLooseObjects(t *testing.T) {
	const n = 60
	log, at := writeForGc(t, n)

	before, err := log.LooseObjects()
	if err != nil {
		t.Fatalf("统计松散对象失败: %v", err)
	}
	if before == 0 {
		t.Skip("当前环境未暴露松散对象计数（storer 不支持），跳过")
	}

	// 用 1 作阈值走完整决策路径：证明"过阈值 → 真的回收"。
	if err := log.gcAt(1); err != nil {
		t.Fatalf("gc 失败: %v", err)
	}

	after, err := log.LooseObjects()
	if err != nil {
		t.Fatalf("统计松散对象失败: %v", err)
	}
	if after >= before {
		t.Fatalf("gc 后松散对象应减少：前 %d，后 %d", before, after)
	}
	// 记录一条都不能少。
	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("gc 后读取失败: %v", err)
	}
	if len(items) != n {
		t.Fatalf("gc 不应丢记录：应 %d 条，实际 %d 条", n, len(items))
	}
	// 回收之后仍然能继续写入：gc 不该破坏仓库。
	if err := log.Append(Entry{
		At:    at.Add(time.Hour),
		Event: EventAssetDelete,
		OK:    true,
	}); err != nil {
		t.Fatalf("gc 后追加失败: %v", err)
	}
}

// TestGcSkipsWhenFewLooseObjects 验证松散对象很少时不做无用功。
//
// gc 要重写 packfile，代价不低。刚启动的仓库只有几十个对象，
// 每天跑一次没有意义，还会让"维护"变成噪声。
func TestGcSkipsWhenFewLooseObjects(t *testing.T) {
	log, _ := writeForGc(t, 1)

	before, err := log.LooseObjects()
	if err != nil {
		t.Fatalf("统计松散对象失败: %v", err)
	}
	// 阈值远高于现有对象数：Gc 应静默返回，不动对象库。
	if err := log.Gc(); err != nil {
		t.Fatalf("对象很少时 gc 应静默跳过，实际报错: %v", err)
	}
	after, err := log.LooseObjects()
	if err != nil {
		t.Fatalf("统计松散对象失败: %v", err)
	}
	if after != before {
		t.Fatalf("跳过时不应改动对象库：前 %d，后 %d", before, after)
	}
}

// TestGcDoesNotHoldLockWhileRunning 验证 gc 期间审计写入不被阻塞。
//
// 这是本次改动最关键的取舍：l.mu 是所有审计写入的必经之路，
// 而 git gc 在仓库变大后会跑很久。如果 gc 压在锁里，
// 一次维护就会变成整个服务的一次时长不可控的停顿。
func TestGcDoesNotHoldLockWhileRunning(t *testing.T) {
	log, at := writeForGc(t, 30)

	done := make(chan error, 1)
	go func() { done <- log.gcAt(1) }()

	// gc 还在跑的时候并发写一条：如果锁被 gc 占着，这里要等到回收结束才返回。
	// 用一个短超时判断"没有被卡住"。
	written := make(chan error, 1)
	go func() {
		written <- log.Append(Entry{
			At:    at.Add(2 * time.Hour),
			Event: EventAssetWrite,
			OK:    true,
		})
	}()

	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("gc 期间写入失败: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("gc 期间写入被阻塞，说明锁被 gc 长时间占着")
	}
	if err := <-done; err != nil {
		t.Fatalf("gc 失败: %v", err)
	}
}
