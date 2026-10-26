package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shichao402/Dec/internal/proto"
)

// hasChinese 报告 s 里是否含有汉字。
//
// 用码点区间判断而不是正则：只要有一个汉字就算中文描述，剩下的英文术语
// （`bundle`、`revision`）本来就是必须保留的原文。
func hasChinese(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

// readOnlyTools 是明确没有副作用的工具（需求 1.7）。
//
// 剩下的工具都会写云端：要么立即推送给在线设备，要么只进审计。
var readOnlyTools = map[string]bool{
	"dec_status":             true,
	"dec_list_assets":        true,
	"dec_read_asset":         true,
	"dec_list_secrets":       true,
	"dec_history":            true,
	"dec_list_projects":      true,
	"dec_list_subscriptions": true,
}

// checkSideEffectNoted 检查描述里是否交代了副作用（需求 1.7）。
//
// 只读工具要明确说「只读」或「无副作用」，写入工具要说清推送与审计——
// AI 客户端只会读这段文字来决定要不要先确认，写漏了就等于把判断责任
// 推给了人。「只读」与「无副作用」是同一件事的不同说法，都算写了。
func checkSideEffectNoted(name, desc string) error {
	if readOnlyTools[name] {
		if !strings.Contains(desc, "无副作用") && !strings.Contains(desc, "只读") {
			return errors.New(name + " 是只读工具，描述里应写明「只读」或「无副作用」: " + desc)
		}
		return nil
	}
	// 写入类：必须提到推送或审计，二选一即可（项目登记只进审计、不推送）。
	if strings.Contains(desc, "推送") || strings.Contains(desc, "审计") {
		return nil
	}
	return errors.New(name + " 是写入工具，描述里应写明推送或审计副作用: " + desc)
}

// fakeCloud 记录代理转发出去的每一次调用，并按预设返回结果。
type fakeCloud struct {
	mu    sync.Mutex
	calls []recordedCall
	// reply 按协议方法返回载荷；返回 error 表示云端拒绝。
	reply map[proto.Method]func(payload json.RawMessage) (json.RawMessage, error)
}

type recordedCall struct {
	Method  proto.Method
	Payload json.RawMessage
}

func (f *fakeCloud) call(ctx context.Context, m proto.Method, payload json.RawMessage) (json.RawMessage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{Method: m, Payload: payload})
	fn := f.reply[m]
	f.mu.Unlock()
	if fn != nil {
		return fn(payload)
	}
	return json.RawMessage(`{}`), nil
}

