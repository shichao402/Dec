package agent

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// 本机 HTTPS 入口（agent-tray-access 需求 4.7、4.8、4.5）。
//
// 持有有效证书时，在 127.0.0.1:443 上额外提供与回环 HTTP 完全相同的处理器，
// 使 https://local.dec.firoyang.com 不带端口即可打开。443 被占用（或系统不允许
// 普通用户绑定）时依次退到 HTTP 端口 +1、系统分配端口，并通过 Status 报告实际地址。
//
// 证书经 GetCertificate 热加载：新证书落盘后调用 Reload 即可，无需重启进程，
// 也不会断开已有连接。没有证书时不监听，HTTP 回环入口不受任何影响。

// HTTPSOptions 是本机 HTTPS 的参数。
type HTTPSOptions struct {
	Store   *LocalTLSStore
	Handler http.Handler
	// HTTPPort 是回环 HTTP 端口，用于计算回退端口。
	HTTPPort int
	// Ports 覆盖候选端口顺序，仅测试使用；为空时取 443、HTTPPort+1、0。
	Ports []int
	// OnListen 在开始监听后调用，参数是实际端口，用于登记 Host 校验白名单。
	OnListen func(port string)
	// Notifier 用于证书到期告警，可为空。
	Notifier Notifier
	Logf     func(format string, args ...any)
	Now      func() time.Time
}

// HTTPS 是本机 HTTPS 服务。
type HTTPS struct {
	opts HTTPSOptions

	mu       sync.Mutex
	cert     *tls.Certificate
	meta     *LocalTLSMeta
	loadErr  error
	listenEr error
	addr     string
	srv      *http.Server
	// warnedDay 记录上一次到期告警的日期，避免每次检查都弹通知。
	warnedDay string
}

// NewHTTPS 构造本机 HTTPS 服务。
func NewHTTPS(opts HTTPSOptions) *HTTPS {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &HTTPS{opts: opts}
}

// Reload 从存储重新加载证书；有可用证书且尚未监听时开始监听。
func (h *HTTPS) Reload() {
	cert, meta, err := h.opts.Store.Load()
	h.mu.Lock()
	h.loadErr = err
	if err == nil {
		h.cert, h.meta = cert, meta
	}
	listening := h.srv != nil
	usable := h.cert != nil && h.meta != nil && h.opts.Now().Before(h.meta.NotAfter)
	h.mu.Unlock()

	if err != nil {
		h.opts.Logf("本机证书不可用: %v", err)
	}
	if usable && !listening {
		if lerr := h.listen(); lerr != nil {
			h.opts.Logf("本机 HTTPS 监听失败: %v", lerr)
		}
	}
	h.checkExpiry()
}

func (h *HTTPS) candidatePorts() []int {
	if len(h.opts.Ports) > 0 {
		return h.opts.Ports
	}
	ports := []int{443}
	if h.opts.HTTPPort > 0 && h.opts.HTTPPort < 65535 {
		ports = append(ports, h.opts.HTTPPort+1)
	}
	return append(ports, 0)
}

func (h *HTTPS) listen() error {
	var ln net.Listener
	var errs []error
	for _, p := range h.candidatePorts() {
		l, err := net.Listen("tcp", net.JoinHostPort(loopbackHost, strconv.Itoa(p)))
		if err == nil {
			ln = l
			break
		}
		errs = append(errs, err)
	}
	if ln == nil {
		err := fmt.Errorf("所有候选端口都无法监听: %w", errors.Join(errs...))
		h.mu.Lock()
		h.listenEr = err
		h.mu.Unlock()
		return err
	}
	srv := &http.Server{
		Handler:           h.opts.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: h.getCertificate,
		},
	}
	addr := ln.Addr().String()
	_, port, _ := net.SplitHostPort(addr)
	if h.opts.OnListen != nil {
		h.opts.OnListen(port)
	}
	h.mu.Lock()
	h.srv, h.addr, h.listenEr = srv, addr, nil
	h.mu.Unlock()
	go func() {
		if err := srv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.opts.Logf("本机 HTTPS 退出: %v", err)
			h.mu.Lock()
			h.listenEr, h.srv, h.addr = err, nil, ""
			h.mu.Unlock()
		}
	}()
	if port != "443" {
		h.opts.Logf("443 不可用，本机 HTTPS 改在 https://%s:%s", LocalDomain, port)
	} else {
		h.opts.Logf("本机 HTTPS 已启动：https://%s", LocalDomain)
	}
	return nil
}

func (h *HTTPS) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cert == nil {
		return nil, errors.New("本机证书未加载")
	}
	return h.cert, nil
}

// Addr 返回实际监听地址，未监听时为空。
func (h *HTTPS) Addr() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.addr
}

// Status 返回 HTTPS 状态快照。
func (h *HTTPS) Status() TLSStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := TLSStatus{Addr: h.addr, State: TLSNone}
	if h.meta != nil {
		st.NotAfter = h.meta.NotAfter
		st.State = ClassifyTLS(h.meta.NotAfter, h.opts.Now())
	}
	switch {
	case h.loadErr != nil && h.cert == nil:
		st.State, st.Error = TLSError, h.loadErr.Error()
	case h.listenEr != nil && h.addr == "" && h.cert != nil:
		st.State, st.Error = TLSError, h.listenEr.Error()
	}
	return st
}

// checkExpiry 在证书即将到期或已过期时发系统通知，每天最多一次（需求 4.8）。
func (h *HTTPS) checkExpiry() {
	st := h.Status()
	if st.State != TLSExpiring && st.State != TLSExpired {
		return
	}
	day := h.opts.Now().Format("2006-01-02")
	h.mu.Lock()
	if h.warnedDay == day {
		h.mu.Unlock()
		return
	}
	h.warnedDay = day
	h.mu.Unlock()

	title := "Dec 本机证书即将到期"
	if st.State == TLSExpired {
		title = "Dec 本机证书已过期"
	}
	body := fmt.Sprintf("%s 到期时间 %s。云端应自动续期，请检查云端告警；回环地址仍可使用。",
		LocalDomain, st.NotAfter.Local().Format("2006-01-02 15:04"))
	h.opts.Logf("%s：%s", title, body)
	if h.opts.Notifier != nil {
		_ = h.opts.Notifier.Notify(title, body)
	}
}

// Run 启动时加载一次证书，之后周期检查到期状态，阻塞到 ctx 结束。
func (h *HTTPS) Run(ctx context.Context, every time.Duration) {
	h.Reload()
	if every <= 0 {
		every = 6 * time.Hour
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Reload()
		}
	}
}

// Close 停止 HTTPS 服务。
func (h *HTTPS) Close() error {
	h.mu.Lock()
	srv := h.srv
	h.srv, h.addr = nil, ""
	h.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Close()
}
