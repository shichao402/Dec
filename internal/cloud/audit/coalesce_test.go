package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 这个测试对应 2026-10-23 的生产故障。
//
// 当时每条审计记录都单独 commit，一次 MCP 会话产生了 76,350 个 commit，
// 把 .git 撑到 5.7G，占满 40G 系统盘后整机卡死（SSH 与 HTTPS 全部无响应，
//
//	ping 还是通的）。根因不是"审计太多"，而是"提交太碎"。
//
// 这里断言两件事：
//  1. 一段密集写入只产生极少 commit（合并生效）；
//  2. 每条记录仍然完整保存（合并不会丢数据）。
func TestCommitCoalescing(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	first := log.Head()

	const n = 500
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		if err := log.Append(Entry{
			At:    at.Add(time.Duration(i) * time.Millisecond),
			Event: EventSync,
			OK:    true,
		}); err != nil {
			t.Fatalf("第 %d 条写入失败: %v", i, err)
		}
	}
	if err := log.Flush(); err != nil {
		t.Fatalf("冲写失败: %v", err)
	}

	head, err := log.repo.Head()
	if err != nil {
		t.Fatalf("读取 head 失败: %v", err)
	}
	commit, err := log.repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("读取 commit 失败: %v", err)
	}

	// 数一下从 first 到 head 之间有多少 commit。
	count := 0
	iter := commit
	for {
		count++
		if iter.Hash.String() == first {
			break
		}
		parents := iter.ParentHashes
		if len(parents) == 0 {
			break
		}
		parent, err := log.repo.CommitObject(parents[0])
		if err != nil {
			t.Fatalf("遍历父提交失败: %v", err)
		}
		iter = parent
	}

	// 500 条记录、单线程顺序写入，理想情况是 1 个 commit。
	// 实测会因 CommitBatch 边界多出几个，给到 10 已经很宽松；
	// 关键是绝不能再退回 500（每条一个）。
	if count > 10 {
		t.Fatalf("%d 条记录产生了 %d 个 commit，合并没有生效；"+
			"碎片化提交会把 .git 撑爆，2026-10-23 就是这样把盘写满的",
			n, count)
	}

	// 记录一条都不能少。
	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取记录失败: %v", err)
	}
	if len(items) != n {
		t.Fatalf("应保留 %d 条记录，实际 %d 条", n, len(items))
	}
}

// 窗口内的记录即使没提交，也必须已经写进工作树文件。
//
// 这是"机器被砸也不丢"的落点：fsync 才是持久化，commit 只是记进历史。
func TestRecordsSurviveWithoutCommit(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	at := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	if err := log.Append(Entry{At: at, Event: EventBootstrap, OK: true}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	// 不 Flush，直接从文件读：模拟提交窗口内进程被强杀。
	rel := filepath.Join("events", at.Format("2006-01"), at.Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("记录应已写入工作树文件: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("工作树文件为空：fsync 没有生效，进程崩溃会丢审计")
	}
}

// 重启后要把上次遗留的未提交记录补上，不能一直悬在工作树里。
func TestOpenCommitsLeftovers(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	at := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	if err := first.Append(Entry{At: at, Event: EventSync, OK: true}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	// 此时记录还在工作树、未提交（窗口未到）。

	if _, err := Open(dir); err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}

	// 重新打开后应已把遗留改动提交：工作树干净。
	repo := first.repo
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("读取工作树失败: %v", err)
	}
	status, err := wt.Status()
	if err != nil {
		t.Fatalf("读取状态失败: %v", err)
	}
	if !status.IsClean() {
		t.Fatalf("重启后应提交遗留改动，实际仍有未提交文件: %v", status)
	}
	if _, err := fmt.Sscanf(first.Head(), "%x", new(string)); err != nil {
		_ = err // head 格式不是这里的关注点，仅确保能取到
	}
}
