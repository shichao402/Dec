package cloudsvc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// 端到端验证：PROXY 头声明的来源真的被用上了。
//
// 前面 proxyproto_test.go 验证的是"解析器能解出地址"，这里验证的是
// "这个地址真的进入了限流与审计"——两者的区别很关键：
// 解析器通过但接线错了（比如 RemoteAddr 调用时机不对），
// 线上表现仍是所有请求共用一个限流桶，而那正是这次要修的问题。
//
// 为什么不走真实 TLS：这里关心的是 listener 层的地址传递，
// 用明文 HTTP 就能验证，不必引入证书。TLS 握手只是"第一次 Read"的一种情形，
// 而解析发生在更前面的位置（RemoteAddr 或首次 Read 都会触发）。

// startProxyListener 起一个带 PROXY 解析的明文 HTTP 服务，返回地址。
func startProxyListener(t *testing.T, h http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	ln = newProxyProtocolListener(ln, true)
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return ln.Addr().String()
}

// sendWithSource 建立一条连接，先发 PROXY 头声明来源，再发 HTTP 请求。
func sendWithSource(t *testing.T, addr, sourceIP, path string) (string, int) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer c.Close()

	// v1 格式最简单，且与 nginx 的 proxy_protocol on 默认行为一致（v1）。
	if _, err := fmt.Fprintf(c, "PROXY TCP4 %s 127.0.0.1 12345 8080\r\n", sourceIP); err != nil {
		t.Fatalf("写 PROXY 头失败: %v", err)
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: dec.test\r\nConnection: close\r\n\r\n", path)
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatalf("写请求失败: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatalf("读响应失败: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), resp.StatusCode
}

// TestProxyHeaderReachesHandler 验证 handler 看到的是 PROXY 声明的来源。
func TestProxyHeaderReachesHandler(t *testing.T) {
	var got []string
	var mu sync.Mutex
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, clientIP(r))
		mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	})

	addr := startProxyListener(t, h)
	if _, code := sendWithSource(t, addr, "203.0.113.42", "/"); code != 200 {
		t.Fatalf("请求应成功，状态码 %d", code)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("应收到 1 次请求，实际 %d", len(got))
	}
	if got[0] != "203.0.113.42" {
		t.Fatalf("handler 应看到 PROXY 声明的 203.0.113.42，实际 %s；"+
			"这说明解析结果没有接到 RemoteAddr 上，线上仍会是所有请求共用一个限流桶", got[0])
	}
}

// TestRateLimitSeparatesProxiedSources 验证限流按真实来源分桶。
//
// 这是本次修复真正要解决的用户可见问题：在修复前，所有公网请求
// 都呈现为 127.0.0.1，共用一个每分钟 30 次的桶——一个爬虫就能把
// 正常用户的下载额度刷光，用户拿到 429。
func TestRateLimitSeparatesProxiedSources(t *testing.T) {
	limiter := newRateLimiter(3, time.Minute)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.allow(clientIP(r)) {
			http.Error(w, "请求过于频繁", http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})

	addr := startProxyListener(t, h)

	// A 用满自己的额度（3 次）。
	for i := 0; i < 3; i++ {
		if _, code := sendWithSource(t, addr, "203.0.113.1", "/"); code != 200 {
			t.Fatalf("第 %d 次应成功，状态码 %d", i+1, code)
		}
	}
	// A 的第 4 次应被限流。
	if _, code := sendWithSource(t, addr, "203.0.113.1", "/"); code != http.StatusTooManyRequests {
		t.Fatalf("超过额度应返回 429，实际 %d", code)
	}

	// 关键断言：另一个来源不该受影响。
	// 修复前所有请求都是 127.0.0.1，这里必然也是 429——那正是线上症状。
	if _, code := sendWithSource(t, addr, "203.0.113.2", "/"); code != 200 {
		t.Fatalf("不同来源应各自独立计数，B 的首次请求应成功，实际 %d；"+
			"返回 429 说明仍在共用同一个桶", code)
	}
}

// TestRequestsWithoutHeaderShareLoopbackBucket 记录未带头连接的行为。
//
// 不带 PROXY 头时（本机调试、健康检查、灰度期间 nginx 还没改），
// 这些连接确实共用回环地址那一个桶。这是已知且有意的取舍：
// 它们的量极小（健康检查每分钟一次），而"为了让它们也独立计数
// 就去信任 HTTP 头"会让限流可被任意绕过。
func TestRequestsWithoutHeaderShareLoopbackBucket(t *testing.T) {
	var seen []string
	var mu sync.Mutex
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, clientIP(r))
		mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	})
	addr := startProxyListener(t, h)

	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer c.Close()
	if _, err := io.WriteString(c, "GET / HTTP/1.1\r\nHost: dec.test\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("写请求失败: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatalf("读响应失败: %v", err)
	}
	resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("应收到 1 次请求，实际 %d", len(seen))
	}
	if !strings.HasPrefix(seen[0], "127.0.0.1") {
		t.Fatalf("无头连接应保持回环地址，实际 %s", seen[0])
	}
}

// 确保 httptest 仍被使用（其它测试文件依赖），避免 import 抖动。
var _ = httptest.NewServer