func (f *fakeCloud) recorded() []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// newTestSession 起一个真实 HTTP 服务并把官方 SDK 客户端连上去。
//
// 用真实的 handler 与真实的客户端，而不是直接调工具函数：
// 握手、会话、JSON-RPC 编解码这些正是本次要替换掉的部分，
// 绕过它们测出来的结论对「标准客户端能不能连上」没有意义。
func newTestSession(t *testing.T, opts MCPServer) *mcp.ClientSession {
	t.Helper()
	addr := newTestServer(t, opts)
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "dec-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: "http://" + addr + "/mcp",
	}, nil)
	if err != nil {
		t.Fatalf("连接 MCP 服务失败: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// newTestServer 起一个真实回环服务，返回它的监听地址。
//
// 必须真的 Start 而不是用 httptest：Host 守卫要求 Host 头里的端口
// 与自己监听的端口一致（防 DNS rebinding），拿不到真实端口就进不来。
func newTestServer(t *testing.T, opts MCPServer) string {
	t.Helper()
	if opts.ServerVersion == "" {
		opts.ServerVersion = "test"
	}
	loop := NewLoopback(LoopbackOptions{
		Status: opts.Status,
		MCP:    NewMCPHandler(opts),
	})
	addr, err := loop.Start()
	if err != nil {
		t.Fatalf("启动回环服务失败: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return addr
}

// TestMCPHandshakeIsLocal 验证握手在本地完成（需求 1.1、1.3）。
//
// 握手被转发给云端是本次要修的核心问题：云端的 initialize 是「首次初始化」，
// 一旦被 MCP 客户端触发，全新服务会被意外初始化掉。
func TestMCPHandshakeIsLocal(t *testing.T) {
	cloud := &fakeCloud{}
	session := newTestSession(t, MCPServer{Call: cloud.call})

	// Connect 内部已完成 initialize 与 notifications/initialized。
	if got := session.InitializeResult(); got == nil {
		t.Fatal("客户端没有拿到 initialize 结果")
	} else if got.ServerInfo == nil || got.ServerInfo.Name != "dec" {
		t.Fatalf("serverInfo 应为 dec，实际 %+v", got.ServerInfo)
	}
	if n := len(cloud.recorded()); n != 0 {
		t.Fatalf("握手阶段不应转发任何调用给云端，实际转发了 %d 次: %+v", n, cloud.recorded())
	}
}

// TestMCPToolsList 验证工具清单（需求 1.4、1.5）。
func TestMCPToolsList(t *testing.T) {
	session := newTestSession(t, MCPServer{Call: (&fakeCloud{}).call})

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list 失败: %v", err)
	}
	got := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}

	want := []string{
		"dec_status",
		"dec_list_assets", "dec_read_asset", "dec_write_asset", "dec_delete_asset_item", "dec_delete_bundle",
		"dec_list_secrets", "dec_put_secret", "dec_delete_secret",
		"dec_history",
		"dec_list_projects", "dec_put_project", "dec_delete_project",
		"dec_list_subscriptions", "dec_set_subscriptions",
	}
	for _, name := range want {
		tool, ok := got[name]
		if !ok {
			t.Errorf("工具清单缺少 %s", name)
			continue
		}
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("%s 缺少描述", name)
		}
		// 需求 1.7：描述必须是中文，并写明副作用。AI 客户端只会读这段文字来
		// 判断该不该调用，英文或只写「写入资产」等于没写副作用。
		if !hasChinese(tool.Description) {
			t.Errorf("%s 的描述不是中文: %q", name, tool.Description)
		}
		if err := checkSideEffectNoted(name, tool.Description); err != nil {
			t.Error(err)
		}
		if tool.InputSchema == nil {
			t.Errorf("%s 缺少输入 schema", name)
		}
	}

	// 需求 1.4 点名要求写资产的描述写明整体替换语义。
	if tool, ok := got["dec_write_asset"]; ok {
		for _, needle := range []string{"整体替换", "删除"} {
			if !strings.Contains(tool.Description, needle) {
				t.Errorf("dec_write_asset 的描述没有写明整体替换语义（缺 %q）: %q", needle, tool.Description)
			}
		}
	}

	// 被禁的工具必须不可见（需求 1.5）。
	banned := []string{
		"dec_read_secret", "dec_devices", "dec_revoke_device",
		"dec_rollback", "dec_rotate_root_key", "dec_set_device_kind",
	}
	for _, name := range banned {
		if _, ok := got[name]; ok {
			t.Errorf("工具清单不应包含 %s", name)
		}
	}
}

// TestMCPStatusForwardsHello 验证状态工具转发的是 hello（需求 1.4、1.8）。
//
// status 含根密钥 ID 与初始化入口开关，服务端对 mcp 来源一律拒绝
// （见 internal/cloudsvc/permission_test.go 的 TestMCPCannotSeeStatus）。
// 如果这里退回转发 status，工具在清单里却必然报 forbidden，
// 就是“标记完成但其实用不了”。
func TestMCPStatusForwardsHello(t *testing.T) {
	cloud := &fakeCloud{}
	session := newTestSession(t, MCPServer{
		Call:   cloud.call,
		Status: func() Status { return Status{DeviceID: "dev-1", Version: "v1.2.3", Aligned: true} },
	})

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "dec_status", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("工具返回错误: %s", contentText(res))
	}
	calls := cloud.recorded()
	if len(calls) != 1 {
		t.Fatalf("应转发 1 次，实际 %d 次", len(calls))
	}
	if calls[0].Method != proto.MethodHello {
		t.Fatalf("dec_status 应转发 hello，实际 %s", calls[0].Method)
	}
	var in proto.HelloRequest
	if err := json.Unmarshal(calls[0].Payload, &in); err != nil {
		t.Fatalf("载荷不是合法的 HelloRequest: %v\n%s", err, calls[0].Payload)
	}
	if in.AgentVersion != "v1.2.3" {
		t.Fatalf("agent_version 应取本机版本 v1.2.3，实际 %q", in.AgentVersion)
	}
}

// TestMCPForwardsWithMCPPayload 验证转发的方法名与载荷（需求 1.6）。
func TestMCPForwardsWithMCPPayload(t *testing.T) {
	cloud := &fakeCloud{
		reply: map[proto.Method]func(json.RawMessage) (json.RawMessage, error){
			proto.MethodReadAsset: func(json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(`{"bundle":"my-skills","files":[]}`), nil
			},
		},
	}
	session := newTestSession(t, MCPServer{Call: cloud.call})

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "dec_read_asset",
		Arguments: map[string]any{"bundle": "my-skills"},
	})
	if err != nil {
		t.Fatalf("调用工具失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("工具返回错误: %s", contentText(res))
	}

	calls := cloud.recorded()
	if len(calls) != 1 {
		t.Fatalf("应转发 1 次，实际 %d 次", len(calls))
	}
	if calls[0].Method != proto.MethodReadAsset {
		t.Fatalf("转发的方法应为 read_asset，实际 %s", calls[0].Method)
	}
	if !strings.Contains(string(calls[0].Payload), `"bundle":"my-skills"`) {
		t.Fatalf("载荷没有带到云端: %s", calls[0].Payload)
	}
}

