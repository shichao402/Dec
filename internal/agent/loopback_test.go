package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/proto"
)

// 回环入口的边界（需求 2.10、13.1、13.2）。
//
// 已定决策：不设 token、不设 CSRF，只校验 Host 与 Origin。
// 所以这些用例的重点是“谁能进来”，而不是“凭据对不对”。

// newTestLoopback 造一个已绑定端口的回环服务。
//
// 端口必须真实存在：Host 校验会把请求 Host 的端口和自己的监听端口比对，
// 没起监听就没有端口可比。
func newTestLoopback(t *testing.T, opts LoopbackOptions) *Loopback {
	t.Helper()
	opts.Port = 0
	l := NewLoopback(opts)
	if _, err := l.Start(); err != nil {
		t.Fatalf("启动回环服务失败: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// callWith 发一个带自定义 Host / Origin 的请求。
func callWith(t *testing.T, l *Loopback, path, host, origin string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	l.Handler().ServeHTTP(rec, req)
	return rec
}

func TestLoopbackRejectsForeignHost(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	port := l.port()

	// 正常：请求就是发给自己的。
	if rec := callWith(t, l, "/api/status", "127.0.0.1:"+port, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("本机请求应通过，得到 %d", rec.Code)
	}
	if rec := callWith(t, l, "/api/status", "localhost:"+port, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("localhost 请求应通过，得到 %d", rec.Code)
	}

	// DNS rebinding：网页把自己的域名解析到 127.0.0.1，
	// 浏览器带的 Host 仍是那个域名，必须挡住。
	for _, host := range []string{
		"evil.example.com:" + port,
		"127.0.0.1:1",
		"127.0.0.1",
		"[::1]:" + port,
		"",
	} {
		if rec := callWith(t, l, "/api/status", host, "", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("Host=%q 应被拒绝，得到 %d", host, rec.Code)
		}
	}
}

func TestLoopbackRejectsForeignOrigin(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	port := l.port()

	if rec := callWith(t, l, "/api/status", "127.0.0.1:"+port, "http://127.0.0.1:"+port, ""); rec.Code != http.StatusOK {
		t.Fatalf("同源请求应通过，得到 %d", rec.Code)
	}
	for _, origin := range []string{
		"https://evil.example.com",
		"http://evil.example.com",
		"null",
		"http://127.0.0.1:1",
		"file://",
	} {
		if rec := callWith(t, l, "/api/status", "127.0.0.1:"+port, origin, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("Origin=%q 应被拒绝，得到 %d", origin, rec.Code)
		}
	}
}

// TestLoopbackCallSourceIsFixedByEntry 验证调用方在 body 里写什么来源都不算数。
//
// 来源由路径决定：/api/call 固定 admin，MCP 入口固定 mcp（后者在
// cmd/dec-agent 接线时写死，由 MCP 协议层测试覆盖）。这里要守住的是
// 「普通 JSON 不能被当成一次自带来源的转发」这条线。
func TestLoopbackCallSourceIsFixedByEntry(t *testing.T) {
	var got []string
	mcp := NewMCPHandler(MCPServer{
		ServerVersion: "test",
		Call: func(ctx context.Context, m proto.Method, payload json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"ok":true}`), nil
		},
	})
	l := newTestLoopback(t, LoopbackOptions{
		Call: func(r *http.Request, m, src string, payload json.RawMessage) (json.RawMessage, error) {
			got = append(got, m+"@"+src)
			return json.RawMessage(`{"ok":true}`), nil
		},
		MCP: mcp,
	})
	port := l.port()

	body := `{"method":"write_asset","source":"admin","payload":{"bundle":"woa"}}`
	if rec := callWith(t, l, "/api/call", "127.0.0.1:"+port, "", body); rec.Code != http.StatusOK {
		t.Fatalf("管理面转发应通过，得到 %d：%s", rec.Code, rec.Body.String())
	}
	// MCP 入口只认 MCP 报文：把管理面的请求体丢过来不该被当成一次转发。
	if rec := callWith(t, l, "/mcp", "127.0.0.1:"+port, "", body); rec.Code == http.StatusOK {
		t.Fatalf("MCP 入口不应把普通 JSON 当成一次转发，得到 %d：%s", rec.Code, rec.Body.String())
	}

	want := []string{"write_asset@admin"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("来源判定 %v，期望 %v", got, want)
	}
}

func TestLoopbackRejectsGetOnCall(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{
		Call: func(r *http.Request, m, src string, payload json.RawMessage) (json.RawMessage, error) {
			t.Fatal("不应该被调用")
			return nil, nil
		},
	})
	port := l.port()
	req := httptest.NewRequest(http.MethodGet, "/api/call", nil)
	req.Host = "127.0.0.1:" + port
	rec := httptest.NewRecorder()
	l.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET 应被拒绝，得到 %d", rec.Code)
	}
}

func TestLoopbackStatusShape(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{
		Status: func() Status {
			return Status{
				ServerURL:     "https://dec.test",
				Version:       "1.0.0",
				RequiredVer:   "1.0.0",
				Aligned:       true,
				DeviceID:      "dev-1",
				DownloadURL:   "https://dec.test/download",
				ServerVersion: "1.0.0",
			}
		},
	})
	port := l.port()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Host = "127.0.0.1:" + port
	rec := httptest.NewRecorder()
	l.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态应可读取，得到 %d", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("状态不是合法 JSON: %v", err)
	}
	if out["device_id"] != "dev-1" || out["aligned"] != true {
		t.Fatalf("状态内容不符：%v", out)
	}
}

func TestLoopbackIndexServed(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	port := l.port()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:" + port
	rec := httptest.NewRecorder()
	l.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("首页应可读取，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Dec 代理") {
		t.Fatalf("首页内容异常：%s", rec.Body.String())
	}
	// 页面必须自包含：代理是下载即跑的单文件程序，不能依赖外链。
	body := rec.Body.String()
	for _, bad := range []string{"<script src=", "<link rel=\"stylesheet\" href="} {
		if strings.Contains(body, bad) {
			t.Fatalf("页面不应引用外部资源：%s", bad)
		}
	}
}

func TestLoopbackUnknownPathIs404(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	port := l.port()
	req := httptest.NewRequest(http.MethodGet, "/etc/passwd", nil)
	req.Host = "127.0.0.1:" + port
	rec := httptest.NewRecorder()
	l.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知路径应 404，得到 %d", rec.Code)
	}
}

func TestProtoSourceConstants(t *testing.T) {
	// 来源常量必须真的指向协议里的值：写错就等于给自己开了个后门。
	if protoSourceAdmin != proto.SourceAdmin {
		t.Fatalf("管理面来源 %q 与协议 %q 不一致", protoSourceAdmin, proto.SourceAdmin)
	}
	if protoSourceMCP != proto.SourceMCP {
		t.Fatalf("MCP 来源 %q 与协议 %q 不一致", protoSourceMCP, proto.SourceMCP)
	}
}
