package agent

import (
	"crypto/tls"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本机 HTTPS（agent-tray-access 需求 4.7、4.8、4.5）。

type recordNotifier struct {
	mu    sync.Mutex
	items []string
}

func (n *recordNotifier) Notify(title, body string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.items = append(n.items, title)
	return nil
}

func (n *recordNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.items)
}

// httpsGet 用 SNI=LocalDomain 连接 addr，返回响应体与对端证书序列号。
func httpsGet(t *testing.T, addr, path string) (string, string) {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: LocalDomain, InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("TLS 握手失败: %v", err)
	}
	serial := conn.ConnectionState().PeerCertificates[0].SerialNumber.Text(16)
	_, port, _ := net.SplitHostPort(addr)
	req := "GET " + path + " HTTP/1.1\r\nHost: " + LocalDomain + ":" + port + "\r\nConnection: close\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(conn)
	conn.Close()
	return string(body), serial
}

func newTestHTTPS(t *testing.T, store *LocalTLSStore, n Notifier) (*HTTPS, *Loopback) {
	t.Helper()
	loop := newTestLoopback(t, LoopbackOptions{})
	h := NewHTTPS(HTTPSOptions{
		Store:    store,
		Handler:  loop.Handler(),
		Ports:    []int{0},
		OnListen: func(p string) { loop.SetExtraPorts(p) },
		Notifier: n,
		Logf:     t.Logf,
	})
	t.Cleanup(func() { _ = h.Close() })
	return h, loop
}

func TestHTTPSNoCertDoesNotListen(t *testing.T) {
	h, _ := newTestHTTPS(t, NewLocalTLSStore(t.TempDir()), nil)
	h.Reload()
	if h.Addr() != "" {
		t.Fatal("没有证书时不应监听")
	}
	if st := h.Status(); st.State != TLSNone {
		t.Fatalf("状态应为 none: %+v", st)
	}
}

func TestHTTPSServesAndHotReloads(t *testing.T) {
	store := NewLocalTLSStore(t.TempDir())
	b1 := testBundle(t, 100)
	if err := store.Save(b1); err != nil {
		t.Fatal(err)
	}
	h, _ := newTestHTTPS(t, store, nil)
	h.Reload()
	addr := h.Addr()
	if addr == "" {
		t.Fatal("有证书时应监听")
	}
	body, serial := httpsGet(t, addr, "/api/status")
	if !strings.Contains(body, "200 OK") {
		t.Fatalf("经 HTTPS 访问状态接口失败: %s", body)
	}
	if serial != b1.Serial {
		t.Fatalf("证书序列号 %s，期望 %s", serial, b1.Serial)
	}
	if st := h.Status(); st.State != TLSOK || st.Addr != addr {
		t.Fatalf("状态: %+v", st)
	}

	// 热替换：不重启监听，新连接拿到新证书。
	b2 := testBundle(t, 200)
	if err := store.Save(b2); err != nil {
		t.Fatal(err)
	}
	h.Reload()
	if h.Addr() != addr {
		t.Fatal("热加载不应更换监听地址")
	}
	if _, serial := httpsGet(t, addr, "/api/status"); serial != b2.Serial {
		t.Fatalf("热加载后序列号 %s，期望 %s", serial, b2.Serial)
	}
}

func TestHTTPSPortFallback(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	_, busyPort, _ := net.SplitHostPort(busy.Addr().String())

	store := NewLocalTLSStore(t.TempDir())
	if err := store.Save(testBundle(t, 1)); err != nil {
		t.Fatal(err)
	}
	loop := newTestLoopback(t, LoopbackOptions{})
	p, _ := net.LookupPort("tcp", busyPort)
	h := NewHTTPS(HTTPSOptions{
		Store:    store,
		Handler:  loop.Handler(),
		Ports:    []int{p, 0},
		OnListen: func(port string) { loop.SetExtraPorts(port) },
		Logf:     t.Logf,
	})
	defer h.Close()
	h.Reload()
	_, got, _ := net.SplitHostPort(h.Addr())
	if got == "" || got == busyPort {
		t.Fatalf("被占用时应退到其他端口，实际 %q", h.Addr())
	}
	urls := AccessURLs(loop.Addr(), h.Status())
	if !urls[0].Available || !strings.HasSuffix(urls[0].URL, ":"+got) {
		t.Fatalf("访问地址应带实际端口: %+v", urls[0])
	}
}

func TestHTTPSExpiryWarnsOncePerDay(t *testing.T) {
	store := NewLocalTLSStore(t.TempDir())
	b := testBundle(t, 9)
	if err := store.Save(b); err != nil {
		t.Fatal(err)
	}
	n := &recordNotifier{}
	h, _ := newTestHTTPS(t, store, n)
	now := b.NotAfter.Add(-10 * 24 * time.Hour)
	h.opts.Now = func() time.Time { return now }
	h.Reload()
	h.Reload()
	if st := h.Status(); st.State != TLSExpiring {
		t.Fatalf("剩 10 天应为 expiring: %+v", st)
	}
	if n.count() != 1 {
		t.Fatalf("同一天只应告警一次，实际 %d", n.count())
	}
	now = now.Add(24 * time.Hour)
	h.Reload()
	if n.count() != 2 {
		t.Fatalf("次日应再次告警，实际 %d", n.count())
	}

	// 已过期：不再新开监听，但不影响已在跑的服务。
	now = b.NotAfter.Add(time.Hour)
	h.Reload()
	if st := h.Status(); st.State != TLSExpired {
		t.Fatalf("应为 expired: %+v", st)
	}
	urls := AccessURLs("127.0.0.1:8787", h.Status())
	if urls[0].Available {
		t.Fatal("过期证书不应作为首选地址")
	}
}
