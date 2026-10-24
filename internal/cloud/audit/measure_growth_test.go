package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// 年增量实验：把「一年会长多大」量出来，而不是估。
//
// 为什么必须量：限额只约束了「写多少条记录」，但仓库体积还取决于
// 「这些记录被切成多少次提交」——git 存的是文件快照，每提交一次就把当时
// 整个小时文件重存一遍。2026-10-23 那次 76,350 个提交把 .git 撑到 5.7G，
// 主因正是这个，而不是记录条数本身。
//
// 这里不手动 Flush：完全按生产策略（按小时收口 + 批量 + 窗口兜底）走，
// 量出来的才是真实行为。对照组用 oldEveryCommit 模拟"每条都提交"的旧行为。
func TestMeasureAnnualGrowth(t *testing.T) {
	if os.Getenv("DEC_MEASURE_GROWTH") == "" {
		t.Skip("设 DEC_MEASURE_GROWTH=1 才跑：这是个实验，不是常规测试")
	}

	t.Run("旧行为：每条都提交", func(t *testing.T) { measureOne(t, true) })
	t.Run("现行为：按小时收口", func(t *testing.T) { measureOne(t, false) })
}

// measureOne 按固定速率造 24 小时数据。
//
// 速率取「打满全局限额」：每小时 MaxAccessRecordsPerHourTotal 条，
// 均匀铺满一小时。这是最坏情况，正常使用远低于它。
func measureOne(t *testing.T, everyCommit bool) {
	const perHour = MaxAccessRecordsPerHourTotal
	const hours = 24

	ipCount := (perHour + MaxAccessRecordsPerHour - 1) / MaxAccessRecordsPerHour

	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	// 关掉两个兜底触发（批量与窗口），让"按小时收口"成为唯一策略，
	// 否则测出来的仍是兜底路径。
	log.commitWindow = 100 * time.Hour
	prevBatch := commitBatchForTest
	commitBatchForTest = 1 << 30
	defer func() { commitBatchForTest = prevBatch }()

	base := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	var records int
	for h := 0; h < hours; h++ {
		hourAt := base.Add(time.Duration(h) * time.Hour)
		for i := 0; i < perHour; i++ {
			if err := log.Append(Entry{
				At:       hourAt.Add(time.Duration(i) * time.Hour / time.Duration(perHour)),
				Event:    EventDownload,
				Source:   "public",
				Subject:  "dec-agent-linux-amd64",
				Detail:   "版本 v2.1.17，9.3 MB",
				OK:       true,
				RemoteIP: fmt.Sprintf("203.0.113.%d", i%ipCount),
			}); err != nil {
				t.Fatalf("写入失败: %v", err)
			}
			records++
			if everyCommit {
				if err := log.Flush(); err != nil {
					t.Fatalf("提交失败: %v", err)
				}
			}
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("收尾失败: %v", err)
	}

	work := workTreeBytes(t, dir)
	before := dirSize(t, dir)
	if err := log.Gc(); err != nil {
		t.Fatalf("gc 失败: %v", err)
	}
	after := dirSize(t, dir)
	commits := countFromRepo(t, dir)

	years := 365.0 * 24 / float64(hours)
	t.Logf("%d 小时 × %d 条 = %d 条记录，实际 %d 次提交（工作树数据 %.1f KB）",
		hours, perHour, records, commits, float64(work)/1024)
	t.Logf("  gc 前 %.2f MB → gc 后 %.2f MB，体积/数据 = %.1f 倍",
		mb(before), mb(after), float64(after)/float64(work))
	t.Logf("  每年 %.0f MB，%.1f 万次提交", mb(after)*years, float64(commits)*years/10000)
}

func workTreeBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if len(rel) >= 4 && rel[:4] == ".git" {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func mb(v int64) float64 { return float64(v) / 1024 / 1024 }

func dirSize(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

// countFromRepo 数审计仓库的提交数。
func countFromRepo(t *testing.T, dir string) int {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("打开仓库失败: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		return 0
	}
	iter, err := repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		t.Fatalf("读取提交历史失败: %v", err)
	}
	defer iter.Close()
	n := 0
	if err := iter.ForEach(func(*object.Commit) error { n++; return nil }); err != nil {
		t.Fatalf("遍历提交失败: %v", err)
	}
	return n
}
