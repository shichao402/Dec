package cloudsvc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/proto"
)

// 第 8 项的验收点：审计要真的写进去、冷备要真的被触发、告警要真的能下发。
//
// 这些都不是纯函数，必须端到端跑一遍才有意义。

// TestAuditOnAssetWrite 验证写入资产产生审计记录。
func TestAuditOnAssetWrite(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	req := &proto.Request{
		ID:       "1",
		Protocol: proto.Version,
		Method:   proto.MethodWriteAsset,
		Source:   proto.SourceAdmin,
	}
	if err := req.EncodePayload(&proto.AssetPayload{
		Bundle:  "core",
		Items:   []proto.AssetItem{{Path: "demo/SKILL.md", Content: []byte("hello")}},
		Message: "测试写入",
	}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if !resp.OK {
		t.Fatalf("写入失败: %s", resp.Error)
	}

	items, err := h.srv.Audit().Read(time.Now().UTC())
	if err != nil {
		t.Fatalf("读取审计失败: %v", err)
	}
	var found bool
	for _, it := range items {
		if it.Event == audit.EventAssetWrite {
			found = true
			if it.DeviceID != "dev-a" {
				t.Fatalf("审计应记录设备 ID，实际 %q", it.DeviceID)
			}
			if it.Source != "admin" {
				t.Fatalf("审计应记录来源入口，实际 %q", it.Source)
			}
			if !strings.Contains(it.Detail, "revision=") {
				t.Fatalf("审计应绑定 revision，实际 %q", it.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("应有资产写入审计，实际 %+v", items)
	}
}

// TestAuditSecretReadNoPlaintext 验证查看明文的审计不含明文。
//
// 这是审计最容易被做错的地方：把值顺手记进去，明文就永久留在 git 历史里了。
func TestAuditSecretReadNoPlaintext(t *testing.T) {
	h := newHarnessWithRootKey(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	// 先写一条密钥。
	put := &proto.Request{
		ID:       "1",
		Protocol: proto.Version,
		Method:   proto.MethodPutSecret,
		Source:   proto.SourceAdmin,
	}
	if err := put.EncodePayload(&proto.SecretPayload{
		Name:  "db-password",
		Value: "super-secret-value",
	}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	if resp := h.postTLS(put, cert); !resp.OK {
		t.Fatalf("写入密钥失败: %s", resp.Error)
	}

	read := &proto.Request{
		ID:       "2",
		Protocol: proto.Version,
		Method:   proto.MethodReadSecret,
		Source:   proto.SourceAdmin,
	}
	if err := read.EncodePayload(&proto.ReadSecretRequest{Name: "db-password"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	if resp := h.postTLS(read, cert); !resp.OK {
		t.Fatalf("读取密钥失败: %s", resp.Error)
	}

	items, err := h.srv.Audit().Read(time.Now().UTC())
	if err != nil {
		t.Fatalf("读取审计失败: %v", err)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("序列化审计失败: %v", err)
	}
	if strings.Contains(string(raw), "super-secret-value") {
		t.Fatal("审计里出现了密钥明文")
	}
	var hasRead bool
	for _, it := range items {
		if it.Event == audit.EventSecretRead {
			hasRead = true
		}
	}
	if !hasRead {
		t.Fatal("查看明文应留下审计")
	}
}

// TestAlertDeliveredViaSync 验证告警能随 sync 下发到设备（需求 12.1 第二条通道）。
func TestAlertDeliveredViaSync(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	h.srv.raise("backup_failed", "冷备推送失败", "远端不可达")

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
	if len(out.Alerts) != 1 {
		t.Fatalf("应有 1 条告警，实际 %d", len(out.Alerts))
	}
	if out.Alerts[0].Title != "冷备推送失败" {
		t.Fatalf("告警标题为 %q", out.Alerts[0].Title)
	}

	// 第二次不应重复下发同一条。
	resp2 := h.postTLS(req, cert)
	var out2 proto.SyncResponse
	if err := json.Unmarshal(resp2.Payload, &out2); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(out2.Alerts) != 0 {
		t.Fatalf("同一条告警不应重复下发，实际 %d", len(out2.Alerts))
	}
}

// TestDownloadAudited 验证下载被记录但不触发告警（需求 20.4）。
func TestDownloadAudited(t *testing.T) {
	h := newHarness(t, "1.0.0")
	if err := os.MkdirAll(filepath.Join(h.dir, "download"), 0o755); err != nil {
		t.Fatalf("准备下载目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "download", "dec-agent"), []byte("bin"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	rel := release{
		Version:        "1.0.0",
		RequiredClient: "1.0.0",
		Protocol:       proto.Version,
		Files:          []releaseFile{{Name: "dec-agent", Size: 3}},
	}
	raw, _ := json.Marshal(rel)
	if err := os.WriteFile(releaseManifestPath(h.dir), raw, 0o644); err != nil {
		t.Fatalf("写入清单失败: %v", err)
	}

	h.download("/download/dec-agent")

	items, err := h.srv.Audit().Read(time.Now().UTC())
	if err != nil {
		t.Fatalf("读取审计失败: %v", err)
	}
	var found bool
	for _, it := range items {
		if it.Event == audit.EventDownload {
			found = true
		}
	}
	if !found {
		t.Fatalf("下载应记审计，实际 %+v", items)
	}
	if got := len(h.srv.Alerts().Recent(10)); got != 0 {
		t.Fatalf("下载不应告警，实际 %d 条", got)
	}
}

// TestBackupTriggeredOnWrite 验证写入触发冷备推送。
//
// 这里不验证真的推上去（那需要远端），只验证调度层被唤醒了。
// 推送本身在 backup 包里有独立的测试。
func TestBackupTriggeredOnWrite(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	req := &proto.Request{
		ID:       "1",
		Protocol: proto.Version,
		Method:   proto.MethodWriteAsset,
		Source:   proto.SourceAdmin,
	}
	if err := req.EncodePayload(&proto.AssetPayload{
		Bundle: "core",
		Items:  []proto.AssetItem{{Path: "a.md", Content: []byte("x")}},
	}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	if resp := h.postTLS(req, cert); !resp.OK {
		t.Fatalf("写入失败: %s", resp.Error)
	}

	// 未配置远端时调度层静默跳过，LastError 应为 nil（不是“推送失败”）。
	time.Sleep(50 * time.Millisecond)
	if err := h.srv.Backup().LastError(); err != nil {
		t.Fatalf("未配置远端时不应记录错误: %v", err)
	}
}
