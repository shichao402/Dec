package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/proto"
)

// 与服务端通讯的客户端（需求 2.4、3.3、5.4）。
//
// 只有一条出站方向：代理主动连服务端。没有入站公网端口，也没有自更新。

// 常见错误。
var (
	// ErrNotConfigured 表示还没有配置服务地址。
	ErrNotConfigured = errors.New("未配置服务地址")
	// ErrNotAligned 表示版本与服务端要求不一致。
	ErrNotAligned = errors.New("版本不对齐")
	// ErrIdentityRejected 表示服务端拒绝了本机身份（吊销、过期、序列号不符）。
	ErrIdentityRejected = errors.New("设备身份被拒绝")
	// ErrMaintenance 表示服务端处于维护状态。
	ErrMaintenance = errors.New("服务端维护中")
)

// Client 是协议客户端。
type Client struct {
	// baseURL 是服务地址，形如 https://dec.firoyang.com。
	baseURL string
	// http 是底层 HTTP 客户端，已配置 mTLS。
	http *http.Client
	// version 是本机代理版本，每个请求都带上（需求 19.4）。
	version string
	// identity 返回当前身份，可能为空（未登记）。
	identity func() *Identity

	mu   sync.Mutex
	seq  uint64
	last *proto.HelloResponse
}

// ClientOptions 是构造参数。
type ClientOptions struct {
	// BaseURL 是服务地址。必须带 scheme，https 之外一律拒绝。
	BaseURL string
	// Version 是代理版本。
	Version string
	// Identity 返回当前身份，可为 nil。
	Identity func() *Identity
	// Timeout 是单次请求超时。长轮询会超过它，所以按方法单独放宽。
	Timeout time.Duration
	// Insecure 仅用于本机调试与测试，绝不在正常安装路径上暴露。
	Insecure bool
}

// NewClient 构造客户端。
func NewClient(opts ClientOptions) (*Client, error) {
	base, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return nil, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		// 严格校验：用系统根证书验证服务端，不做 TOFU（需求 3.3）。
		RootCAs: nil,
	}
	if opts.Insecure {
		tlsCfg.InsecureSkipVerify = true
	}
	tr := &http.Transport{
		TLSClientConfig:     tlsCfg,
		ForceAttemptHTTP2:   true,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{
		baseURL:  base,
		http:     &http.Client{Transport: &certTransport{base: tr}, Timeout: timeout},
		version:  opts.Version,
		identity: opts.Identity,
	}, nil
}

// certTransport 按请求注入客户端证书。
//
// 证书不能写在共享 Transport 的 TLSClientConfig 里：那样并发请求之间会串证书，
// 而且 hello 这类按方法决定是否带证书的路径无法表达。改成从 context 取，
// 每个请求各自决定带不带。
type certTransport struct {
	base *http.Transport
}

// RoundTrip 实现 http.RoundTripper。
func (t *certTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cert, ok := clientCertFrom(req.Context())
	if !ok {
		return t.base.RoundTrip(req)
	}
	// 克隆 Transport：TLSClientConfig 里的 GetClientCertificate 是每次握手才调用的，
	// 但仍然要按请求隔离，避免把某一台设备的证书留在连接池的配置里。
	clone := t.base.Clone()
	cfg := clone.TLSClientConfig.Clone()
	cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		return &cert, nil
	}
	clone.TLSClientConfig = cfg
	resp, err := clone.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// 克隆出来的 Transport 由这次请求独占，交给响应体关闭时释放。
	resp.Body = &closingBody{ReadCloser: resp.Body, closeFn: clone.CloseIdleConnections}
	return resp, nil
}

// closingBody 在关闭响应体时释放一次性 Transport。
type closingBody struct {
	io.ReadCloser
	closeFn func()
}

// Close 关闭响应体并释放连接。
func (b *closingBody) Close() error {
	err := b.ReadCloser.Close()
	if b.closeFn != nil {
		b.closeFn()
	}
	return err
}

// normalizeBaseURL 校验并规范化服务地址。
func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrNotConfigured
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("服务地址无法解析: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("服务地址必须是 https：%q", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("服务地址缺少主机名：%q", raw)
	}
	// 统一去掉尾部斜杠，避免拼出 //api。
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// BaseURL 返回规范化后的服务地址。
func (c *Client) BaseURL() string { return c.baseURL }

// DownloadURL 返回要求版本的下载地址（需求 19.5）。
func (c *Client) DownloadURL() string { return c.baseURL + "/download" }

// Hello 上报版本并获取服务端要求版本（需求 19.7）。
//
// 这是唯一保证永不破坏兼容的方法：未登记、证书过期、版本不对齐时都要能调。
func (c *Client) Hello(ctx context.Context, in proto.HelloRequest) (*proto.HelloResponse, error) {
	var out proto.HelloResponse
	if err := c.call(ctx, proto.MethodHello, proto.SourceAgent, in, &out, true); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.last = &out
	c.mu.Unlock()
	return &out, nil
}

