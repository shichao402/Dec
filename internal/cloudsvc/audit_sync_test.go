package cloudsvc

import (
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/audit"
)

// sync 审计降噪的边界测试。
//
// 背景（2026-10-23 生产故障）：说明行导致的无限重写把审计刷到 8.4 万条
// sync 记录，真正有用的信息被淹没。降噪的目的不是"少记日志"，
// 而是让"每轮都重发"这种异常在日志量级上就看得出来。
//
// 降噪已经从"按内容指纹 + 30 分钟静默期"改为日志层的按小时分桶聚合
// （见 internal/cloud/audit/aggregate.go、ADR 0043）。换掉的原因：
// 指纹方案在"内容每轮都变"的故障后期会完全失效，而按小时分桶是有界的，
// 上限只由配置决定，与请求量无关。
//
// 这里守三条不变量：
//  1. 同一设备一小时内的重复下发，落盘条数是常数上限，不随请求数增长；
//  2. 被聚合掉的条数必须以 count 留下，量级信号不能丢；
//  3. 跨设备不合并——"这份内容送到过哪些机器"必须还能回答。

// syncEntry 构造一条下发审计记录。
func syncEntry(at time.Time, device string) audit.Entry {
	return audit.Entry{
		At:       at,
		Event:    audit.EventSync,
		DeviceID: device,
		Source:   "agent",
		Subject:  "rev-1",
		Detail:   "下发 3 个文件、删除 0 个",
		OK:       true,
	}
}

// TestSyncAuditIsBoundedPerHour 验证高频下发不会把审计刷爆。
//
// 这是那次故障的直接回归测试：一秒一轮、跑一小时是 3600 次下发，
// 但审计里必须只剩常数条（放行阈值 + 一条汇总）。
func TestSyncAuditIsBoundedPerHour(t *testing.T) {
	agg := audit.NewAccessAggregator()
	hour := time.Date(2026, 10, 23, 21, 0, 0, 0, time.UTC)

	const requests = 3600
	emitted := 0
	var summaries []audit.Entry
	for i := 0; i < requests; i++ {
		at := hour.Add(time.Duration(i) * time.Second)
		d := agg.Offer(syncEntry(at, "dev-a"))
		if d.Emit {
			emitted++
		}
		summaries = append(summaries, d.Summaries...)
	}

	if emitted > audit.MaxAccessRecordsPerHour {
		t.Fatalf("%d 次下发放行了 %d 条，超过上限 %d",
			requests, emitted, audit.MaxAccessRecordsPerHour)
	}
	if emitted == 0 {
		t.Fatal("至少应放行一条：完全静默会让审计看不出这台设备在对账")
	}
	// 同一小时内的聚合不会在中途产出汇总（只有跨小时才结算），
	// 所以这里应当没有汇总记录。
	if len(summaries) != 0 {
		t.Fatalf("同一小时内不应产生跨小时汇总，实际 %d 条", len(summaries))
	}

	// 关停时把剩余计数补成汇总，条数必须对得上：放行 + 聚合 = 总请求数。
	left := agg.Take()
	totalSuppressed := 0
	for _, s := range left {
		totalSuppressed += s.Count
	}
	if emitted+totalSuppressed != requests {
		t.Fatalf("放行 %d + 聚合 %d = %d，与请求数 %d 不符（有记录被静默丢弃）",
			emitted, totalSuppressed, emitted+totalSuppressed, requests)
	}
}

// TestSyncAuditSummaryCarriesCount 验证聚合条数会以 count 留下。
//
// 这是"量级信号不能丢"的落点：一万条合并成一条之后，
// 事后仍要能看出这是一万次而不是一次。
func TestSyncAuditSummaryCarriesCount(t *testing.T) {
	agg := audit.NewAccessAggregator()
	hour := time.Date(2026, 10, 23, 21, 0, 0, 0, time.UTC)

	// 先填满本小时的放行额度。
	for i := 0; i < audit.MaxAccessRecordsPerHour; i++ {
		agg.Offer(syncEntry(hour.Add(time.Duration(i)*time.Second), "dev-a"))
	}
	// 再多来 500 条，全部应被聚合。
	const extra = 500
	for i := 0; i < extra; i++ {
		agg.Offer(syncEntry(hour.Add(time.Duration(i)*time.Millisecond), "dev-a"))
	}

	left := agg.Take()
	if len(left) != 1 {
		t.Fatalf("应合并成 1 条汇总，实际 %d 条", len(left))
	}
	if left[0].Count != extra {
		t.Fatalf("汇总应记录 %d 条，实际 %d", extra, left[0].Count)
	}
	if left[0].Event != audit.EventSync || left[0].DeviceID != "dev-a" {
		t.Fatalf("汇总应保留事件与设备信息，实际 %+v", left[0])
	}
}

