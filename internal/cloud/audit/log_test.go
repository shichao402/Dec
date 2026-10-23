package audit

import (
	"strings"
	"testing"
	"time"
)

// 审计的核心不变量只有一条：只追加。
//
// 所以测试重点不是“能不能写”，而是“写了之后能不能原样读回来、
// 同一秒的重复事件会不会丢、跨月分文件会不会串”。

// TestAppendAndRead 验证写入后能读回，且字段完整。
func TestAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}

	at := time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)
	entry := Entry{
		At:       at,
		Event:    EventSecretRead,
		DeviceID: "dev-a",
		Source:   "admin",
		Subject:  "db-password",
		Detail:   "显式查看明文",
		OK:       true,
		RemoteIP: "10.0.0.1",
	}
	if err := log.Append(entry); err != nil {
		t.Fatalf("写入审计失败: %v", err)
	}

	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取审计失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("应有 1 条记录，实际 %d", len(items))
	}
	got := items[0]
	if got.Event != EventSecretRead {
		t.Fatalf("事件类型为 %q", got.Event)
	}
	if got.DeviceID != "dev-a" || got.Source != "admin" {
		t.Fatalf("设备与来源记录错误: %+v", got)
	}
	if got.Subject != "db-password" {
		t.Fatalf("对象记录错误: %q", got.Subject)
	}
	if !got.OK {
		t.Fatal("OK 应为 true")
	}
}

// TestAppendIsAppendOnly 验证多次写入不会互相覆盖。
func TestAppendIsAppendOnly(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}

	at := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	for _, sub := range []string{"a", "b", "c"} {
		if err := log.Append(Entry{At: at, Event: EventAssetWrite, Subject: sub, OK: true}); err != nil {
			t.Fatalf("写入 %s 失败: %v", sub, err)
		}
	}

	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("应有 3 条，实际 %d", len(items))
	}
	for i, want := range []string{"a", "b", "c"} {
		if items[i].Subject != want {
			t.Fatalf("第 %d 条为 %q，期望 %q（顺序被破坏）", i, items[i].Subject, want)
		}
	}
}

// TestSameSecondNotDropped 验证同一秒的重复事件不会被 git 当成“无变更”吞掉。
//
// 这是真实风险：如果提交依赖文件内容变化，同一秒写两条相同记录
// 第二条就会因为“没有可提交的变更”而消失。
func TestSameSecondNotDropped(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	at := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := log.Append(Entry{At: at, Event: EventDownload, Subject: "dec-agent", OK: true}); err != nil {
			t.Fatalf("第 %d 次写入失败: %v", i+1, err)
		}
	}
	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("同一秒的 3 条记录应全部保留，实际 %d", len(items))
	}
}

// TestReadMissingDay 验证没有记录的日期返回空而不是错误。
func TestReadMissingDay(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	items, err := log.Read(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("空日期不应报错: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("应为空，实际 %d 条", len(items))
	}
}

// TestAppendCommit 验证把业务 revision 绑进审计。
func TestAppendCommit(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	if err := log.AppendCommit(Entry{At: at, Event: EventAssetWrite, Subject: "core", OK: true}, "abc123"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(items) != 1 || !strings.Contains(items[0].Detail, "revision=abc123") {
		t.Fatalf("应记录 revision，实际 %+v", items)
	}
}

// TestHeadAdvances 验证提交后 head 前进。
//
// 2026-10-23 起提交按时间窗合并（见 CommitWindow），Append 不再每次都 commit，
// 所以这里显式 Flush 一次再断言。窗口内的记录仍在工作树里、已 fsync 落盘，
// 只是还没记进历史。
func TestHeadAdvances(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	first := log.Head()
	at := time.Date(2026, 10, 21, 13, 0, 0, 0, time.UTC)
	if err := log.Append(Entry{At: at, Event: EventBootstrap, OK: true}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := log.Flush(); err != nil {
		t.Fatalf("冲写失败: %v", err)
	}
	if log.Head() == first {
		t.Fatal("冲写后 head 应前进")
	}
}

// TestFilesIncludeEvents 验证审计文件出现在仓库树里，能被冷备推走。
func TestFilesIncludeEvents(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	at := time.Date(2026, 10, 21, 14, 0, 0, 0, time.UTC)
	if err := log.Append(Entry{At: at, Event: EventDeploy, OK: true}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	// 提交按窗口合并，Files 读的是已提交的树，先冲写一次。
	if err := log.Flush(); err != nil {
		t.Fatalf("冲写失败: %v", err)
	}
	files, err := log.Files()
	if err != nil {
		t.Fatalf("读取文件列表失败: %v", err)
	}
	found := false
	for _, f := range files {
		if strings.Contains(f, "2026-10-21.jsonl") {
			found = true
		}
	}
	if !found {
		t.Fatalf("审计文件应在仓库树里，实际 %v", files)
	}
}

// TestMonthPartition 验证按月份分目录。
func TestMonthPartition(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	oct := time.Date(2026, 10, 31, 23, 0, 0, 0, time.UTC)
	nov := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	if err := log.Append(Entry{At: oct, Event: EventSync, OK: true}); err != nil {
		t.Fatalf("写入 10 月失败: %v", err)
	}
	if err := log.Append(Entry{At: nov, Event: EventSync, OK: true}); err != nil {
		t.Fatalf("写入 11 月失败: %v", err)
	}
	// 同上：Files 读已提交的树。
	if err := log.Flush(); err != nil {
		t.Fatalf("冲写失败: %v", err)
	}
	files, err := log.Files()
	if err != nil {
		t.Fatalf("读取文件列表失败: %v", err)
	}
	joined := strings.Join(files, "\n")
	if !strings.Contains(joined, "events/2026-10/2026-10-31.jsonl") {
		t.Fatalf("缺少 10 月文件: %s", joined)
	}
	if !strings.Contains(joined, "events/2026-11/2026-11-01.jsonl") {
		t.Fatalf("缺少 11 月文件: %s", joined)
	}
}
