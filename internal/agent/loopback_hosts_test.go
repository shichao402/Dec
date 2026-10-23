package agent

import (
	"net/http"
	"testing"
)

// 回环 Host 白名单扩展（agent-tray-access 需求 4.1、4.2、7.7）。
//
// 白名单固定为 127.0.0.1、localhost、*.localhost、local.dec.firoyang.com 四类；
// 端口必须是代理自己监听的端口。这里五类输入各覆盖一遍。

func TestAllowedHostname(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":               true,
		"localhost":               true,
		"LOCALHOST":               true,
		"localhost.":              true,
		"dec.localhost":           true,
		"admin.dec.localhost":     true,
		"local.dec.firoyang.com":  true,
		"Local.Dec.Firoyang.com":  true,
		"local.dec.firoyang.com.": true,

		"":                            false,
		".localhost":                  false,
		"a..localhost":                false,
		"localhost.evil.com":          false,
		"evil-localhost":              false,
		"dec.firoyang.com":            false,
		"x.local.dec.firoyang.com":    false,
		"local.dec.firoyang.com.evil": false,
		"evil.example.com":            false,
		"::1":                         false,
		"127.0.0.2":                   false,
	}
	for in, want := range cases {
		if got := allowedHostname(in); got != want {
			t.Errorf("allowedHostname(%q) = %v，期望 %v", in, got, want)
		}
	}
}

func TestLoopbackAcceptsLocalNames(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	port := l.port()

	for _, host := range []string{
		"127.0.0.1:" + port,
		"localhost:" + port,
		"dec.localhost:" + port,
		LocalDomain + ":" + port,
	} {
		if rec := callWith(t, l, "/api/status", host, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("Host=%q 应通过，得到 %d", host, rec.Code)
		}
	}

	for _, host := range []string{
		"evil.example.com:" + port,
		"dec.firoyang.com:" + port,
		"dec.localhost:1",
		LocalDomain,       // 未登记 443 时不接受省略端口
		LocalDomain + ":", // 畸形
	} {
		if rec := callWith(t, l, "/api/status", host, "", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("Host=%q 应被拒绝，得到 %d", host, rec.Code)
		}
	}
}

func TestLoopbackExtraHTTPSPort(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	port := l.port()
	l.SetExtraPorts("443")

	// 浏览器访问 https://local.dec.firoyang.com 时 Host 不带端口。
	for _, host := range []string{LocalDomain, LocalDomain + ":443", "127.0.0.1:443", "127.0.0.1:" + port} {
		if rec := callWith(t, l, "/api/status", host, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("Host=%q 应通过，得到 %d", host, rec.Code)
		}
	}
	if rec := callWith(t, l, "/api/status", "evil.example.com", "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("外部域名省略端口也应被拒绝，得到 %d", rec.Code)
	}
}

func TestLoopbackOriginSchemes(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	port := l.port()
	l.SetExtraPorts("443")
	host := "127.0.0.1:" + port

	for _, origin := range []string{
		"http://127.0.0.1:" + port,
		"http://dec.localhost:" + port,
		"https://" + LocalDomain,
		"https://" + LocalDomain + ":443",
	} {
		if rec := callWith(t, l, "/api/status", host, origin, ""); rec.Code != http.StatusOK {
			t.Fatalf("Origin=%q 应通过，得到 %d", origin, rec.Code)
		}
	}
	for _, origin := range []string{
		"http://" + LocalDomain, // http 省略端口即 80，代理不监听
		"https://evil.example.com",
		"https://dec.firoyang.com",
		"http://evil.localhost.example.com:" + port,
	} {
		if rec := callWith(t, l, "/api/status", host, origin, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("Origin=%q 应被拒绝，得到 %d", origin, rec.Code)
		}
	}
}
