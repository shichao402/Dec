package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 审计文件必须按小时分片。
//
// 为什么这条约束值得单独锁住（2026-10-24 生产事故）：
// 原先按天一个文件。审计是高流量追加，一天能到 9MB；而 git 存的是文件全量快照，
// 于是每提交一次就要重新存一遍整个 9MB 文件。45 分钟里 .git 从 15M 长到 82M，
// 内存被顶到几乎耗尽（可用 66MB、swap 2G 全满），推送还因为打包松散对象反复失败。
//
// 分片之后，一次提交只影响当前这一个小时的文件，历史小时不再重存。

// TestFilesAreHourlySharded 验证写入会落到小时分片文件里。
func TestFilesAreHourlySharded(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	base := time.Date(2026, 10, 24, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * 20 * time.Minute)
		if err := log.Append(Entry{At: at, Event: EventSync, OK: true}); err != nil {
			t.Fatalf("追加失败: %v", err)
		}
	}
	if err := log.Flush(); err != nil {
		t.Fatalf("冲写失败: %v", err)
	}

	files, err := log.Files()
	if err != nil {
		t.Fatalf("列出文件失败: %v", err)
	}
	// 03:00、03:20、03:40 应落在同一个小时文件里。
	want := "events/2026-10/2026-10-24T03.hour.jsonl"
	found := false
	for _, f := range files {
		if f == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("应有小时分片 %s，实际: %v", want, files)
	}
}

// TestShardsSplitAcrossHours 验证跨小时会分到不同文件。
func TestShardsSplitAcrossHours(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	// 每小时的量级不必很大，这里只验证边界。
	for _, h := range []int{3, 4} {
		at := time.Date(2026, 10, 24, h, 10, 0, 0, time.UTC)
		if err := log.Append(Entry{At: at, Event: EventSync, OK: true}); err != nil {
			t.Fatalf("追加失败: %v", err)
		}
	}
	if err := log.Flush(); err != nil {
		t.Fatalf("冲写失败: %v", err)
	}

	files, err := log.Files()
	if err != nil {
		t.Fatalf("列出文件失败: %v", err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f] = true
	}
	for _, h := range []string{"03", "04"} {
		want := fmt.Sprintf("events/2026-10/2026-10-24T%s.hour.jsonl", h)
		if !got[want] {
			t.Fatalf("缺少小时分片 %s，实际: %v", want, files)
		}
	}
}

// TestReadSpansHourlyShards 验证 Read 能把一天内多个小时分片合并读回。
func TestReadSpansHourlyShards(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	day := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	total := 0
	for _, h := range []int{3, 5, 9} {
		for i := 0; i < 2; i++ {
			at := day.Add(time.Duration(h)*time.Hour + time.Duration(i)*time.Minute)
			if err := log.Append(Entry{At: at, Event: EventSync, OK: true, Detail: fmt.Sprintf("%d-%d", h, i)}); err != nil {
				t.Fatalf("追加失败: %v", err)
			}
			total++
		}
	}
	if err := log.Flush(); err != nil {
		t.Fatalf("冲写失败: %v", err)
	}

	got, err := log.Read(day)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(got) != total {
		t.Fatalf("应读回 %d 条，实际 %d 条", total, len(got))
	}
	// 顺序必须是时间序：03 → 05 → 09。
	wantHours := []int{3, 3, 5, 5, 9, 9}
	for i, e := range got {
		if int(e.At.Hour()) != wantHours[i] {
			t.Fatalf("第 %d 条小时为 %d，期望 %d（顺序应跨分片保持时间序）", i, e.At.Hour(), wantHours[i])
		}
	}
}

// TestReadStillFindsLegacyDailyFile 验证旧的按天文件仍能读出来。
//
// 兼容不是可有可无的：2026-10-24 之前的历史审计全在 events/YYYY-MM/YYYY-MM-DD.jsonl 里，
// 改成分片后如果读不到，等于把已有档案变成不可读。
func TestReadStillFindsLegacyDailyFile(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	// 直接按旧路径写一条，模拟升级前的数据。
	legacy := fmt.Sprintf("events/%s/%s.jsonl", day.Format("2006-01"), day.Format("2006-01-02"))
	full := filepath.Join(dir, filepath.FromSlash(legacy))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	line := `{"at":"2026-10-22T10:00:00Z","event":"sync","ok":true}` + "\n"
	if err := os.WriteFile(full, []byte(line), 0o600); err != nil {
		t.Fatalf("写入旧格式失败: %v", err)
	}

	got, err := log.Read(day)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("旧格式应能读出 1 条，实际 %d 条", len(got))
	}
}
