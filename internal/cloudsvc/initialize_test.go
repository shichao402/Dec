package cloudsvc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/cloud/identity"
	"github.com/shichao402/Dec/internal/proto"
)

// 初始化链路（需求 10.1、10.2、10.3）。
//
// 这些用例存在的理由：全新服务上 enroll 与 recover 都不可用，
// 只有 initialize 能把服务从「没有根密钥」带到「可以签发设备证书」。
// 一旦分发器漏掉这个方法，线上表现是部署完成后完全没有入口可用。

// fakeVaultForInit 是一个只记账的 Bitwarden 替身。
type fakeVaultForInit struct {
	notes map[string]string
	fail  bool
}

func (f *fakeVaultForInit) Unlock(ctx context.Context, email, password, totp string) error {
	if f.fail {
		return errFakeVault
	}
	return nil
}

func (f *fakeVaultForInit) UpsertNote(ctx context.Context, folder, name, content string) (bool, error) {
	if f.fail {
		return false, errFakeVault
	}
	key := folder + "/" + name
	_, existed := f.notes[key]
	f.notes[key] = content
	return existed, nil
}

func (f *fakeVaultForInit) GetNote(ctx context.Context, folder, name string) (string, bool, error) {
	if f.fail {
		return "", false, errFakeVault
	}
	v, ok := f.notes[folder+"/"+name]
	return v, ok, nil
}

func (f *fakeVaultForInit) Close() {}

var errFakeVault = errors.New("fake vault 不可用")

// newInitHarness 在全新数据目录上启动服务端，并注入可用的 Bitwarden 替身。
func newInitHarness(t *testing.T) (*harness, *fakeVaultForInit) {
	t.Helper()
	dir := t.TempDir()
	fv := &fakeVaultForInit{notes: map[string]string{}}
	srv, err := New(Config{
		Domain:          "dec.test",
		Listen:          "127.0.0.1:0",
		DataDir:         dir,
		ServerVersion:   "1.0.0",
		RequiredVersion: "1.0.0",
		AcceptAny:       true,
		VaultFactory:    func() (identity.Vault, error) { return fv, nil },
	})
	if err != nil {
		t.Fatalf("构造服务端失败: %v", err)
	}
	t.Cleanup(srv.closeAll)
	return &harness{t: t, dir: dir, srv: srv, handler: srv.Handler()}, fv
}

// initReq 构造一次初始化请求，自带新生成的设备公钥。
func initReq(t *testing.T) *proto.EnrollBeginRequest {
	t.Helper()
	_, pub, err := identity.GenerateDeviceKey()
	if err != nil {
		t.Fatalf("生成设备密钥失败: %v", err)
	}
	return &proto.EnrollBeginRequest{
		Email:           "me@example.com",
		Password:        "pw",
		DevicePublicKey: string(pub),
		DeviceName:      "MacBook",
		OS:              "darwin",
		Arch:            "arm64",
	}
}

// TestInitializeCreatesRootKeyAndFirstDevice 是主链路的正面用例。
func TestInitializeCreatesRootKeyAndFirstDevice(t *testing.T) {
	h, fv := newInitHarness(t)

	// 全新服务：初始化入口必须是开着的。
	if !h.srv.Bootstrapper().State().BootstrapOpen {
		t.Fatal("全新服务应打开初始化入口")
	}

	req, err := proto.WithPayload(proto.MethodInitialize, proto.SourceEnroll, initReq(t))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.ID = "init-1"

	resp := h.post(req)
	if !resp.OK {
		t.Fatalf("初始化失败: %s %s", resp.Code, resp.Error)
	}
	var out proto.EnrollBeginResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if out.DeviceID == "" || out.Certificate == "" {
		t.Fatalf("初始化结果不完整: %+v", out)
	}
	if !out.IsFirst {
		t.Fatal("初始化生成的设备应标记为第一台")
	}

	// 根密钥必须落到磁盘，且只有本用户可读（需求 9.1）。
	keyPath := filepath.Join(h.dir, "root.key")
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("根密钥未写入: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("根密钥权限应为 0600，实际 %o", perm)
	}

	// 冷备必须有内容且能读回（需求 10.5）。
	found := 0
	for k := range fv.notes {
		if strings.Contains(k, "_dec-cloud") {
			found++
		}
	}
	if found == 0 {
		t.Fatalf("冷备未写入 Bitwarden，现有条目: %v", fv.notes)
	}

	// 入口必须永久关闭（需求 10.3）。
	if h.srv.Bootstrapper().State().BootstrapOpen {
		t.Fatal("初始化完成后入口应永久关闭")
	}
}

