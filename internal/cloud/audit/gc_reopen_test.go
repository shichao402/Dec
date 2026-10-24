package audit

import (
	"testing"
	"time"
)

// TestGcKeepsRepositoryUsable 验证 gc 之后审计还能继续提交。
//
// 这是一个真实缺陷的回归测试，而不是预防性断言：
// go-git 的 ObjectStorage 会缓存 pack 索引（requireIndex 只在 index 为 nil 时
// 重建），而 git gc 会删掉旧 pack、写成新 pack。Log 持有的仓库句柄是长寿命的，
// gc 之后它仍指向已删除的 pack 文件，于是下一次 commit 读 HEAD tree 时就失败。
//
// 危险之处在于失败是静默的：commitPending 的错误经 writeEntry 被吞成
// dropped 计数，业务照常返回成功，而审计从此不再落盘。所以这里必须
// 走完整路径——Append + Flush 真的触发一次 commit，并检查仓库仍可读。
func TestGcKeepsRepositoryUsable(t *testing.T) {
	const n = 20
	log, at := writeForGc(t, n)

	before, err := log.LooseObjects()
	if err != nil {
		t.Fatalf("统计松散对象失败: %v", err)
	}
	if before == 0 {
		t.Skip("当前环境未暴露松散对象计数（storer 不支持），跳过")
	}
	if err := log.gcAt(1); err != nil {
		t.Fatalf("gc 失败: %v", err)
	}

	// gc 之后再写一条并真的提交：这一步要读 HEAD tree 与已有对象，
	// 句柄若还指向被删的 pack，这里就会失败。
	if err := log.Append(Entry{
		At:    at.Add(3 * time.Hour),
		Event: EventAssetDelete,
		OK:    true,
	}); err != nil {
		t.Fatalf("gc 后追加失败: %v", err)
	}
	if err := log.Flush(); err != nil {
		t.Fatalf("gc 后提交失败（仓库句柄指向了被 gc 删除的 pack）: %v", err)
	}
	if got := log.Dropped(); got != 0 {
		t.Fatalf("gc 后出现 %d 条审计被丢弃，说明写入路径已经失效", got)
	}

	// 提交必须真的进了历史。
	head := log.Head()
	if head == "" {
		t.Fatal("gc 后读不到 HEAD")
	}
	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("gc 后读取失败: %v", err)
	}
	if len(items) != n+1 {
		t.Fatalf("应 %d 条，实际 %d 条", n+1, len(items))
	}
}

// TestGcThenDropAllCaches 验证连续两次 gc 也安全。
//
// 真实运维里 gc 是每天跑的，跨多天连续执行不该越跑越糟。
func TestGcThenDropAllCaches(t *testing.T) {
	log, at := writeForGc(t, 10)
	for round := 0; round < 2; round++ {
		if err := log.gcAt(1); err != nil {
			t.Fatalf("第 %d 轮 gc 失败: %v", round+1, err)
		}
		if err := log.Append(Entry{
			At:    at.Add(time.Duration(round+4) * time.Hour),
			Event: EventAssetWrite,
			OK:    true,
		}); err != nil {
			t.Fatalf("第 %d 轮 gc 后追加失败: %v", round+1, err)
		}
		if err := log.Flush(); err != nil {
			t.Fatalf("第 %d 轮 gc 后提交失败: %v", round+1, err)
		}
	}
	if got := log.Dropped(); got != 0 {
		t.Fatalf("连续 gc 后出现 %d 条丢弃", got)
	}
}
