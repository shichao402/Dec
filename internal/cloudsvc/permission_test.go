package cloudsvc

import (
	"testing"

	"github.com/shichao402/Dec/internal/proto"
)

// MCP 来源的权限边界（需求 13.2、已定决策 12、计划 9.2）。
//
// 六条规则，逐条锁死：
//   1. 可以写资产与密钥
//   2. 可以列元数据
//   3. 读不到密钥明文
//   4. 管不了设备（登记、吊销、改类型、改订阅）
//   5. 不能回滚
//   6. 不能轮换根密钥
//
// 这些用例都用 MCP 来源真跑一次请求。写成“检查权限函数返回 false”是不够的：
// 真正会出问题的地方是某个方法忘了调用权限函数。

// mcpRequest 构造一个 MCP 来源的请求。
func mcpRequest(t *testing.T, m proto.Method, payload any) *proto.Request {
	t.Helper()
	req := &proto.Request{ID: "mcp-1", Protocol: proto.Version, Method: m, Source: proto.SourceMCP}
	if payload != nil {
		if err := req.EncodePayload(payload); err != nil {
			t.Fatalf("构造载荷失败: %v", err)
		}
	}
	return req
}

// adminRequest 构造一个管理面来源的请求，用来对照“同样的方法换成管理面就成功”。
func adminRequest(t *testing.T, m proto.Method, payload any) *proto.Request {
	t.Helper()
	req := &proto.Request{ID: "adm-1", Protocol: proto.Version, Method: m, Source: proto.SourceAdmin}
	if payload != nil {
		if err := req.EncodePayload(payload); err != nil {
			t.Fatalf("构造载荷失败: %v", err)
		}
	}
	return req
}

// TestMCPCanListMetadata 验证 MCP 能看资产与密钥的元数据。
//
// 这是 AI 帮用户整理资产的日常用法，必须能用。
// 注意 status 不在此列：它含根密钥 ID、初始化入口开关这类内部信息，
// 只给本机管理面与 CLI（MCP 的“元数据”指资产与密钥，不是服务端内部状态）。
func TestMCPCanListMetadata(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	for _, m := range []proto.Method{proto.MethodListAssets, proto.MethodListSecrets} {
		resp := h.postTLS(mcpRequest(t, m, nil), cert)
		if !resp.OK {
			t.Fatalf("MCP 应能调用 %s，实际失败: %s", m, resp.Error)
		}
	}
	// 历史也算元数据：AI 需要看到改过什么才能帮用户判断。
	resp := h.postTLS(mcpRequest(t, proto.MethodHistory, &proto.HistoryRequest{Kind: "asset", ID: "woa"}), cert)
	if !resp.OK {
		t.Fatalf("MCP 应能查历史，实际失败: %s", resp.Error)
	}
}

// TestMCPCannotSeeStatus 锁定服务状态不对 MCP 开放。
func TestMCPCannotSeeStatus(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	resp := h.postTLS(mcpRequest(t, proto.MethodStatus, nil), cert)
	if resp.OK {
		t.Fatal("MCP 来源不应查看服务状态")
	}
	if resp.Code != proto.ErrForbidden {
		t.Fatalf("错误码为 %q，期望 forbidden", resp.Code)
	}
}

// TestMCPCanWriteAsset 验证 MCP 能写资产。
func TestMCPCanWriteAsset(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")
	req := mcpRequest(t, proto.MethodWriteAsset, &proto.AssetPayload{
		Bundle: "woa",
		Items: []proto.AssetItem{
			{Path: "a.md", Content: []byte("hello")},
		},
		Message: "写入一条规则",
	})
	resp := h.postTLS(req, cert)
	if !resp.OK {
		t.Fatalf("MCP 应能写资产，实际失败: %s", resp.Error)
	}
}

// TestMCPCannotRollback 验证 MCP 不能回滚。
//
// 回滚会一次性改变全部设备的期望状态，是高危操作；
// AI 一次误判就能把线上内容整体退回，所以只允许本机管理面发起。
func TestMCPCannotRollback(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	req := mcpRequest(t, proto.MethodRollback, &proto.RollbackRequest{
		Kind:   "asset",
		ID:     "woa",
		Commit: "deadbeef",
	})
	resp := h.postTLS(req, cert)
	if resp.OK {
		t.Fatal("MCP 来源不应回滚")
	}
	if resp.Code != proto.ErrForbidden {
		t.Fatalf("错误码为 %q，期望 forbidden", resp.Code)
	}
}