// TestInitializeClosesAfterFirstDevice 验证入口不会二次打开。
func TestInitializeClosesAfterFirstDevice(t *testing.T) {
	h, _ := newInitHarness(t)

	first, _ := proto.WithPayload(proto.MethodInitialize, proto.SourceEnroll, initReq(t))
	first.ID = "init-1"
	if resp := h.post(first); !resp.OK {
		t.Fatalf("首次初始化失败: %s", resp.Error)
	}

	second, _ := proto.WithPayload(proto.MethodInitialize, proto.SourceEnroll, initReq(t))
	second.ID = "init-2"
	resp := h.post(second)
	if resp.OK {
		t.Fatal("已完成初始化后，再次初始化必须被拒绝")
	}
	if resp.Code != proto.ErrBootstrapClosed {
		t.Fatalf("错误码应为 %s，实际 %s（%s）", proto.ErrBootstrapClosed, resp.Code, resp.Error)
	}
}

// TestEnrollFailsBeforeInitialize 固定「全新服务不能直接登记」这条前置条件。
//
// 这是最容易踩的坑：如果没有 initialize，部署完成后照文档执行 enroll，
// 会得到一句「根密钥未加载」，而文档里没有任何办法继续。
func TestEnrollFailsBeforeInitialize(t *testing.T) {
	h, _ := newInitHarness(t)

	req, _ := proto.WithPayload(proto.MethodEnroll, proto.SourceEnroll, initReq(t))
	req.ID = "enroll-1"
	resp := h.post(req)
	if resp.OK {
		t.Fatal("全新服务上登记必须失败")
	}
	if resp.Code != proto.ErrNotInitialized {
		t.Fatalf("错误码应为 %s，实际 %s（%s）", proto.ErrNotInitialized, resp.Code, resp.Error)
	}
}

// TestInitializeIsNoCertMethod 确认初始化不需要设备证书。
//
// 这一步发生在任何身份存在之前，要求证书等于永远无法完成初始化。
func TestInitializeIsNoCertMethod(t *testing.T) {
	if !proto.NoCertMethods[proto.MethodInitialize] {
		t.Fatal("initialize 必须在免证书清单里")
	}
	if !proto.VersionMethods[proto.MethodInitialize] {
		t.Fatal("initialize 必须在永不破坏兼容的方法清单里")
	}
}

// TestInitializeFailureLeavesServiceUsable 验证冷备写失败时不留下「有密钥但没备份」。
func TestInitializeFailureLeavesServiceUsable(t *testing.T) {
	h, fv := newInitHarness(t)
	fv.fail = true

	req, _ := proto.WithPayload(proto.MethodInitialize, proto.SourceEnroll, initReq(t))
	req.ID = "init-fail"
	if resp := h.post(req); resp.OK {
		t.Fatal("冷备不可用时初始化必须失败")
	}

	// 关键：本地根密钥文件必须被清掉，否则服务会以「已初始化但没备份」启动。
	if _, err := os.Stat(filepath.Join(h.dir, "root.key")); !os.IsNotExist(err) {
		t.Fatalf("初始化失败后不应留下根密钥文件，stat err=%v", err)
	}
	// 入口仍应可用，允许用户修正后重试。
	if !h.srv.Bootstrapper().State().BootstrapOpen {
		t.Fatal("初始化失败后入口应保持打开以便重试")
	}
}
