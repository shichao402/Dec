package cloudsvc

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 服务端端到端测试。
//
// 用 httptest 的 TLS 服务器 + 真实设备证书模拟 mTLS：
// 只有这样才能验证“证书可选 + 按方法判定身份”这条链路，
// 而不是只在单测里相信函数返回值。

// harness 是一套跑起来的服务端与它的数据目录。
type harness struct {
	t       *testing.T
	dir     string
	srv     *Server
	handler http.Handler
	client  *http.Client
}

// newHarness 启动一个带自签证书的服务端。
func newHarness(t *testing.T, required string) *harness {
	t.Helper()
	return newHarnessAt(t, t.TempDir(), required)
}

// newHarnessWithRootKey 在数据目录里预置根密钥。
//
// 加解密类方法（密钥写入、查看明文）要求服务端已加载根密钥，
// 而全新数据目录会以“待初始化”状态启动，根密钥为空。
func newHarnessWithRootKey(t *testing.T, required string) *harness {
	t.Helper()
	dir := t.TempDir()
	key, err := crypt.NewKey(nil)
	if err != nil {
		t.Fatalf("生成根密钥失败: %v", err)
	}
	if err := crypt.WriteRootKey(filepath.Join(dir, "root.key"), key, time.Now().UTC(), ""); err != nil {
		t.Fatalf("写入根密钥失败: %v", err)
	}
	return newHarnessAt(t, dir, required)
}

// newHarnessAt 在指定数据目录上启动服务端。
func newHarnessAt(t *testing.T, dir, required string) *harness {
	t.Helper()
	srv, err := New(Config{
		Domain:          "dec.test",
		Listen:          "127.0.0.1:0",
		DataDir:         dir,
		ServerVersion:   "1.0.0",
		RequiredVersion: required,
		AcceptAny:       true,
	})
	if err != nil {
		t.Fatalf("构造服务端失败: %v", err)
	}
	// 后台协程（冷备调度、告警发送）随用例结束一起退出。
	t.Cleanup(srv.closeAll)

	return &harness{
		t:       t,
		dir:     dir,
		srv:     srv,
		handler: srv.Handler(),
	}
}

// post 直接调用 handler，不带客户端证书。
func (h *harness) post(req *proto.Request) *proto.Response {
	h.t.Helper()
	return h.postTLS(req, nil)
}

// download 请求一个下载路径，返回状态码。
func (h *harness) download(path string) int {
	h.t.Helper()
	ts := httptest.NewUnstartedServer(h.handler)
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequestClientCert}
	ts.StartTLS()
	defer ts.Close()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		h.t.Fatalf("下载请求失败: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// postTLS 用指定客户端证书调用 handler。
//
// 走真实 TLS 握手，这样 r.TLS.PeerCertificates 与生产环境一致。
func (h *harness) postTLS(req *proto.Request, cert *tls.Certificate) *proto.Response {
	h.t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		h.t.Fatalf("序列化请求失败: %v", err)
	}

	ts := httptest.NewUnstartedServer(h.handler)
	ts.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequestClientCert,
	}
	ts.StartTLS()
	defer ts.Close()

	client := ts.Client()
	client.Timeout = 10 * time.Second
	if cert != nil {
		client.Transport.(*http.Transport).TLSClientConfig.Certificates = []tls.Certificate{*cert}
	}

	resp, err := client.Post(ts.URL+"/api", "application/json", bytes.NewReader(body))
	if err != nil {
		h.t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("读取响应失败: %v", err)
	}
	var out proto.Response
	if err := json.Unmarshal(raw, &out); err != nil {
		h.t.Fatalf("响应不是合法协议外壳: %s", string(raw))
	}
	return &out
}

// registerDevice 登记一台设备并返回它的证书。
func (h *harness) registerDevice(id, name string) *tls.Certificate {
	h.t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		h.t.Fatalf("生成设备密钥失败: %v", err)
	}
	ca := h.srv.boot.CurrentCA()
	issued, err := ca.SignDevice(id, &key.PublicKey, time.Now().Add(24*time.Hour))
	if err != nil {
		h.t.Fatalf("签发设备证书失败: %v", err)
	}
	dev := ssot.Device{
		ID:           id,
		Name:         name,
		Kind:         ssot.DeviceMine,
		OS:           "darwin",
		Arch:         "arm64",
		CertSerial:   issued.Serial,
		CertNotAfter: issued.NotAfter,
	}
	author := ssot.Author{Name: "test", Email: "test@dec.local", Source: "admin"}
	if err := h.srv.reg.Add(dev, author); err != nil {
		h.t.Fatalf("登记设备失败: %v", err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{mustParseDER(h.t, issued.CertPEM)},
		PrivateKey:  key,
	}
}

