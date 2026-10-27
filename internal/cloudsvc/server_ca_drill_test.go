package cloudsvc

import (
	"crypto/tls"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/cloud/identity"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 服务端 CA 丢失与恢复的跨重启演练（ADR 0052）。
//
// 只保留磁盘状态、每一步换一个进程，走真实协议与真实 TLS 握手：
// 这才是"服务器连 certs/ 一起丢了"时真正会发生的事。

// initDevice 完成初始化，返回第一台设备的 TLS 证书（用 initReq 那把设备私钥）。
func initDevice(t *testing.T, dir string, srv *Server) *tls.Certificate {
	t.Helper()
	privPEM, pubPEM, err := identity.GenerateDeviceKey()
	if err != nil {
		t.Fatalf("生成设备密钥: %v", err)
	}
	in := initReq(t)
	in.DevicePublicKey = string(pubPEM)
	req, err := proto.WithPayload(proto.MethodInitialize, proto.SourceEnroll, in)
	if err != nil {
		t.Fatalf("构造初始化请求: %v", err)
	}
	req.ID = "ca-init"
	resp := drillHarness(t, dir, srv).post(req)
	if !resp.OK {
		t.Fatalf("初始化失败: %s %s", resp.Code, resp.Error)
	}
	var out proto.EnrollBeginResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("解析初始化结果: %v", err)
	}
	cert, err := tls.X509KeyPair([]byte(out.Certificate), privPEM)
	if err != nil {
		t.Fatalf("组装设备证书: %v", err)
	}
	return &cert
}

func syncReq(t *testing.T) *proto.Request {
	t.Helper()
	r := &proto.Request{ID: "ca-sync", Protocol: proto.Version, Method: proto.MethodSync}
	if err := r.EncodePayload(&proto.SyncRequest{}); err != nil {
		t.Fatalf("构造 sync: %v", err)
	}
	return r
}

func recoverRequest(t *testing.T, in *proto.RecoverRequest) *proto.Request {
	t.Helper()
	in.Email, in.Password = "me@example.com", "pw"
	r, err := proto.WithPayload(proto.MethodRecover, proto.SourceEnroll, in)
	if err != nil {
		t.Fatalf("构造恢复请求: %v", err)
	}
	r.ID = "ca-recover"
	return r
}

// healthOf 读取健康检查里的 CA 状态。
func healthOf(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.handleHealth(rec, httptest.NewRequest("GET", "/healthz", nil))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析健康检查: %v", err)
	}
	return out
}

// TestServerCALostRestoredFromBitwarden 演练二（预期路径）：CA 丢了，从 Bitwarden 取回，设备无需重新登记。
func TestServerCALostRestoredFromBitwarden(t *testing.T) {
	dir := t.TempDir()
	vault := newDrillVault()

	first := startInstance(t, dir, vault)
	cert := initDevice(t, dir, first)
	if h := healthOf(t, first); h["ca_state"] != "ready" || h["ca_backup_pending"] != false {
		t.Fatalf("初始化后 CA 应就绪且已备份: %v", h)
	}
	first.closeAll()

	// 服务器重建：certs/ 整个没了。
	if err := os.RemoveAll(filepath.Join(dir, "certs")); err != nil {
		t.Fatalf("删除 certs: %v", err)
	}
	second := startInstance(t, dir, vault)
	defer second.closeAll()
	h := drillHarness(t, dir, second)

	if hl := healthOf(t, second); hl["ca_state"] != string(identity.CANeedsRestore) {
		t.Fatalf("健康检查应报告 CA 待恢复: %v", hl)
	}
	resp := h.postTLS(syncReq(t), cert)
	if resp.OK || resp.Code != proto.ErrCAPendingRestore {
		t.Fatalf("CA 待恢复时 sync 应返回 %s，实际 %v %s", proto.ErrCAPendingRestore, resp.OK, resp.Code)
	}
	// hello 必须放行，代理才能知道发生了什么。
	hello, _ := proto.WithPayload(proto.MethodHello, proto.SourceAgent, &proto.HelloRequest{AgentVersion: "1.0.0"})
	hello.ID = "ca-hello"
	if r := h.post(hello); !r.OK {
		t.Fatalf("CA 待恢复时 hello 应放行: %s", r.Error)
	}

	r := h.post(recoverRequest(t, &proto.RecoverRequest{RestoreOnly: true}))
	if !r.OK {
		t.Fatalf("恢复失败: %s %s", r.Code, r.Error)
	}
	var out proto.RecoverResponse
	_ = json.Unmarshal(r.Payload, &out)
	if !out.CARestored || out.CARebuilt || out.EnrollBeginResponse != nil {
		t.Fatalf("应只取回 CA: %+v", out)
	}
	if resp := h.postTLS(syncReq(t), cert); !resp.OK {
		t.Fatalf("取回原 CA 后旧设备证书应能直接用: %s %s", resp.Code, resp.Error)
	}
	second.closeAll()

	// 再重启一次：直接就绪。
	third := startInstance(t, dir, vault)
	defer third.closeAll()
	if hl := healthOf(t, third); hl["ca_state"] != "ready" {
		t.Fatalf("恢复后重启应直接就绪: %v", hl)
	}
	if resp := drillHarness(t, dir, third).postTLS(syncReq(t), cert); !resp.OK {
		t.Fatalf("重启后旧证书仍应可用: %s", resp.Error)
	}
}

