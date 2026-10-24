package audit

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
)

// 提交节奏：按小时收口。
//
// 为什么这条值得单独守：仓库体积主要由「提交次数」决定，而不是「记录条数」。
// git 存的是文件快照，同一个小时文件每提交一次就多存一份完整副本。
//
// 2026-10-24 实测（TestMeasureAnnualGrowth，24 小时打满全局限额）：
//
//	每条都提交      → 1440 次提交，体积是数据的 4.1 倍，每年 354 MB
//	按小时收口      → 24 次提交，1.1 倍，每年 96 MB
//
// 线上也有直接证据：events/2026-10/2026-10-23T20.hour.jsonl 被提交了 594 次，
// 而当它只有 660 KB。2026-10-23 那次 5.7G 的 .git 正是这样堆出来的。
//
// 所以这里断言的是「一个小时间片只提交一次」，不是「某段时间内提交少于某个数」。
// 前者是机制，后者会随测试时长漂移。

// TestCommitOncePerHour 验证一个小时间片只产生一次提交。
func TestCommitOncePerHour(t *testing.T) {
	log, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	// 关掉两个兜底触发，让"按小时收口"成为唯一路径。
	log.commitWindow = 100 * time.Hour
	prev := commitBatchForTest
	commitBatchForTest = 1 << 30
	defer func() { commitBatchForTest = prev }()

	base := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	// 写满三个小时，每小时 20 条。
	const perHour = 20
	for h := 0; h < 3; h++ {
		for i := 0; i < perHour; i++ {
			if err := log.Append(Entry{
				At:    base.Add(time.Duration(h)*time.Hour + time.Duration(i)*time.Minute),
				Event: EventAssetWrite,
				OK:    true,
			}); err != nil {
				t.Fatalf("写入失败: %v", err)
			}
		}
	}
	// 收尾会把最后一个小时提交掉。
	if err := log.Close(); err != nil {
		t.Fatalf("收尾失败: %v", err)
	}

	// 3 个小时片 → 3 次提交（第 2、3 小时的写入各触发一次上小时收口，
	// 第 3 小时由 Close 收口）。
	commits := countFromRepo(t, log.Dir())
	if commits != 3 {
		t.Fatalf("3 个小时间片应产生 3 次提交，实际 %d 次；"+
			"提交次数直接决定仓库体积，请勿改回按秒提交", commits)
	}
}

// TestCommitNotPerRecord 验证同一小时内连续写入不会一条一提交。
//
// 这是上一条的反面：即使写很多条，只要还在同一小时里，提交数都应该很小。
func TestCommitNotPerRecord(t *testing.T) {
	log, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	log.commitWindow = 100 * time.Hour
	prev := commitBatchForTest
	commitBatchForTest = 1 << 30
	defer func() { commitBatchForTest = prev }()

	base := time.Date(2026, 10, 24, 3, 0, 0, 0, time.UTC)
	// 同一小时内写 100 条。
	for i := 0; i < 100; i++ {
		if err := log.Append(Entry{
			At:    base.Add(time.Duration(i) * 30 * time.Second),
			Event: EventAssetWrite,
			OK:    true,
		}); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("收尾失败: %v", err)
	}

	commits := countFromRepo(t, log.Dir())
	if commits > 1 {
		t.Fatalf("同一小时内应只提交 1 次，实际 %d 次：每多一次就多存一份完整文件快照", commits)
	}
	// 记录一条都不能少。
	items, err := log.Read(base)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(items) != 100 {
		t.Fatalf("记录应 100 条，实际 %d 条", len(items))
	}
}

// TestCommitAtHourBoundaryCommitsBeforeWrite 验证跨小时时先提交再写。
//
// 顺序很关键：如果先写新记录再提交，新小时文件里已有内容，
// 这次提交会把它快照进去，等下个小时收口时再存一遍——
// 每份文件都被存两遍，白放大一倍。
func TestCommitAtHourBoundaryCommitsBeforeWrite(t *testing.T) {
	log, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	log.commitWindow = 100 * time.Hour
	prev := commitBatchForTest
	commitBatchForTest = 1 << 30
	defer func() { commitBatchForTest = prev }()

	base := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	// 第一小时最后一条。
	if err := log.Append(Entry{At: base.Add(59 * time.Minute), Event: EventAssetWrite, OK: true}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	// 跨进第二小时的第一条：此时应先把第一小时提交掉，再写这条。
	if err := log.Append(Entry{At: base.Add(time.Hour), Event: EventAssetWrite, OK: true}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	// 第一小时的文件此刻应当已经进历史，且历史里那一版只有一条记录。
	// 注意不能用 log.Read(base)：它读的是当天所有小时文件，会把第二小时的记录也算进来。
	hourFile := filepath.Join("events", "2026-10", "2026-10-24T05.hour.jsonl")
	committed, err := committedFileContent(log.Dir(), hourFile)
	if err != nil {
		t.Fatalf("读取已提交内容失败: %v", err)
	}
	if n := countLines(committed); n != 1 {
		t.Fatalf("跨小时时应先提交第一小时再写新记录，历史里该文件应有 1 条，实际 %d 条；"+
			"说明提交发生在写入之后，新小时文件被提前快照了", n)
	}
}

// TestCommitWindowIsFallback 验证窗口是兜底而非主策略。
//
// 主策略是按小时收口。窗口只防"一直不跨小时"这种异常，
// 所以它必须比一个分片周期长——否则会在同一小时内反复提交，
// 又退回到快照放大的老问题。
func TestCommitWindowIsFallback(t *testing.T) {
	if CommitWindow <= time.Hour {
		t.Fatalf("CommitWindow = %v，必须大于一个小时间片；"+
			"小于等于一小时会让同一文件在小时内被反复提交", CommitWindow)
	}
}

// TestCommitBatchStillBursts 验证批量阈值仍在起作用。
//
// 按小时收口不等于"一小时只提交一次"：高峰时段（一次故障刷出几千条）
// 应该按批提交，不能让未提交内容堆到小时末。
func TestCommitBatchStillBursts(t *testing.T) {
	log, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	// 保留默认批量阈值，只把窗口拉长以排除它的影响。
	log.commitWindow = 100 * time.Hour

	base := time.Date(2026, 10, 24, 7, 0, 0, 0, time.UTC)
	// 同一小时内写满一个批量再多一点。
	n := CommitBatch + 10
	for i := 0; i < n; i++ {
		if err := log.Append(Entry{
			At:    base.Add(time.Duration(i) * time.Second),
			Event: EventAssetWrite,
			OK:    true,
		}); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	commits := countFromRepo(t, log.Dir())
	if commits < 1 {
		t.Fatalf("写满 %d 条后应已按批提交，实际 %d 次提交", n, commits)
	}
	if got := log.Dropped(); got != 0 {
		t.Fatalf("出现 %d 条丢弃", got)
	}
}

// committedFileContent 从 HEAD 读取某个文件的已提交内容。
//
// 用来区分"工作树里的内容"与"已经进历史的内容"——跨小时收口验证的正是后者：
// 该文件在那一刻是否已经定稿并提交。
func committedFileContent(dir, rel string) (string, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", err
	}
	head, err := repo.Head()
	if err != nil {
		// 还没有任何提交：返回空，由调用方按 0 条处理。
		return "", nil
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return "", err
	}
	f, err := commit.File(rel)
	if err != nil {
		return "", nil
	}
	return f.Contents()
}

// countLines 数非空行数，用于统计 jsonl 记录条数。
func countLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