// mustParseDER 从 PEM 里取 DER。
func mustParseDER(t *testing.T, certPEM []byte) []byte {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatalf("证书 PEM 无法解析")
	}
	return block.Bytes
}

// TestHealthNoCert 验证健康检查无需证书即可访问。
func TestHealthNoCert(t *testing.T) {
	h := newHarness(t, "1.0.0")
	ts := httptest.NewUnstartedServer(h.handler)
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequestClientCert}
	ts.StartTLS()
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 %d，期望 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("健康检查 ok 为 %v", body["ok"])
	}
	if body["protocol"] != float64(proto.Version) {
		t.Fatalf("协议版本为 %v，期望 %d", body["protocol"], proto.Version)
	}
}

// TestHelloWithoutCert 验证 hello 无证书可访问且能拿到要求版本。
func TestHelloWithoutCert(t *testing.T) {
	h := newHarness(t, "2.0.0")
	req := &proto.Request{ID: "1", Protocol: proto.Version, Method: proto.MethodHello}
	if err := req.EncodePayload(&proto.HelloRequest{AgentVersion: "1.0.0"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.post(req)
	if !resp.OK {
		t.Fatalf("hello 失败: %s", resp.Error)
	}
	var out proto.HelloResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if out.RequiredVersion != "2.0.0" {
		t.Fatalf("要求版本为 %q，期望 2.0.0", out.RequiredVersion)
	}
	if out.Aligned {
		t.Fatalf("版本 1.0.0 不应被认为对齐")
	}
	if out.DeviceID != "" {
		t.Fatalf("无证书请求不应该有设备身份，实际 %q", out.DeviceID)
	}
}

// TestSyncRequiresCert 验证需要身份的方法在没有证书时被拒绝。
func TestSyncRequiresCert(t *testing.T) {
	h := newHarness(t, "1.0.0")
	req := &proto.Request{ID: "1", Protocol: proto.Version, Method: proto.MethodSync}
	if err := req.EncodePayload(&proto.SyncRequest{}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.post(req)
	if resp.OK {
		t.Fatal("无证书的 sync 应该被拒绝")
	}
	if resp.Code != proto.ErrUnauthorized {
		t.Fatalf("错误码为 %q，期望 unauthorized", resp.Code)
	}
}

// TestSyncWithCert 验证带合法证书的 sync 能拿到身份并完成一次对账。
func TestSyncWithCert(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	req := &proto.Request{ID: "1", Protocol: proto.Version, Method: proto.MethodSync}
	if err := req.EncodePayload(&proto.SyncRequest{}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if !resp.OK {
		t.Fatalf("sync 失败: %s", resp.Error)
	}
	var out proto.SyncResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !out.Accepted {
		t.Fatal("sync 应被接受")
	}
}

// TestUnregisteredDeviceRejected 验证未登记设备的证书被拒绝。
//
// 证书由本 CA 签发，但登记表里没有这台设备：必须拒绝，
// 否则拿到任意一张签名证书就能读全部内容。
func TestUnregisteredDeviceRejected(t *testing.T) {
	h := newHarness(t, "1.0.0")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	issued, err := h.srv.boot.CurrentCA().SignDevice("ghost", &key.PublicKey, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("签发证书失败: %v", err)
	}
	cert := &tls.Certificate{
		Certificate: [][]byte{mustParseDER(t, issued.CertPEM)},
		PrivateKey:  key,
	}

	req := &proto.Request{ID: "1", Protocol: proto.Version, Method: proto.MethodSync}
	if err := req.EncodePayload(&proto.SyncRequest{}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if resp.OK {
		t.Fatal("未登记设备应被拒绝")
	}
	if resp.Code != proto.ErrUnauthorized {
		t.Fatalf("错误码为 %q，期望 unauthorized", resp.Code)
	}
}

// TestRevokedDeviceRejected 验证吊销立即生效。
func TestRevokedDeviceRejected(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	author := ssot.Author{Name: "test", Email: "test@dec.local", Source: "admin"}
	if _, err := h.srv.reg.Revoke("dev-a", "丢失", author); err != nil {
		t.Fatalf("吊销失败: %v", err)
	}

	req := &proto.Request{ID: "1", Protocol: proto.Version, Method: proto.MethodSync}
	if err := req.EncodePayload(&proto.SyncRequest{}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if resp.OK {
		t.Fatal("吊销设备应被拒绝")
	}
	if resp.Code != proto.ErrUnauthorized {
		t.Fatalf("错误码为 %q，期望 unauthorized", resp.Code)
	}
}

// TestVersionMismatchBlocks 验证版本不对齐时只放行三个方法（需求 19.4）。
func TestVersionMismatchBlocks(t *testing.T) {
	h := newHarness(t, "2.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	req := &proto.Request{
		ID:               "1",
		Protocol:         proto.Version,
		Method:           proto.MethodSync,
		RequestedVersion: "1.0.0",
	}
	if err := req.EncodePayload(&proto.SyncRequest{}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if resp.OK {
		t.Fatal("版本不对齐的 sync 应被拒绝")
	}
	if resp.Code != proto.ErrVersionMismatch {
		t.Fatalf("错误码为 %q，期望 version_mismatch", resp.Code)
	}
	if resp.RequiredVersion != "2.0.0" {
		t.Fatalf("应返回要求版本 2.0.0，实际 %q", resp.RequiredVersion)
	}

	// hello 必须仍然放行，否则旧代理没有自救通道。
	hello := &proto.Request{
		ID:               "2",
		Protocol:         proto.Version,
		Method:           proto.MethodHello,
		RequestedVersion: "1.0.0",
	}
	if err := hello.EncodePayload(&proto.HelloRequest{AgentVersion: "1.0.0"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	hr := h.postTLS(hello, cert)
	if !hr.OK {
		t.Fatalf("版本不对齐时 hello 应放行，实际失败: %s", hr.Error)
	}
}

// TestMaintenanceBlocksWrites 验证维护状态下写操作被拦截（需求 21.4）。
func TestMaintenanceBlocksWrites(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")
	h.srv.SetMaintenance(true)

	req := &proto.Request{ID: "1", Protocol: proto.Version, Method: proto.MethodListAssets}
	if err := req.EncodePayload(&struct{}{}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if resp.OK {
		t.Fatal("维护状态下 list_assets 应被拒绝")
	}
	if resp.Code != proto.ErrMaintenance {
		t.Fatalf("错误码为 %q，期望 maintenance", resp.Code)
	}

	// hello 在维护状态下必须放行，否则代理无从得知维护状态。
	hello := &proto.Request{ID: "2", Protocol: proto.Version, Method: proto.MethodHello}
	if err := hello.EncodePayload(&proto.HelloRequest{AgentVersion: "1.0.0"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	if hr := h.postTLS(hello, cert); !hr.OK {
		t.Fatalf("维护状态下 hello 应放行，实际失败: %s", hr.Error)
	}
}

// TestUnknownMethodRejected 验证未知方法被拒绝。
func TestUnknownMethodRejected(t *testing.T) {
	h := newHarness(t, "1.0.0")
	req := &proto.Request{ID: "1", Protocol: proto.Version, Method: proto.Method("nope")}
	resp := h.post(req)
	if resp.OK {
		t.Fatal("未知方法应被拒绝")
	}
	if resp.Code != proto.ErrInvalid {
		t.Fatalf("错误码为 %q，期望 invalid", resp.Code)
	}
}

// TestMCPCannotReadSecret 验证 MCP 来源读不到明文（已定决策 12）。
func TestMCPCannotReadSecret(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	req := &proto.Request{
		ID:       "1",
		Protocol: proto.Version,
		Method:   proto.MethodReadSecret,
		Source:   proto.SourceMCP,
	}
	if err := req.EncodePayload(&proto.ReadSecretRequest{Name: "db"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if resp.OK {
		t.Fatal("MCP 来源不应读到明文")
	}
	if resp.Code != proto.ErrForbidden {
		t.Fatalf("错误码为 %q，期望 forbidden", resp.Code)
	}
}

// TestReceiverDeviceCannotWrite 验证只接收设备不能写。
func TestReceiverDeviceCannotWrite(t *testing.T) {
	h := newHarness(t, "1.0.0")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	issued, err := h.srv.boot.CurrentCA().SignDevice("dev-r", &key.PublicKey, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("签发证书失败: %v", err)
	}
	dev := ssot.Device{
		ID:           "dev-r",
		Name:         "NUC",
		Kind:         ssot.DeviceReceiver,
		OS:           "linux",
		Arch:         "amd64",
		CertSerial:   issued.Serial,
		CertNotAfter: issued.NotAfter,
	}
	author := ssot.Author{Name: "test", Email: "test@dec.local", Source: "admin"}
	if err := h.srv.reg.Add(dev, author); err != nil {
		t.Fatalf("登记设备失败: %v", err)
	}
	cert := &tls.Certificate{
		Certificate: [][]byte{mustParseDER(t, issued.CertPEM)},
		PrivateKey:  key,
	}

	req := &proto.Request{
		ID:       "1",
		Protocol: proto.Version,
		Method:   proto.MethodWriteAsset,
		Source:   proto.SourceAdmin,
	}
	if err := req.EncodePayload(&proto.AssetPayload{Bundle: "core"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if resp.OK {
		t.Fatal("只接收设备不应能写资产")
	}
	if resp.Code != proto.ErrForbidden {
		t.Fatalf("错误码为 %q，期望 forbidden", resp.Code)
	}
}

// TestDownloadWhitelist 验证下载端点只提供清单里声明的文件（需求 20.1、20.5）。
func TestDownloadWhitelist(t *testing.T) {
	h := newHarness(t, "1.0.0")
	dl := filepath.Join(h.dir, "download")
	if err := os.MkdirAll(dl, 0o755); err != nil {
		t.Fatalf("准备下载目录失败: %v", err)
	}
	// 清单只声明 dec-agent-darwin-arm64；另一个文件在目录里但不在清单里。
	if err := os.WriteFile(filepath.Join(dl, "dec-agent-darwin-arm64"), []byte("binary"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dl, "secret.txt"), []byte("do not serve"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	rel := release{
		Version:         "1.0.0",
		RequiredClient:  "1.0.0",
		Protocol:        proto.Version,
		BuiltAt:         time.Now().UTC().Format(time.RFC3339),
		Files:           []releaseFile{{Name: "dec-agent-darwin-arm64", OS: "darwin", Arch: "arm64", Size: 6}},
	}
	raw, err := json.Marshal(rel)
	if err != nil {
		t.Fatalf("序列化清单失败: %v", err)
	}
	if err := os.WriteFile(releaseManifestPath(h.dir), raw, 0o644); err != nil {
		t.Fatalf("写入清单失败: %v", err)
	}

	ts := httptest.NewUnstartedServer(h.handler)
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequestClientCert}
	ts.StartTLS()
	defer ts.Close()
	client := ts.Client()

	ok, err := client.Get(ts.URL + "/download/dec-agent-darwin-arm64")
	if err != nil {
		t.Fatalf("下载已声明文件失败: %v", err)
	}
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("已声明文件状态码 %d，期望 200", ok.StatusCode)
	}
	body, _ := io.ReadAll(ok.Body)
	if string(body) != "binary" {
		t.Fatalf("下载内容为 %q", string(body))
	}

	// 不在清单里的文件必须拿不到。
	bad, err := client.Get(ts.URL + "/download/secret.txt")
	if err != nil {
		t.Fatalf("请求未声明文件失败: %v", err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusNotFound {
		t.Fatalf("未声明文件状态码 %d，期望 404", bad.StatusCode)
	}

	// 上跳路径必须被拒绝。
	up, err := client.Get(ts.URL + "/download/..%2Froot.key")
	if err != nil {
		t.Fatalf("请求上跳路径失败: %v", err)
	}
	defer up.Body.Close()
	if up.StatusCode != http.StatusNotFound && up.StatusCode != http.StatusBadRequest {
		t.Fatalf("上跳路径状态码 %d，期望 404 或 400", up.StatusCode)
	}
}

// TestDownloadIndex 验证索引页可访问并包含版本信息。
func TestDownloadIndex(t *testing.T) {
	h := newHarness(t, "1.0.0")
	rel := release{Version: "3.1.4", RequiredClient: "3.1.4", Protocol: proto.Version,
		// 托盘放在前面：同平台同架构排序相等，命令示例绝不能取到托盘。
		Files: []releaseFile{
			{Name: "dec-tray-darwin-arm64", OS: "darwin", Arch: "arm64"},
			{Name: "dec-agent-darwin-arm64", OS: "darwin", Arch: "arm64"},
		}}
	raw, _ := json.Marshal(rel)
	if err := os.WriteFile(releaseManifestPath(h.dir), raw, 0o644); err != nil {
		t.Fatalf("写入清单失败: %v", err)
	}

	ts := httptest.NewUnstartedServer(h.handler)
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequestClientCert}
	ts.StartTLS()
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/download")
	if err != nil {
		t.Fatalf("请求索引页失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 %d，期望 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	if !strings.Contains(page, "3.1.4") {
		t.Fatalf("索引页应包含版本 3.1.4")
	}
	if !strings.Contains(page, "<code>dec-tray-darwin-arm64</code>") {
		t.Fatalf("下载列表应包含托盘文件")
	}
	if !strings.Contains(page, "/download/dec-agent-darwin-arm64") {
		t.Fatalf("命令示例应下载代理")
	}
	if strings.Contains(page, "/download/dec-tray-") {
		t.Fatalf("命令示例不应把托盘当成代理安装")
	}
}

// TestAPIRouteTrimming 验证 /api/ 与 /api 都能进协议层。
func TestAPIRouteTrimming(t *testing.T) {
	h := newHarness(t, "1.0.0")
	ts := httptest.NewUnstartedServer(h.handler)
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequestClientCert}
	ts.StartTLS()
	defer ts.Close()

	body, _ := json.Marshal(&proto.Request{
		ID:     "1",
		Method: proto.MethodHello,
	})
	resp, err := ts.Client().Post(ts.URL+"/api/", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 %d，期望 200", resp.StatusCode)
	}
	var out proto.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !out.OK {
		t.Fatalf("hello 失败: %s", out.Error)
	}
}

// TestNonPostRejected 验证协议端点只接受 POST。
func TestNonPostRejected(t *testing.T) {
	h := newHarness(t, "1.0.0")
	ts := httptest.NewUnstartedServer(h.handler)
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequestClientCert}
	ts.StartTLS()
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 %d，期望 405", resp.StatusCode)
	}
	if resp.Header.Get("Allow") != http.MethodPost {
		t.Fatalf("Allow 头为 %q", resp.Header.Get("Allow"))
	}
}

// TestBadJSON 验证非法 JSON 得到稳定的协议错误。
func TestBadJSON(t *testing.T) {
	h := newHarness(t, "1.0.0")
	ts := httptest.NewUnstartedServer(h.handler)
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequestClientCert}
	ts.StartTLS()
	defer ts.Close()

	resp, err := ts.Client().Post(ts.URL+"/api", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	var out proto.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if out.OK {
		t.Fatal("非法 JSON 应被拒绝")
	}
	if out.Code != proto.ErrInvalid {
		t.Fatalf("错误码为 %q，期望 invalid", out.Code)
	}
}

// TestLimiter 验证下载限流（需求 20.4）。
func TestLimiter(t *testing.T) {
	l := newRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	if l.allow("1.2.3.4") {
		t.Fatal("超过上限应被拒绝")
	}
	if !l.allow("5.6.7.8") {
		t.Fatal("不同 IP 不应互相影响")
	}
}

// TestHumanSize 验证大小格式化。
func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		512:              "512 B",
		2048:             "2.0 KB",
		5 * 1024 * 1024:  "5.0 MB",
	}
	for in, want := range cases {
		if got := humanSize(in); got != want {
			t.Fatalf("humanSize(%d) = %q，期望 %q", in, got, want)
		}
	}
}

// TestTrimTrailing 验证路径尾部斜杠处理。
func TestTrimTrailing(t *testing.T) {
	if got := trimTrailing("/a/b/"); got != "/a/b" {
		t.Fatalf("trimTrailing = %q", got)
	}
	if got := trimTrailing("/a/b"); got != "/a/b" {
		t.Fatalf("trimTrailing = %q", got)
	}
}
