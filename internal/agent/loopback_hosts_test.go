package agent

import (
	"net/http"
	"testing"
)

// 回环 Host 白名单（agent-tray-access 需求 4.1、4.2、7.7）。
//
// 白名单固定为 127.0.0.1、localhost、*.localhost 三类；
// 端口必须是代理自己监听的端口。

func TestAllowedHostname(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":           true,
		"localhost":           true,
		"LOCALHOST":           true,
		"localhost.":          true,
		"dec.localhost":       true,
		"admin.dec.localhost": true,

		"":                       false,
		".localhost":             false,
		"a..localhost":           false,
		"localhost.evil.com":     false,
		"evil-localhost":         false,
		"dec.firoyang.com":       false,
		"local.dec.firoyang.com": false,
		"evil.example.com":       false,
		"::1":                    false,
		"127.0.0.2":              false,
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
	} {
		if rec := callWith(t, l, "/api/status", host, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("Host=%q 应通过，得到 %d", host, rec.Code)
		}
	}

	for _, host := range []string{
		"evil.example.com:" + port,
		"dec.firoyang.com:" + port,
		"local.dec.firoyang.com:" + port,
		"dec.localhost:1",
		"127.0.0.1",  // 省略端口（隐含 80/443），代理不监听
		"127.0.0.1:", // 畸形
		"127.0.0.1:443",
	} {
		if rec := callWith(t, l, "/api/status", host, "", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("Host=%q 应被拒绝，得到 %d", host, rec.Code)
		}
	}
}

func TestLoopbackOriginSchemes(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	port := l.port()
	host := "127.0.0.1:" + port

	for _, origin := range []string{
		"http://127.0.0.1:" + port,
		"http://dec.localhost:" + port,
	} {
		if rec := callWith(t, l, "/api/status", host, origin, ""); rec.Code != http.StatusOK {
			t.Fatalf("Origin=%q 应通过，得到 %d", origin, rec.Code)
		}
	}
	for _, origin := range []string{
		"http://127.0.0.1",
		"https://127.0.0.1:" + port, // 代理不提供 HTTPS
		"https://local.dec.firoyang.com",
		"https://evil.example.com",
		"http://evil.localhost.example.com:" + port,
	} {
		if rec := callWith(t, l, "/api/status", host, origin, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("Origin=%q 应被拒绝，得到 %d", origin, rec.Code)
		}
	}
}