// TestSyncAuditSeparatesDevices 验证聚合按设备隔离。
//
// 同一份内容推给两台设备是两条独立的事实，合并会直接毁掉
// "这份内容送到过哪些机器"这个取证能力。
func TestSyncAuditSeparatesDevices(t *testing.T) {
	agg := audit.NewAccessAggregator()
	hour := time.Date(2026, 10, 23, 21, 0, 0, 0, time.UTC)

	// 把 dev-a 的额度用满。
	for i := 0; i < audit.MaxAccessRecordsPerHour; i++ {
		agg.Offer(syncEntry(hour, "dev-a"))
	}
	// dev-b 的第一条必须仍然放行：它不应该被 dev-a 的用量影响。
	d := agg.Offer(syncEntry(hour, "dev-b"))
	if !d.Emit {
		t.Fatal("dev-b 首次下发应放行，不应被 dev-a 的聚合影响")
	}
}

// TestSyncAuditReopensNextHour 验证跨小时重新放行。
//
// 降噪不能变成"这台设备以后永远不再记"：每天正常对账都应该留下痕迹，
// 否则审计里看不出设备还活着。
func TestSyncAuditReopensNextHour(t *testing.T) {
	agg := audit.NewAccessAggregator()
	hour := time.Date(2026, 10, 23, 21, 0, 0, 0, time.UTC)

	for i := 0; i < audit.MaxAccessRecordsPerHour; i++ {
		agg.Offer(syncEntry(hour, "dev-a"))
	}
	if d := agg.Offer(syncEntry(hour, "dev-a")); d.Emit {
		t.Fatal("本小时额度已满，不应继续放行")
	}

	// 进入下一小时：应重新放行，并把上一小时的聚合计数结算成汇总。
	next := hour.Add(time.Hour)
	d := agg.Offer(syncEntry(next, "dev-a"))
	if !d.Emit {
		t.Fatal("新的一小时应重新放行")
	}
	if len(d.Summaries) != 1 {
		t.Fatalf("跨小时应结算出 1 条汇总，实际 %d 条", len(d.Summaries))
	}
	if d.Summaries[0].Count != 1 {
		t.Fatalf("上小时被聚合 1 条，汇总应记 1，实际 %d", d.Summaries[0].Count)
	}
	// 汇总必须落在上一小时的分片里，否则跨小时的计数会写进错误文件。
	if got := d.Summaries[0].At.Truncate(time.Hour); !got.Equal(hour) {
		t.Fatalf("汇总应落在 %s，实际 %s", hour, got)
	}
}

// TestSyncAuditIgnoresRevisionForBucketing 验证 revision 变化不会让计数桶增殖。
//
// 这是旧指纹方案失效的那个场景：故障后期每轮下发的 revision 都可能不同。
// 如果聚合键带上 revision，每个新 revision 都会开一个新桶，
// 降噪等于没有。
func TestSyncAuditIgnoresRevisionForBucketing(t *testing.T) {
	agg := audit.NewAccessAggregator()
	hour := time.Date(2026, 10, 23, 21, 0, 0, 0, time.UTC)

	emitted := 0
	for i := 0; i < 1000; i++ {
		e := syncEntry(hour, "dev-a")
		// 每轮 revision 都不同，模拟内容持续变化的失控场景。
		e.Subject = "rev-" + time.Duration(i).String()
		if agg.Offer(e).Emit {
			emitted++
		}
	}
	if emitted > audit.MaxAccessRecordsPerHour {
		t.Fatalf("revision 变化时放行了 %d 条，超过上限 %d：聚合键不应包含 revision",
			emitted, audit.MaxAccessRecordsPerHour)
	}
}