// TestMCPCannotRotateRootKey 验证 MCP 不能轮换根密钥。
func TestMCPCannotRotateRootKey(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	req := mcpRequest(t, proto.MethodRotateRootKey, &proto.RotateRequest{
		Email:    "me@example.com",
		Password: "x",
	})
	resp := h.postTLS(req, cert)
	if resp.OK {
		t.Fatal("MCP 来源不应轮换根密钥")
	}
	if resp.Code != proto.ErrForbidden {
		t.Fatalf("错误码为 %q，期望 forbidden", resp.Code)
	}
}

// TestMCPCannotManageDevices 验证 MCP 管不了设备。
//
// 这条比看起来重要：一次提示注入如果能吊销设备，就等于把用户锁在门外。
func TestMCPCannotManageDevices(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")
	h.registerDevice("dev-b", "NUC")

	cases := []struct {
		name string
		m    proto.Method
		in   any
	}{
		{"吊销设备", proto.MethodRevokeDevice, &proto.RevokeRequest{DeviceID: "dev-b"}},
		{"改设备类型", proto.MethodSetDeviceKind, &proto.SetDeviceKindRequest{DeviceID: "dev-b", Kind: "receiver"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := h.postTLS(mcpRequest(t, c.m, c.in), cert)
			if resp.OK {
				t.Fatalf("MCP 来源不应%s", c.name)
			}
			if resp.Code != proto.ErrForbidden {
				t.Fatalf("错误码为 %q，期望 forbidden", resp.Code)
			}
		})
	}
}

// TestMCPCanSetOwnSubscriptions 验证 MCP 能改本机设备的订阅。
//
// 订阅列表只决定这台设备自己消费哪些 bundle，不涉及别的设备与任何密钥。
// 把它一并归入“设备管理”会让 dec_set_subscriptions 这个工具永远调不通——
// 工具在清单里却必然报 forbidden，正是本次要杜绝的“标记完成但其实用不了”。
func TestMCPCanSetOwnSubscriptions(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	resp := h.postTLS(mcpRequest(t, proto.MethodSetSubscriptions, &proto.SetSubscriptionsRequest{
		DeviceID: "dev-a",
		Bundles:  []string{"woa"},
	}), cert)
	if !resp.OK {
		t.Fatalf("MCP 应能改本机订阅，实际失败: %s", resp.Error)
	}
}

// TestMCPCannotSetOtherDeviceSubscriptions 验证 MCP 改不了别的设备的订阅。
//
// 跨设备修改是真正的设备管理：一条提示注入不该能把另一台机器的订阅换掉。
func TestMCPCannotSetOtherDeviceSubscriptions(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")
	h.registerDevice("dev-b", "NUC")

	resp := h.postTLS(mcpRequest(t, proto.MethodSetSubscriptions, &proto.SetSubscriptionsRequest{
		DeviceID: "dev-b",
		Bundles:  []string{"woa"},
	}), cert)
	if resp.OK {
		t.Fatal("MCP 来源不应修改其它设备的订阅")
	}
	if resp.Code != proto.ErrForbidden {
		t.Fatalf("错误码为 %q，期望 forbidden", resp.Code)
	}
}

// TestMCPCannotEnroll 验证 MCP 入口不能登记新设备。
//
// enroll 是免证书方法，来源由入口决定；即使有人直接打协议，
// 也不该让一次 AI 调用换来一张长期设备证书。
func TestMCPCannotEnroll(t *testing.T) {
	h := newHarness(t, "1.0.0")
	// 先占掉初始化：初始化入口只在“没有任何设备”时开放。
	h.registerDevice("dev-a", "MacBook")

	req := mcpRequest(t, proto.MethodEnroll, &proto.EnrollBeginRequest{
		Email:           "me@example.com",
		Password:        "x",
		DevicePublicKey: "not-a-real-key",
		DeviceName:      "evil",
	})
	resp := h.post(req)
	if resp.OK {
		t.Fatal("设备表非空时登记应被拒绝")
	}
}

// TestAdminCanRollbackPermissionShape 验证管理面不会被上面这些限制误伤。
//
// 只断言“不是 forbidden”：这个方法在用例里注定因为找不到 commit 而失败，
// 但失败原因必须是业务错误，而不是权限被挡。
func TestAdminCanRollbackPermissionShape(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	req := adminRequest(t, proto.MethodRollback, &proto.RollbackRequest{
		Kind:   "asset",
		ID:     "woa",
		Commit: "deadbeef",
	})
	resp := h.postTLS(req, cert)
	if resp.Code == proto.ErrForbidden {
		t.Fatalf("管理面回滚不应被权限挡住: %s", resp.Error)
	}
}
