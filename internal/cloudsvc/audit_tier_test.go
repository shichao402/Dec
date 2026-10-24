package cloudsvc

import (
	"os"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/proto"
)

// 审计是旁路，不是主路（ADR 0043）。
//
// 这条不变量必须被测试锁住，而不能只写在注释里：2026-10 那次故障的本质
// 就是审计从旁路变成了主路的负担——写审计把磁盘写满，磁盘满让整个服务失联。
// 所以这里断言：审计层出任何问题，业务请求都必须照常成功。

// writeBlockingFile 在给定路径上放一个普通文件。
//
// 用来占据"本该是个目录"的位置，让随后对这些路径的 MkdirAll 必然失败。
func writeBlockingFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("准备阻塞文件失败: %v", err)
	}
}

// TestAuditFailureDoesNotBreakBusiness 验证审计写不进去时业务仍然成功。
//
// 做法：把审计仓库的 events 目录堵死（在目录该在的位置放一个普通文件），
// 让每次写入都失败，然后正常发起一次资产写入。
func TestAuditFailureDoesNotBreakBusiness(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	// 堵死审计目录：events 位置上放一个文件，MkdirAll 必然失败。
	writeBlockingFile(t, h.srv.Audit().Dir()+"/events")

	req := &proto.Request{
		ID:       "1",
		Protocol: proto.Version,
		Method:   proto.MethodWriteAsset,
		Source:   proto.SourceAdmin,
	}
	if err := req.EncodePayload(&proto.AssetPayload{
		Bundle:  "core",
		Items:   []proto.AssetItem{{Path: "demo/SKILL.md", Content: []byte("hello")}},
		Message: "审计不可写时仍应成功",
	}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if !resp.OK {
		t.Fatalf("审计不可写时资产写入仍应成功，实际失败: %s", resp.Error)
	}

	// 而且这次失败要被计数，不能静默。
	if h.srv.Audit().Dropped() == 0 {
		t.Fatal("审计写入失败应被计数，否则'审计有洞'这件事不可见")
	}
}

// TestAccessAuditIsBoundedEndToEnd 验证端到端下高频下发不会污染审计。
//
// 这是 2026-10-23 故障的整链路回归：真实走 MethodSync，而不是直接调用聚合器。
// 断言无论对账多少次，该小时该设备的原始 sync 记录条数都是常数，
// 同时下发次数仍然可以从记录里还原出来。
func TestAccessAuditIsBoundedEndToEnd(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	// 先写一次资产，并把这台设备订阅到该 bundle。
	//
	// 少了订阅这一步，服务端对账时算出的期望状态是空的，
	// 每轮都不会下发任何内容，测试就变成"什么也没验证"。
	put := &proto.Request{
		ID:       "1",
		Protocol: proto.Version,
		Method:   proto.MethodWriteAsset,
		Source:   proto.SourceAdmin,
	}
	if err := put.EncodePayload(&proto.AssetPayload{
		Bundle: "core",
		Items:  []proto.AssetItem{{Path: "demo/SKILL.md", Content: []byte("hello")}},
	}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	if resp := h.postTLS(put, cert); !resp.OK {
		t.Fatalf("准备资产失败: %s", resp.Error)
	}

	sub := &proto.Request{
		ID:       "2",
		Protocol: proto.Version,
		Method:   proto.MethodSetSubscriptions,
		Source:   proto.SourceAdmin,
	}
	if err := sub.EncodePayload(&proto.SetSubscriptionsRequest{
		DeviceID: "dev-a",
		Bundles:  []string{"core"},
	}); err != nil {
		t.Fatalf("构造订阅载荷失败: %v", err)
	}
	if resp := h.postTLS(sub, cert); !resp.OK {
		t.Fatalf("设置订阅失败: %s", resp.Error)
	}

	// 反复对账：每轮都上报"我什么都没有"，服务端每轮都会下发全量。
	const rounds = 300
	for i := 0; i < rounds; i++ {
		req := &proto.Request{ID: "s", Protocol: proto.Version, Method: proto.MethodSync}
		if err := req.EncodePayload(&proto.SyncRequest{}); err != nil {
			t.Fatalf("构造对账载荷失败: %v", err)
		}
		resp := h.postTLS(req, cert)
		if !resp.OK {
			t.Fatalf("第 %d 轮对账失败: %s", i, resp.Error)
		}
	}
	// 用 Close 结算：这是唯一会把聚合计数落盘的时机（Flush 只做提交）。
	if err := h.srv.Audit().Close(); err != nil {
		t.Fatalf("收尾失败: %v", err)
	}

	items, err := h.srv.Audit().Read(time.Now().UTC())
	if err != nil {
		t.Fatalf("读取审计失败: %v", err)
	}
	raw, restored := 0, 0
	for _, it := range items {
		if it.Event != audit.EventSync || it.DeviceID != "dev-a" {
			continue
		}
		if it.Count > 1 {
			restored += it.Count
			continue
		}
		raw++
		restored++
	}

	// 落盘条数必须有界。
	if raw > audit.MaxAccessRecordsPerHour {
		t.Fatalf("%d 轮对账落盘了 %d 条原始记录，超过上限 %d",
			rounds, raw, audit.MaxAccessRecordsPerHour)
	}
	// 但"下发过多少次"必须还能还原，不能凭空丢。
	if restored < rounds {
		t.Fatalf("审计丢失下发记录：还原 %d 条，实际下发 %d 轮", restored, rounds)
	}
}
