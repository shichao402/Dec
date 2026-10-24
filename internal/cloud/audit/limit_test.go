package audit

import (
	"fmt"
	"testing"
	"time"
)

// 访问级事件的硬上限测试（ADR 0043）。
//
// 单键限额只解决"同一台设备疯狂重发"，解决不了"请求方不断换身份"。
// 下载端点是公网匿名接口，聚合键里带 IP，而 IP 可以随意轮换；
// 所以必须还有全局限额与桶数上限，才算真的封死。
//
// 这组用例守的命题只有一句：无论输入长什么样，一小时落盘的条数都是常数。

// TestGlobalLimitStopsKeyRotation 验证换 IP 轮换也无法突破全局上限。
//
// 这是单键限额的漏网场景：每来一个不同 IP 就开一个新桶，
// 每个桶都只用了 1 次额度，单键限额永远触发不了。
func TestGlobalLimitStopsKeyRotation(t *testing.T) {
	agg := NewAccessAggregator()
	hour := time.Date(2026, 10, 24, 3, 0, 0, 0, time.UTC)

	const requests = 5000
	emitted := 0
	for i := 0; i < requests; i++ {
		e := Entry{
			At:       hour.Add(time.Duration(i) * time.Millisecond),
			Event:    EventDownload,
			Source:   "public",
			Subject:  "dec-agent-darwin-arm64",
			RemoteIP: fmt.Sprintf("203.0.113.%d", i%256),
			OK:       true,
		}
		// 再加一层：主体也轮换，模拟同时换 IP 和换文件名。
		e.Subject = fmt.Sprintf("dec-agent-%d", i)
		if agg.Offer(e).Emit {
			emitted++
		}
	}

	if emitted > MaxAccessRecordsPerHourTotal {
		t.Fatalf("轮换 IP 后放行 %d 条，超过全局上限 %d",
			emitted, MaxAccessRecordsPerHourTotal)
	}
	// 条数仍要能还原，不能因为压上限就把发生次数丢掉。
	total := emitted
	for _, s := range agg.Take() {
		total += s.Count
	}
	if total != requests {
		t.Fatalf("还原 %d 条，应为 %d 条", total, requests)
	}
}

// TestBucketCountIsCapped 验证桶表不会随键的数量无界增长。
//
// 桶表本身也是内存状态。如果它能被公网请求撑到几十万条，
// 那就是把"磁盘写满"换成了"内存吃满"，同样是设计缺陷。
func TestBucketCountIsCapped(t *testing.T) {
	agg := NewAccessAggregator()
	hour := time.Date(2026, 10, 24, 4, 0, 0, 0, time.UTC)

	for i := 0; i < MaxAccessBuckets*4; i++ {
		agg.Offer(Entry{
			At:       hour.Add(time.Duration(i) * time.Millisecond),
			Event:    EventDownload,
			Source:   "public",
			Subject:  fmt.Sprintf("f-%d", i),
			RemoteIP: fmt.Sprintf("198.51.100.%d", i%256),
		})
	}
	if got := len(agg.buckets); got > MaxAccessBuckets {
		t.Fatalf("桶数 %d 超过上限 %d", got, MaxAccessBuckets)
	}
}

// TestTotalResetsNextHour 验证全局计数跨小时后归零。
//
// 不归零的后果是：某一小时被刷满之后，服务从此再也不记任何访问级事件，
// 等于把功能永久关掉。这比丢几条记录严重得多。
func TestTotalResetsNextHour(t *testing.T) {
	agg := NewAccessAggregator()
	hour := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)

	// 把这一小时彻底打满。
	for i := 0; i < MaxAccessRecordsPerHourTotal*2; i++ {
		agg.Offer(Entry{
			At:       hour.Add(time.Duration(i) * time.Millisecond),
			Event:    EventDownload,
			Source:   "public",
			Subject:  fmt.Sprintf("f-%d", i),
			RemoteIP: fmt.Sprintf("192.0.2.%d", i%256),
		})
	}

	// 下一个小时的第一条必须放行。
	d := agg.Offer(Entry{
		At:      hour.Add(time.Hour),
		Event:   EventDownload,
		Source:  "public",
		Subject: "dec-agent",
	})
	if !d.Emit {
		t.Fatal("新的一小时应重新放行：全局计数必须跨小时归零")
	}
}

// TestNormalUsageUnaffected 验证正常使用不会碰到任何限额。
//
// 限流最大的风险是误伤：日常操作被聚合掉，审计就变成不可信的。
// 所以这条断言"正常量级下一条都不该被省"。
func TestNormalUsageUnaffected(t *testing.T) {
	agg := NewAccessAggregator()
	hour := time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)

	// 一台设备一小时对账 10 次、两次客户端下载：远低于任何阈值。
	const syncs = 10
	emitted := 0
	for i := 0; i < syncs; i++ {
		if agg.Offer(Entry{
			At:       hour.Add(time.Duration(i) * time.Minute),
			Event:    EventSync,
			DeviceID: "dev-a",
			Source:   "agent",
			Subject:  fmt.Sprintf("rev-%d", i),
		}).Emit {
			emitted++
		}
	}
	if emitted != syncs {
		t.Fatalf("正常对账 %d 次应全部记录，实际记录 %d 次", syncs, emitted)
	}
	for i := 0; i < 2; i++ {
		if !agg.Offer(Entry{
			At:       hour.Add(time.Duration(i) * time.Minute),
			Event:    EventDownload,
			Source:   "public",
			Subject:  "dec-agent",
			RemoteIP: "203.0.113.7",
		}).Emit {
			t.Fatal("正常下载应记录")
		}
	}
}
