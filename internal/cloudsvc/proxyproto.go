package cloudsvc

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

// 解析 HAProxy PROXY protocol（v1 文本格式与 v2 二进制格式）。
//
// 为什么需要它：生产拓扑是
//
//	公网 443 → nginx stream(ssl_preread + PROXY protocol) → 127.0.0.1:8445 → dec-cloud 127.0.0.1:8444
//
// nginx 在 443 那一跳就写入了真实来源地址，但中间的 8445 只是字节转发，
// 它把这个头剥掉了。所以 dec-cloud 拿到的 RemoteAddr 永远是 127.0.0.1。
//
// 这不只是"审计里 remote_ip 没有取证价值"，更要紧的是限流：
// rateLimiter 按 IP 分桶（每分钟 30 次），所有公网请求共用一个桶，
// 任意一个爬虫都能把正常用户的下载额度刷光。
//
// 修法：让 8445 把 PROXY 头重新发给 8444（nginx 侧一行配置），
// 这里解析出来填进 RemoteAddr。不改 SNI 分流、不改 systemd 单元、不改回滚路径。
//
// 安全性：只在监听回环时允许开启（见 Config.ProxyProtocol 与 requireLoopbackListen）。
// 公网直连时若能自报来源，伪造 IP 会让限流可被无限绕过，比"地址不准"更糟。

// proxyProtocolListener 把底层 listener 的每条连接包装成能读 PROXY 头的连接。
type proxyProtocolListener struct {
	net.Listener
}

func (l *proxyProtocolListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &proxyProtocolConn{Conn: c}, nil
}

// proxyProtocolConn 在首次访问时解析 PROXY 头，之后把剩余字节交给上层。
type proxyProtocolConn struct {
	net.Conn

	// mu 保护下面的状态。一条连接通常只会被一个 goroutine 使用，
	// 但 RemoteAddr 与 Read 可能来自不同调用路径（TLS 握手在 Read 里，
	// 而 http.Server 会单独问 RemoteAddr），所以还是要串行化。
	mu     sync.Mutex
	br     *bufio.Reader
	parsed bool
	// src 是 PROXY 头声明的来源地址；为 nil 表示这条连接没有 PROXY 头。
	src net.Addr
}

// Read 在首次调用时解析 PROXY 头，之后直接从缓冲读。
func (c *proxyProtocolConn) Read(p []byte) (int, error) {
	if err := c.ensureParsed(); err != nil {
		return 0, err
	}
	return c.br.Read(p)
}

// RemoteAddr 返回 PROXY 头声明的来源地址。
//
// 这里也要触发解析，不能假设 Read 一定先发生：http.Server 在把地址填进
// http.Request.RemoteAddr 时会问 RemoteAddr，而我们无法保证那一刻握手已经读过数据。
// 若懒解析只挂在 Read 上，就可能出现"头还没解析、地址已取走"，
// 结果限流与审计拿到的仍是 127.0.0.1 —— 修复看起来生效了，实际没生效。
func (c *proxyProtocolConn) RemoteAddr() net.Addr {
	if err := c.ensureParsed(); err != nil {
		// 解析失败不该让调用方拿到错误地址之外的异常：
		// 退回底层地址，让连接按原有（不带头）语义继续。
		return c.Conn.RemoteAddr()
	}
	if c.src != nil {
		return c.src
	}
	return c.Conn.RemoteAddr()
}

func (c *proxyProtocolConn) ensureParsed() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.parsed {
		return nil
	}
	err := c.parseHeaderLocked()
	if err != nil {
		return err
	}
	c.parsed = true
	return nil
}

// proxyV2Signature 是 PROXY protocol v2 的 12 字节固定前缀。
var proxyV2Signature = []byte{
	0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A,
}

// maxProxyV1Len 是 v1 头的最大长度（含 CRLF），规范规定不超过 107 字节。
const maxProxyV1Len = 107

// parseHeaderLocked 解析 PROXY 头。调用方需持有 c.mu。
//
// 没有头时不算错误：本机直连（调试、健康检查、以及尚未改造的 nginx）
// 本来就不带 PROXY 头，此时保持原样继续，绝不能因此断开连接。
// 这一点是灰度发布的前提——先上能解析的服务端，再改 nginx 发头，两步都不中断。
func (c *proxyProtocolConn) parseHeaderLocked() error {
	c.br = bufio.NewReader(c.Conn)

	head, err := c.br.Peek(len(proxyV2Signature))
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			// 连接太短：没有头，按原样继续（后续 Read 会返回 EOF）。
			return nil
		}
		return err
	}

	if bytes.Equal(head, proxyV2Signature) {
		return c.parseV2()
	}
	if bytes.HasPrefix(head, []byte("PROXY ")) {
		return c.parseV1()
	}
	// 既不是 v1 也不是 v2：正常 TLS 字节流，不带头。
	return nil
}