// LastHello 返回最近一次握手结果。
func (c *Client) LastHello() *proto.HelloResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// Sync 发起一次对账。waitSeconds 为 0 表示立即返回。
func (c *Client) Sync(ctx context.Context, in proto.SyncRequest) (*proto.SyncResponse, error) {
	var out proto.SyncResponse
	if err := c.call(ctx, proto.MethodSync, proto.SourceAgent, in, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// Ack 回报落地结果。
func (c *Client) Ack(ctx context.Context, in proto.AckRequest) error {
	var out proto.AckResponse
	return c.call(ctx, proto.MethodAck, proto.SourceAgent, in, &out, false)
}

// Enroll 提交登记请求（需求 18.4）。
func (c *Client) Enroll(ctx context.Context, in proto.EnrollBeginRequest) (*proto.EnrollBeginResponse, error) {
	var out proto.EnrollBeginResponse
	if err := c.call(ctx, proto.MethodEnroll, proto.SourceEnroll, in, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

// Recover 在恢复模式下取回根密钥并重新登记（需求 11）。
func (c *Client) Recover(ctx context.Context, in proto.RecoverRequest) (*proto.EnrollBeginResponse, error) {
	var out proto.EnrollBeginResponse
	if err := c.call(ctx, proto.MethodRecover, proto.SourceEnroll, in, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

// Call 是通用的协议调用，供 MCP 入口与管理面转发使用。
//
// source 决定服务端的权限判定（需求 13.2、已决策 12）：
// 代理不能替调用方“提权”，来源必须由入口处如实给出。
func (c *Client) Call(ctx context.Context, m proto.Method, src proto.Source, in any, out any) error {
	noCert := proto.NoCertMethods[m] || proto.OptionalCertMethods[m]
	return c.call(ctx, m, src, in, out, noCert)
}

// call 执行一次请求。
func (c *Client) call(ctx context.Context, m proto.Method, src proto.Source, in, out any, noCert bool) error {
	if c == nil || c.baseURL == "" {
		return ErrNotConfigured
	}
	req := &proto.Request{
		Protocol:         proto.Version,
		Method:           m,
		Source:           src,
		RequestedVersion: c.version,
	}
	req.ID = c.nextID()
	if err := req.EncodePayload(in); err != nil {
		return fmt.Errorf("编码请求失败: %w", err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// 需要身份的方法才带证书；免证书方法不附带，避免“顺手带上”把私钥暴露给
	// 一个尚未通过证书校验的端点。
	if !noCert {
		if err := c.attachCert(httpReq); err != nil {
			return err
		}
	} else if id := c.identity(); id != nil {
		// hello 允许带证书：服务端会顺带更新在线状态与上报版本。
		if err := c.attachCert(httpReq); err != nil {
			return err
		}
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务端返回 %d", resp.StatusCode)
	}
	var out2 proto.Response
	if err := json.Unmarshal(raw, &out2); err != nil {
		return fmt.Errorf("响应不是合法协议外壳: %w", err)
	}
	if !out2.OK {
		return errorFromResponse(&out2)
	}
	if out == nil || len(out2.Payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(out2.Payload, out); err != nil {
		return fmt.Errorf("解析响应载荷失败: %w", err)
	}
	return nil
}

// attachCert 给请求挂上设备证书。
func (c *Client) attachCert(req *http.Request) error {
	id := c.identity()
	if id == nil {
		return ErrNoIdentity
	}
	cert, err := id.TLSCertificate()
	if err != nil {
		return err
	}
	// 通过 Context 传证书，避免在共享 Transport 上改写 TLSClientConfig
	// （那会在并发请求之间串证书）。
	*req = *req.WithContext(withClientCert(req.Context(), cert))
	return nil
}

// ctxKey 是本包内部用的 context key 类型。
type ctxKey int

const clientCertKey ctxKey = iota

// withClientCert 把证书放进 context。
func withClientCert(ctx context.Context, cert tls.Certificate) context.Context {
	return context.WithValue(ctx, clientCertKey, cert)
}

// clientCertFrom 从 context 取证书。
func clientCertFrom(ctx context.Context) (tls.Certificate, bool) {
	cert, ok := ctx.Value(clientCertKey).(tls.Certificate)
	return cert, ok
}

// nextID 生成单调递增的请求 ID，便于日志对齐。
func (c *Client) nextID() string {
	c.mu.Lock()
	c.seq++
	n := c.seq
	c.mu.Unlock()
	return fmt.Sprintf("a-%d", n)
}

// errorFromResponse 把协议错误映射成本包的稳定错误。
func errorFromResponse(resp *proto.Response) error {
	switch resp.Code {
	case proto.ErrVersionMismatch:
		return fmt.Errorf("%w: 需要 %s", ErrNotAligned, resp.RequiredVersion)
	case proto.ErrUnauthorized:
		return fmt.Errorf("%w: %s", ErrIdentityRejected, resp.Error)
	case proto.ErrMaintenance:
		return fmt.Errorf("%w: %s", ErrMaintenance, resp.Error)
	}
	return &protoError{code: resp.Code, msg: resp.Error}
}

// protoError 是未被本包特化的协议错误。
type protoError struct {
	code proto.ErrorCode
	msg  string
}

func (e *protoError) Error() string {
	if e.msg == "" {
		return string(e.code)
	}
	return string(e.code) + ": " + e.msg
}

// Code 返回协议错误码，便于调用方判断。
func (e *protoError) Code() proto.ErrorCode { return e.code }

// CodeOf 返回错误的协议错误码，不是协议错误时返回空。
func CodeOf(err error) proto.ErrorCode {
	var pe *protoError
	if errors.As(err, &pe) {
		return pe.code
	}
	if errors.Is(err, ErrNotAligned) {
		return proto.ErrVersionMismatch
	}
	if errors.Is(err, ErrIdentityRejected) {
		return proto.ErrUnauthorized
	}
	if errors.Is(err, ErrMaintenance) {
		return proto.ErrMaintenance
	}
	return ""
}
