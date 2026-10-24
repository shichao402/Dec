package audit

import (
	"os"
	"testing"
	"time"
)

// 审计分级的落点测试（ADR 0043）。
//
// 这些用例要守的不是"能不能聚合"，而是"聚合不能越过该保的东西"：
// 安全级与事实级一条都不能少，只有访问级允许减量。

// TestSecurityTierNeverSuppressed 验证安全级事件在任意高频下都逐条落盘。
//
// 这是最重要的一条：密钥明文查看、设备吊销、根密钥轮换这类事件
// 一旦被聚合或丢弃，审计就失去了取证价值。哪怕同一秒来一万条也必须全在。
func TestSecurityTierNeverSuppressed(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}

	at := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	// 远超访问级限额（20/小时），足以证明安全级不受限额影响。
	// 不取更大的数：每条都要 fsync，纯为断言而拉长测试时间没有意义。
	const n = 300
	for i := 0; i < n; i++ {
		if err := log.Append(Entry{
			At:       at.Add(time.Duration(i) * time.Millisecond),
			Event:    EventSecretRead,
			DeviceID: "dev-a",
			Source:   "admin",
			Subject:  "db-password",
			OK:       true,
		}); err != nil {
			t.Fatalf("第 %d 条写入失败: %v", i, err)
		}
	}
	if err := log.Flush(); err != nil {
		t.Fatalf("冲写失败: %v", err)
	}

	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(items) != n {
		t.Fatalf("安全级事件必须逐条保留：应 %d 条，实际 %d 条", n, len(items))
	}
	for _, it := range items {
		if it.Count > 1 {
			t.Fatalf("安全级事件不应出现聚合记录: %+v", it)
		}
	}
}

// TestFactTierNeverSuppressed 验证业务事实级事件也逐条落盘。
func TestFactTierNeverSuppressed(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}

	at := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	// 同样远超访问级上限，证明事实级不受访问级限额影响。
	const n = 200
	for i := 0; i < n; i++ {
		if err := log.Append(Entry{
			At:      at.Add(time.Duration(i) * time.Millisecond),
			Event:   EventAssetWrite,
			Subject: "core",
			OK:      true,
		}); err != nil {
			t.Fatalf("第 %d 条写入失败: %v", i, err)
		}
	}
	if err := log.Flush(); err != nil {
		t.Fatalf("冲写失败: %v", err)
	}

	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(items) != n {
		t.Fatalf("事实级事件必须逐条保留：应 %d 条，实际 %d 条", n, len(items))
	}
}

// TestAccessTierIsBoundedButAccounted 验证访问级事件被限量，但条数可还原。
//
// 两个断言缺一不可：
//   - 落盘条数有上限，不会随请求数增长（这是那次写满磁盘的直接原因）；
//   - 放行数 + 聚合数 == 请求数，"发生过多少次"不能凭空消失。
func TestAccessTierIsBoundedButAccounted(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}

	at := time.Date(2026, 10, 24, 11, 0, 0, 0, time.UTC)
	const n = 2000
	for i := 0; i < n; i++ {
		if err := log.Append(Entry{
			At:       at.Add(time.Duration(i) * time.Millisecond),
			Event:    EventSync,
			DeviceID: "dev-a",
			Source:   "agent",
			OK:       true,
		}); err != nil {
			t.Fatalf("第 %d 条写入失败: %v", i, err)
		}
	}
	// Close 会把剩余聚合计数补成汇总记录（Flush 不做这件事，
	// 否则周期性冲写会让每条被抑制的记录都变成一条 count=1 的汇总）。
	if err := log.Close(); err != nil {
		t.Fatalf("收尾失败: %v", err)
	}

	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	// 落盘条数：最多放行 MaxAccessRecordsPerHour 条，外加一条汇总。
	if len(items) > MaxAccessRecordsPerHour+1 {
		t.Fatalf("%d 次下发落盘了 %d 条，超过上限 %d+1",
			n, len(items), MaxAccessRecordsPerHour)
	}
	// 条数必须可还原。
	total := 0
	for _, it := range items {
		if it.Count > 1 {
			total += it.Count
			continue
		}
		total++
	}
	if total != n {
		t.Fatalf("还原出 %d 条，应为 %d 条（聚合不能让计数消失）", total, n)
	}
}

