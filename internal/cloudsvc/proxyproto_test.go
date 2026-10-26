package cloudsvc

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// PROXY protocol 解析的测试。
//
// 这里要守住的核心不是"能解析出 IP"，而是三件事：
//  1. 带头时能拿到真实来源（限流才能按来源分桶，审计才有取证价值）。
//  2. 不带头时连接照常工作——这是灰度发布的前提：先上能解析的服务端，
//     再改 nginx 发头，两步之间不能断。
//  3. 解析失败时不能把连接变成"读到半个头就卡住"。

// 起一对内存连接，一端写入原始字节，另一端交给解析器读。
func pipeProxyConn(t *testing.T, payload string) (net.Conn, *proxyProtocolConn) {
	t.Helper()
	client, server := net.Pipe()
	conn := &proxyProtocolConn{Conn: server}
	go func() {
		defer client.Close()
		_, _ = io.WriteString(client, payload)
	}()
	t.Cleanup(func() { _ = server.Close() })
	return client, conn
}

// TestProxyV1IPv4 验证文本格式能解析出来源。
func TestProxyV1IPv4(t *testing.T) {
	const rest = "hello"
	_, conn := pipeProxyConn(t, "PROXY TCP4 203.0.113.7 127.0.0.1 54321 443\r\n"+rest)

	// 先取地址（模拟 http.Server 在 Read 之前问 RemoteAddr）：
	// 若解析只挂在 Read 上，这里会退回 127.0.0.1，等于修复没生效。
	if got := conn.RemoteAddr().String(); got != "203.0.113.7:54321" {
		t.Fatalf("来源地址应为 203.0.113.7:54321，实际 %s", got)
	}

	// 头之后的内容必须原样交给上层，不能吞掉。
	buf := make([]byte, len(rest))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("读取头之后的内容失败: %v", err)
	}
	if string(buf) != rest {
		t.Fatalf("头之后的字节应为 %q，实际 %q", rest, string(buf))
	}
}

// TestProxyV1IPv6 验证 v6 地址。
func TestProxyV1IPv6(t *testing.T) {
	_, conn := pipeProxyConn(t, "PROXY TCP6 2001:db8::1 2001:db8::2 1234 443\r\n")
	if got := conn.RemoteAddr().String(); got != "[2001:db8::1]:1234" {
		t.Fatalf("来源地址应为 [2001:db8::1]:1234，实际 %s", got)
	}
}

// TestProxyV1UnknownKeepsOriginal 验证 UNKNOWN 时退回底层地址。
func TestProxyV1UnknownKeepsOriginal(t *testing.T) {
	client, server := net.Pipe()
	conn := &proxyProtocolConn{Conn: server}
	go func() {
		defer client.Close()
		_, _ = io.WriteString(client, "PROXY UNKNOWN\r\nbody")
	}()
	t.Cleanup(func() { _ = server.Close() })
	_ = client

	// UNKNOWN 表示来源不可知：应保持底层连接的地址，不编造一个。
	if got, want := conn.RemoteAddr(), server.RemoteAddr(); got.String() != want.String() {
		t.Fatalf("UNKNOWN 时应保持原地址 %s，实际 %s", want, got)
	}
}

// TestNoHeaderPassesThrough 验证不带 PROXY 头时连接照常可用。
//
// 这是灰度前提：服务端先上线，nginx 还没改，此时所有连接都不带头。
// 如果这里出错，整个服务会在部署那一刻不可用。
func TestNoHeaderPassesThrough(t *testing.T) {
	const payload = "\x16\x03\x01 hello-tls"
	_, conn := pipeProxyConn(t, payload)

	// 地址应保持底层（127.0.0.1 之类的 pipe 地址），不报错。
	if conn.RemoteAddr() == nil {
		t.Fatal("无头连接不应没有地址")
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("无头连接读取失败: %v", err)
	}
	if string(buf) != payload {
		t.Fatalf("无头连接的字节被改动: %q", string(buf))
	}
}

// TestProxyV2IPv4 验证二进制格式。
func TestProxyV2IPv4(t *testing.T) {
	// 12 字节签名 + 版本/命令(0x21) + 族/协议(0x11) + 长度(12) + 地址
	body := []byte{
		203, 0, 113, 9, // 源 IP
		127, 0, 0, 1, // 目的 IP
		0x1F, 0x90, // 源端口 8080
		0x01, 0xBB, // 目的端口 443
	}
	var b strings.Builder
	b.Write(proxyV2Signature)
	b.WriteByte(0x21) // v2, PROXY
	b.WriteByte(0x11) // AF_INET, STREAM
	b.Write([]byte{0x00, 0x0C})
	b.Write(body)
	const rest = "TLS"
	_, conn := pipeProxyConn(t, b.String()+rest)

	if got := conn.RemoteAddr().String(); got != "203.0.113.9:8080" {
		t.Fatalf("来源地址应为 203.0.113.9:8080，实际 %s", got)
	}
	buf := make([]byte, len(rest))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if string(buf) != rest {
		t.Fatalf("头之后字节应为 %q，实际 %q", rest, string(buf))
	}
}

// TestProxyV2LocalCommandIgnored 验证 LOCAL 命令不采信来源。
//
// LOCAL 表示"这条连接不来自代理"，来源地址字段不可信，必须忽略。
func TestProxyV2LocalCommandIgnored(t *testing.T) {
	body := []byte{1, 2, 3, 4, 5, 6, 7, 8, 0x00, 0x01, 0x00, 0x02}
	var b strings.Builder
	b.Write(proxyV2Signature)
	b.WriteByte(0x20) // v2, LOCAL
	b.WriteByte(0x11)
	b.Write([]byte{0x00, 0x0C})
	b.Write(body)
	_, conn := pipeProxyConn(t, b.String()+"x")

	if got, want := conn.RemoteAddr(), conn.Conn.RemoteAddr(); got.String() != want.String() {
		t.Fatalf("LOCAL 命令应采用底层地址 %s，实际 %s", want, got)
	}
}

// TestMalformedHeaderDoesNotHang 验证畸形头不会让连接卡死。
func TestMalformedHeaderDoesNotHang(t *testing.T) {
	_, conn := pipeProxyConn(t, "PROXY TCP4 not-an-ip 127.0.0.1 1 2\r\n")

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 8)
		_, _ = conn.Read(buf)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("畸形 PROXY 头导致读取阻塞")
	}
}

// TestShortConnectionDoesNotHang 验证连接立刻关闭不阻塞。
func TestShortConnectionDoesNotHang(t *testing.T) {
	client, server := net.Pipe()
	conn := &proxyProtocolConn{Conn: server}
	_ = client.Close()
	t.Cleanup(func() { _ = server.Close() })

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4)
		_, _ = conn.Read(buf)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("空连接导致读取阻塞")
	}
}

// TestLoopbackGuard 验证信任 PROXY 时必须监听回环。
func TestLoopbackGuard(t *testing.T) {
	ok := []string{"127.0.0.1:8444", "localhost:8444", "[::1]:8444"}
	for _, addr := range ok {
		if err := requireLoopbackListen(addr); err != nil {
			t.Fatalf("%s 应被接受，实际拒绝: %v", addr, err)
		}
	}
	bad := []string{"0.0.0.0:8444", ":8444", "203.0.113.1:8444", ""}
	for _, addr := range bad {
		if err := requireLoopbackListen(addr); err == nil {
			t.Fatalf("%s 应被拒绝（公网监听 + 信任自报来源 = 限流可绕过），实际接受", addr)
		}
	}
}