// TestMCPWriteAssetPayload 验证写入载荷的字段映射（需求 1.4、1.7）。
//
// 重点是 content：proto.AssetItem.Content 是 []byte，若把普通字符串直接
// json.Unmarshal 进去会被当成 base64，文本会静默损坏。
func TestMCPWriteAssetPayload(t *testing.T) {
	cloud := &fakeCloud{}
	session := newTestSession(t, MCPServer{Call: cloud.call})

	text := "# 标题\n带 & 与 = 的正文\n"
	cases := []struct {
		name    string
		content string
		b64     bool
	}{
		{name: "纯文本", content: text},
		{name: "base64", content: "aGVsbG8=", b64: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := map[string]any{
				"bundle": "my-skills",
				"items": []any{
					map[string]any{"path": "skills/a/SKILL.md", "content": c.content, "base64": c.b64},
				},
				"message": "测试写入",
			}
			res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "dec_write_asset", Arguments: args,
			})
			if err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			if res.IsError {
				t.Fatalf("工具返回错误: %s", contentText(res))
			}
		})
	}

	calls := cloud.recorded()
	if len(calls) != len(cases) {
		t.Fatalf("应转发 %d 次，实际 %d 次", len(cases), len(calls))
	}
	var payload proto.AssetPayload
	if err := json.Unmarshal(calls[0].Payload, &payload); err != nil {
		t.Fatalf("载荷不是合法的 AssetPayload: %v\n%s", err, calls[0].Payload)
	}
	if string(payload.Items[0].Content) != text {
		t.Fatalf("纯文本正文被损坏：期望 %q，实际 %q", text, string(payload.Items[0].Content))
	}

	var binary proto.AssetPayload
	if err := json.Unmarshal(calls[1].Payload, &binary); err != nil {
		t.Fatalf("载荷不是合法的 AssetPayload: %v", err)
	}
	if string(binary.Items[0].Content) != "hello" {
		t.Fatalf("base64 正文解码错误：期望 hello，实际 %q", string(binary.Items[0].Content))
	}
}

// TestMCPBannedToolNotCallable 验证清单外的工具调不通（需求 1.5）。
func TestMCPBannedToolNotCallable(t *testing.T) {
	cloud := &fakeCloud{}
	session := newTestSession(t, MCPServer{Call: cloud.call})

	_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "dec_read_secret", Arguments: map[string]any{"name": "my-skills/github-token"},
	})
	if err == nil {
		t.Fatal("调用被禁的工具应当失败")
	}
	if n := len(cloud.recorded()); n != 0 {
		t.Fatalf("被禁的工具不应转发给云端，实际转发了 %d 次", n)
	}
}

// TestMCPErrorMessages 验证各类错误都能说清原因（需求 1.8）。
func TestMCPErrorMessages(t *testing.T) {
	cases := []struct {
		name     string
		status   Status
		callErr  error
		wantWord string
	}{
		{
			name:     "未登记",
			status:   Status{DeviceID: "", Version: "v1.0.0"},
			wantWord: "还没有登记",
		},
		{
			name:     "版本不对齐",
			status:   Status{DeviceID: "dev-1", Version: "v1.0.0", Aligned: false, RequiredVer: "v2.0.0"},
			wantWord: "版本与服务端要求不一致",
		},
		{
			name:     "维护中",
			status:   Status{DeviceID: "dev-1", Version: "v1.0.0", Aligned: true, Maintenance: true},
			wantWord: "维护中",
		},
		{
			name:     "云端拒绝",
			status:   Status{DeviceID: "dev-1", Version: "v1.0.0", Aligned: true},
			callErr:  errors.New("forbidden: 来源 mcp 不能读密钥明文"),
			wantWord: "云端调用失败",
		},
		{
			name:     "身份被拒",
			status:   Status{DeviceID: "dev-1", Version: "v1.0.0", Aligned: true},
			callErr:  ErrIdentityRejected,
			wantWord: "设备身份被服务端拒绝",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status := c.status
			call := func(context.Context, proto.Method, json.RawMessage) (json.RawMessage, error) {
				if c.callErr != nil {
					return nil, c.callErr
				}
				return json.RawMessage(`{}`), nil
			}
			session := newTestSession(t, MCPServer{
				Call:   call,
				Status: func() Status { return status },
			})
			res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "dec_list_assets", Arguments: map[string]any{},
			})
			if err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			if !res.IsError {
				t.Fatal("应当返回工具错误")
			}
			msg := contentText(res)
			if !strings.Contains(msg, c.wantWord) {
				t.Fatalf("错误信息应包含 %q，实际 %q", c.wantWord, msg)
			}
			if strings.TrimSpace(msg) == "" {
				t.Fatal("错误信息为空，无法定位原因")
			}
		})
	}
}

// TestMCPRejectsExternalOrigin 验证 Origin 守卫对 /mcp 同样生效（需求 1.9）。
func TestMCPRejectsExternalOrigin(t *testing.T) {
	addr := newTestServer(t, MCPServer{Call: (&fakeCloud{}).call})

	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/mcp", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("外部 Origin 应被拒绝，实际 %d", resp.StatusCode)
	}
}

// TestMCPAcceptsNoOrigin 验证不带 Origin 的本机客户端可以连（需求 1.9）。
func TestMCPAcceptsNoOrigin(t *testing.T) {
	// newTestSession 用的就是 SDK 客户端，它不发 Origin 头。
	session := newTestSession(t, MCPServer{Call: (&fakeCloud{}).call})
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("不带 Origin 的本机客户端应能连上: %v", err)
	}
}

// contentText 取结果里的文本内容。
func contentText(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}