// TestFlushDoesNotResettleAggregates 验证周期性 Flush 不会把聚合计数重新写回去。
//
// 这是 2026-10-24 端到端测试发现的真实缺陷：原先 Flush 每次都把 suppressed
// 结算成一条汇总，于是在"每写一条就 Flush"的场景下，每条被抑制的记录
// 都会变成一条 count=1 的汇总重新落盘，聚合等于没做
// （实测 200 次下发仍落盘 200 条，而上限是 20）。
//
// 正确的结算时机只有两个：小时切换（Offer 内部）与进程退出（Close）。
func TestFlushDoesNotResettleAggregates(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}

	at := time.Date(2026, 10, 24, 13, 0, 0, 0, time.UTC)
	// 每写一条就 Flush 一次，模拟"冷备推送很频繁"的最坏情况。
	const n = 200
	for i := 0; i < n; i++ {
		log.Append(Entry{
			At:       at.Add(time.Duration(i) * time.Millisecond),
			Event:    EventSync,
			DeviceID: "dev-a",
			Source:   "agent",
		})
		if err := log.Flush(); err != nil {
			t.Fatalf("第 %d 次冲写失败: %v", i, err)
		}
	}

	// Flush 阶段不应产生任何汇总：此时文件里只该有放行的那些条。
	mid, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(mid) > MaxAccessRecordsPerHour {
		t.Fatalf("Flush 期间落盘了 %d 条，超过上限 %d：Flush 不应结算聚合",
			len(mid), MaxAccessRecordsPerHour)
	}

	// 进程退出时结算，条数必须能还原。
	if err := log.Close(); err != nil {
		t.Fatalf("收尾失败: %v", err)
	}
	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	raw, restored := 0, 0
	for _, it := range items {
		if it.Count > 1 {
			restored += it.Count
			continue
		}
		raw++
		restored++
	}
	if raw > MaxAccessRecordsPerHour {
		t.Fatalf("落盘 %d 条原始记录，超过上限 %d", raw, MaxAccessRecordsPerHour)
	}
	if restored != n {
		t.Fatalf("还原 %d 条，应为 %d 条（Close 必须把聚合计数落盘）", restored, n)
	}
}

// TestAccessTierKeepsDifferentDevices 验证聚合不跨设备合并。
func TestAccessTierKeepsDifferentDevices(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}

	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	// 把 dev-a 的额度用满。
	for i := 0; i < MaxAccessRecordsPerHour*3; i++ {
		log.Append(Entry{At: at.Add(time.Duration(i) * time.Millisecond), Event: EventSync, DeviceID: "dev-a"})
	}
	// dev-b 首次必须仍然落盘。
	log.Append(Entry{At: at.Add(time.Second), Event: EventSync, DeviceID: "dev-b"})
	if err := log.Flush(); err != nil {
		t.Fatalf("冲写失败: %v", err)
	}

	items, err := log.Read(at)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	seenB := false
	for _, it := range items {
		if it.DeviceID == "dev-b" {
			seenB = true
		}
	}
	if !seenB {
		t.Fatal("dev-b 的下发必须留下记录：聚合不能跨设备合并")
	}
}

// TestDroppedCountsFailures 验证写入失败会被计数而不是静默消失。
//
// 审计写不进去不阻断业务（这是有意的取舍），但"有洞"这件事必须可见。
func TestDroppedCountsFailures(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatalf("打开审计仓库失败: %v", err)
	}
	if log.Dropped() != 0 {
		t.Fatalf("初始不应有丢弃，实际 %d", log.Dropped())
	}
	// 人为让目录不可写：把 events 建在文件位置上，MkdirAll 必然失败。
	blocker := dir + "/events"
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("准备阻塞文件失败: %v", err)
	}
	if err := log.Append(Entry{At: time.Now().UTC(), Event: EventAssetWrite, OK: true}); err == nil {
		t.Fatal("目录不可写时应返回错误")
	}
	if log.Dropped() != 1 {
		t.Fatalf("失败应被计数，实际 %d", log.Dropped())
	}
}

// TestTierDefaults 验证未知事件默认按访问级处理。
//
// 默认从严是刻意的：新事件在证明自己低频之前，先按"可能刷屏"对待，
// 避免再出现一次"把高频事件当成事实级而写满磁盘"。
func TestTierDefaults(t *testing.T) {
	if got := Event("some.new.event").Tier(); got != TierAccess {
		t.Fatalf("未知事件应按访问级处理，实际 %s", got)
	}
	cases := map[Event]Tier{
		EventSecretRead:    TierSecurity,
		EventSecretDeliver: TierSecurity,
		EventRevoke:        TierSecurity,
		EventDeploy:        TierSecurity,
		EventAssetWrite:    TierFact,
		EventSecretWrite:   TierSecurity,
		EventSync:          TierAccess,
		EventDownload:      TierAccess,
	}
	for e, want := range cases {
		if got := e.Tier(); got != want {
			t.Fatalf("%s 应为 %s，实际 %s", e, want, got)
		}
	}
}
