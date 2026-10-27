package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 本机入口信息（agent-tray-access 需求 2.3、3.2、3.3）。

func TestAccessURLsUseActualPort(t *testing.T) {
	urls := AccessURLs("127.0.0.1:9123")
	// 端口必须用实际监听端口，不能写死默认值（需求 3.3）。
	if got := PreferredAdminURL(urls); got != "http://127.0.0.1:9123/admin" {
		t.Fatalf("首选地址 = %q", got)
	}
	var hasLocalhost bool
	for _, u := range urls {
		if u.Kind == "localhost" && u.URL == "http://dec.localhost:9123" && u.Available {
			hasLocalhost = true
		}
	}
	if !hasLocalhost {
		t.Fatalf("应包含 dec.localhost 地址: %+v", urls)
	}
	if got := AccessURLs(""); got != nil {
		t.Fatalf("未监听时不应给出地址: %+v", got)
	}
}

func TestDefaultPortIsUncommon(t *testing.T) {
	// 8787/8788 常被其他开发服务占用，默认端口不能退回这类整齐数字。
	if DefaultPort == 8787 || DefaultPort == 8788 || DefaultPort < 1024 {
		t.Fatalf("默认端口 %d 容易冲突或需要特权", DefaultPort)
	}
}

func TestLoopbackStatusIncludesAccessInfo(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{
		LogPath: "/tmp/dec-agent/logs/agent.log",
		Status:  func() Status { return Status{Version: "v9.9.9", Aligned: true} },
	})
	port := l.port()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Host = "127.0.0.1:" + port
	rec := httptest.NewRecorder()
	l.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["version"] != "v9.9.9" || out["aligned"] != true {
		t.Fatalf("原有字段丢失: %v", out)
	}
	if out["admin_url"] != "http://127.0.0.1:"+port+"/admin" {
		t.Fatalf("admin_url = %v", out["admin_url"])
	}
	if out["log_path"] != "/tmp/dec-agent/logs/agent.log" {
		t.Fatalf("log_path = %v", out["log_path"])
	}
	if _, ok := out["pid"].(float64); !ok {
		t.Fatalf("缺少 pid: %v", out)
	}
	if _, ok := out["tls"]; ok {
		t.Fatalf("本机 HTTPS 已移除，不应再有 tls 字段: %v", out["tls"])
	}
	if urls, _ := out["urls"].([]any); len(urls) != 2 {
		t.Fatalf("urls = %v", out["urls"])
	}
}