// TestServerCALostDeclaredAndDeviceReenrolls 演练一（最坏情况）：Bitwarden 里没有 CA，宣告丢失后重建，设备沿用原 ID 重新登记。
func TestServerCALostDeclaredAndDeviceReenrolls(t *testing.T) {
	dir := t.TempDir()
	vault := newDrillVault()

	first := startInstance(t, dir, vault)
	privPEM, pubPEM, err := identity.GenerateDeviceKey()
	if err != nil {
		t.Fatalf("生成设备密钥: %v", err)
	}
	in := initReq(t)
	in.DevicePublicKey = string(pubPEM)
	ireq, _ := proto.WithPayload(proto.MethodInitialize, proto.SourceEnroll, in)
	ireq.ID = "ca-init"
	ir := drillHarness(t, dir, first).post(ireq)
	if !ir.OK {
		t.Fatalf("初始化失败: %s", ir.Error)
	}
	var enrolled proto.EnrollBeginResponse
	_ = json.Unmarshal(ir.Payload, &enrolled)
	if _, err := first.reg.Update(enrolled.DeviceID, ssot.Author{Name: "t", Source: "admin"}, "订阅",
		func(d *ssot.Device) { d.UserBundles = []string{"woa"} }); err != nil {
		t.Fatalf("设置订阅: %v", err)
	}
	oldCert, _ := tls.X509KeyPair([]byte(enrolled.Certificate), privPEM)
	first.closeAll()

	// 模拟"从未进过 Bitwarden"的存量 CA：把冷备里的 CA 条目抹掉。
	vault.mu.Lock()
	for name := range vault.notes[ssot.DirRootKeyBackup] {
		if strings.HasPrefix(name, ssot.FolderServerCA+"/") {
			delete(vault.notes[ssot.DirRootKeyBackup], name)
		}
	}
	vault.mu.Unlock()
	if err := os.Remove(filepath.Join(dir, "certs", identity.CAKeyFile)); err != nil {
		t.Fatalf("删除 ca.key: %v", err)
	}

	second := startInstance(t, dir, vault)
	defer second.closeAll()
	h := drillHarness(t, dir, second)

	// 不带确认：拒绝，并告诉用户找不到哪一条。
	r := h.post(recoverRequest(t, &proto.RecoverRequest{RestoreOnly: true}))
	if r.OK || r.Code != proto.ErrCANotInBackup {
		t.Fatalf("应返回 %s，实际 %v %s", proto.ErrCANotInBackup, r.OK, r.Code)
	}
	if !strings.Contains(r.Error, "server-ca/") {
		t.Fatalf("错误应写明条目名: %s", r.Error)
	}

	// 带确认：重建。
	r = h.post(recoverRequest(t, &proto.RecoverRequest{RestoreOnly: true, DeclareCALost: true}))
	if !r.OK {
		t.Fatalf("宣告丢失后恢复失败: %s %s", r.Code, r.Error)
	}
	var out proto.RecoverResponse
	_ = json.Unmarshal(r.Payload, &out)
	if !out.CARebuilt || out.ReenrollDevices != 1 {
		t.Fatalf("应重建并标记 1 台设备: %+v", out)
	}

	// 旧证书：明确告知需要重新登记，而不是笼统的"未登记"。
	resp := h.postTLS(syncReq(t), &oldCert)
	if resp.OK || resp.Code != proto.ErrDeviceNeedsReenroll {
		t.Fatalf("旧证书应返回 %s，实际 %v %s %s", proto.ErrDeviceNeedsReenroll, resp.OK, resp.Code, resp.Error)
	}

	// 用原设备公钥重新登记：沿用原 ID，订阅保留。
	payload := initReq(t)
	payload.DevicePublicKey = string(pubPEM)
	ereq, _ := proto.WithPayload(proto.MethodEnroll, proto.SourceEnroll, payload)
	ereq.ID = "ca-reenroll"
	er := h.post(ereq)
	if !er.OK {
		t.Fatalf("重新登记失败: %s %s", er.Code, er.Error)
	}
	var again proto.EnrollBeginResponse
	_ = json.Unmarshal(er.Payload, &again)
	if again.DeviceID != enrolled.DeviceID {
		t.Fatalf("应沿用原设备 ID %s，实际 %s", enrolled.DeviceID, again.DeviceID)
	}
	newCert, _ := tls.X509KeyPair([]byte(again.Certificate), privPEM)
	if resp := h.postTLS(syncReq(t), &newCert); !resp.OK {
		t.Fatalf("新证书应可用: %s %s", resp.Code, resp.Error)
	}
	dev, _ := second.reg.Device(enrolled.DeviceID)
	if dev.NeedsReenroll || len(dev.UserBundles) != 1 || dev.UserBundles[0] != "woa" {
		t.Fatalf("重新登记后订阅应保留、标记应清除: %+v", dev)
	}
	second.closeAll()

	// 演练二紧接着：再删一次 ca.key，这次应能从 Bitwarden 取回重建后的那份，设备无需重新登记。
	if err := os.Remove(filepath.Join(dir, "certs", identity.CAKeyFile)); err != nil {
		t.Fatalf("再次删除 ca.key: %v", err)
	}
	third := startInstance(t, dir, vault)
	defer third.closeAll()
	h3 := drillHarness(t, dir, third)
	r = h3.post(recoverRequest(t, &proto.RecoverRequest{RestoreOnly: true}))
	if !r.OK {
		t.Fatalf("第二次恢复失败: %s %s", r.Code, r.Error)
	}
	if resp := h3.postTLS(syncReq(t), &newCert); !resp.OK {
		t.Fatalf("从 Bitwarden 取回后设备应直接可用: %s %s", resp.Code, resp.Error)
	}
}