// parseV1 解析 "PROXY TCP4 1.2.3.4 5.6.7.8 1234 5678\r\n"。
func (c *proxyProtocolConn) parseV1() error {
	var line strings.Builder
	terminated := false
	for line.Len() <= maxProxyV1Len {
		b, err := c.br.ReadByte()
		if err != nil {
			return fmt.Errorf("读取 PROXY v1 头失败: %w", err)
		}
		line.WriteByte(b)
		if b == '\n' {
			terminated = true
			break
		}
	}
	if !terminated {
		return fmt.Errorf("PROXY v1 头超过 %d 字节上限仍未结束", maxProxyV1Len)
	}
	s := strings.TrimSuffix(strings.TrimSuffix(line.String(), "\n"), "\r")
	fields := strings.Fields(s)

	// "PROXY UNKNOWN" 是合法形式：来源不可知，按不带头处理。
	if len(fields) < 2 || fields[1] == "UNKNOWN" {
		return nil
	}
	// PROXY TCP4|TCP6 <src> <dst> <sport> <dport>
	if len(fields) < 6 {
		return fmt.Errorf("PROXY v1 头字段不足: %q", s)
	}
	ip := net.ParseIP(fields[2])
	if ip == nil {
		return fmt.Errorf("PROXY v1 来源地址无法解析: %q", fields[2])
	}
	port, err := strconv.Atoi(fields[4])
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("PROXY v1 来源端口无法解析: %q", fields[4])
	}
	c.src = &net.TCPAddr{IP: ip, Port: port}
	return nil
}

// parseV2 解析二进制格式。
//
// 结构：12 字节签名 + 1 字节版本/命令 + 1 字节地址族/协议
// + 2 字节长度 + 该长度的地址信息。
func (c *proxyProtocolConn) parseV2() error {
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(c.br, hdr); err != nil {
		return fmt.Errorf("读取 PROXY v2 头失败: %w", err)
	}
	if v := hdr[12] >> 4; v != 0x2 {
		return fmt.Errorf("PROXY v2 版本号异常: %d", v)
	}
	addrLen := int(binary.BigEndian.Uint16(hdr[14:16]))

	// 命令 0 = LOCAL（健康检查等，来源不可信），1 = PROXY。
	if hdr[12]&0x0F != 0x1 {
		if addrLen > 0 {
			if _, err := c.br.Discard(addrLen); err != nil {
				return fmt.Errorf("跳过 PROXY v2 本地连接地址失败: %w", err)
			}
		}
		return nil
	}

	body := make([]byte, addrLen)
	if _, err := io.ReadFull(c.br, body); err != nil {
		return fmt.Errorf("读取 PROXY v2 地址失败: %w", err)
	}

	switch hdr[13] >> 4 {
	case 0x1: // AF_INET：源 4 + 目的 4 + 源端口 2 + 目的端口 2
		if len(body) < 12 {
			return fmt.Errorf("PROXY v2 IPv4 地址长度不足: %d", len(body))
		}
		c.src = &net.TCPAddr{
			IP:   net.IP(body[0:4]),
			Port: int(binary.BigEndian.Uint16(body[8:10])),
		}
	case 0x2: // AF_INET6：源 16 + 目的 16 + 源端口 2 + 目的端口 2
		if len(body) < 36 {
			return fmt.Errorf("PROXY v2 IPv6 地址长度不足: %d", len(body))
		}
		c.src = &net.TCPAddr{
			IP:   net.IP(body[0:16]),
			Port: int(binary.BigEndian.Uint16(body[32:34])),
		}
	default:
		// AF_UNIX 等：没有可用于限流的 IP，按原样继续。
		return nil
	}
	return nil
}

// newProxyProtocolListener 按需包装 listener。
func newProxyProtocolListener(ln net.Listener, enabled bool) net.Listener {
	if !enabled {
		return ln
	}
	return &proxyProtocolListener{Listener: ln}
}
