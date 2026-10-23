package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 本机入口信息（agent-tray-access 需求 2.3、3.2、3.3、4.9）。

func TestClassifyTLS(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		notAfter time.Time
		want     TLSState
	}{
		{time.Time{}, TLSNone},
		{now.Add(-time.Second), TLSExpired},
		{now, TLSExpired},
		{now.Add(13 * 24 * time.Hour), TLSExpiring},
		{now.Add(15 * 24 * time.Hour), TLSOK},
	}
	for _, c := range cases {
		if got := ClassifyTLS(c.notAfter, now); got != c.want {
			t.Errorf("ClassifyTLS(%v) = %s，期望 %s", c.notAfter, got, c.want)
		}
	}
}

func TestAccessURLsPrefersLocalDomain(t *testing.T) {
	urls := AccessURLs("127.0.0.1:8787", TLSStatus{State: TLSOK, Addr: "127.0.0.1:443"})
	if got := PreferredAdminURL(urls); got != "https://local.dec.firoyang.com/admin" {
		t.Fatalf("首选地址 = %q", got)
	}

	// 443 被占用，HTTPS 落在别的端口：地址必须带上实际端口。
	urls = AccessURLs("127.0.0.1:8787", TLSStatus{State: TLSExpiring, Addr: "127.0.0.1:8788"})
	if got := PreferredAdminURL(urls); got != "https://local.dec.firoyang.com:8788/admin" {
		t.Fatalf("443 被占用时首选地址 = %q", got)
	}
}

func TestAccessURLsFallsBackToLoopback(t *testing.T) {
	for _, tls := range []TLSStatus{
		{State: TLSNone},
		{State: TLSExpired, Addr: "127.0.0.1:443"},
		{State: TLSError, Error: "监听 443 失败"},
		{State: TLSOK}, // 有证书但没监听上
	} {
		urls := AccessURLs("127.0.0.1:9123", tls)
		// 端口必须用实际监听端口，不能写死 8787（需求 3.3）。
		if got := PreferredAdminURL(urls); got != "http://127.0.0.1:9123/admin" {
			t.Fatalf("tls=%+v 时首选地址 = %q", tls, got)
		}
		if urls[0].Available || urls[0].Reason == "" {
			t.Fatalf("tls=%+v 时 local 子域应标记不可用并给出原因: %+v", tls, urls[0])
		}
	}
	urls := AccessURLs("127.0.0.1:9123", TLSStatus{State: TLSNone})
	var hasLocalhost bool
	for _, u := range urls {
		if u.Kind == "localhost" && u.URL == "http://dec.localhost:9123" && u.Available {
			hasLocalhost = true
		}
	}
	if !hasLocalhost {
		t.Fatalf("应包含 dec.localhost 地址: %+v", urls)
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
	tls, _ := out["tls"].(map[string]any)
	if tls["state"] != string(TLSNone) {
		t.Fatalf("tls.state = %v", tls["state"])
	}
	if urls, _ := out["urls"].([]any); len(urls) != 3 {
		t.Fatalf("urls = %v", out["urls"])
	}
}
