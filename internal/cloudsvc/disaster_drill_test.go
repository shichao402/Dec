package cloudsvc

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/shichao402/Dec/internal/cloud/identity"
	"github.com/shichao402/Dec/internal/proto"
)

// 灾难恢复演练：在同一个数据目录上反复「重启进程」，验证需求 9.4 与需求 11。
//
// 为什么必须跨重启：现有单元测试都在同一个 Bootstrapper 实例上跑，
// 根密钥一直留在内存里，测不出「重启后靠磁盘上的东西能不能活」。
// 而真实灾难（服务器重建、根密钥文件被误删）恰恰只保留磁盘状态。
//
// 演练不碰生产环境，也不需要用户的 Bitwarden 主密码：
// 冷备用内存替身，但冷备之外的一切（数据目录、根密钥文件、SSOT 仓库、
// CA 证书、设备表）都是真实文件，链路是真的。

// drillVault 是演练用的 Bitwarden 替身。
//
// 与 initialize_test.go 的 fakeVaultForInit 的差别只有 ListNotes：
// 恢复流程要列出 _dec-cloud 下的全部根密钥条目，没有这个方法直接报「不支持列出条目」，
// 演练会假失败。
type drillVault struct {
	mu    sync.Mutex
	notes map[string]map[string]string
}

func newDrillVault() *drillVault {
	return &drillVault{notes: map[string]map[string]string{}}
}

func (v *drillVault) Unlock(_ context.Context, _, _, _ string) error { return nil }

func (v *drillVault) UpsertNote(_ context.Context, folder, name, content string) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.notes[folder] == nil {
		v.notes[folder] = map[string]string{}
	}
	old, existed := v.notes[folder][name]
	v.notes[folder][name] = content
	return !existed || old != content, nil
}

func (v *drillVault) GetNote(_ context.Context, folder, name string) (string, bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	content, ok := v.notes[folder][name]
	return content, ok, nil
}

func (v *drillVault) ListNotes(_ context.Context, folder string) (map[string]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := map[string]string{}
	for name, content := range v.notes[folder] {
		out[name] = content
	}
	return out, nil
}

func (v *drillVault) Close() {}

// startInstance 在同一个数据目录上启动一次服务端，模拟一次进程启动。
//
// 不复用 newHarnessAt 是因为它固定不注入 VaultFactory，
// 而恢复流程必须有可用的冷备。
func startInstance(t *testing.T, dir string, vault *drillVault) *Server {
	t.Helper()
	srv, err := New(Config{
		Domain:          "dec.test",
		Listen:          "127.0.0.1:0",
		DataDir:         dir,
		ServerVersion:   "1.0.0",
		RequiredVersion: "1.0.0",
		AcceptAny:       true,
		VaultFactory:    func() (identity.Vault, error) { return vault, nil },
	})
	if err != nil {
		t.Fatalf("启动服务端失败: %v", err)
	}
	return srv
}

// drillHarness 把一次启动包成可发请求的 harness。
func drillHarness(t *testing.T, dir string, srv *Server) *harness {
	t.Helper()
	return &harness{t: t, dir: dir, srv: srv, handler: srv.Handler()}
}

// TestDisasterDrillAcrossRestart 是主演练：初始化 → 重启 → 丢根密钥 → 恢复 → 再重启。
//
// 每一步都只保留磁盘状态，模拟真实灾难里「进程已经换了一个」的情形。
func TestDisasterDrillAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	vault := newDrillVault()
	keyPath := filepath.Join(dir, "root.key")

	// ---- 阶段一：全新服务完成初始化 ----
	first := startInstance(t, dir, vault)
	if !first.Bootstrapper().State().BootstrapOpen {
		t.Fatal("全新服务应打开初始化入口")
	}
	initRequest, err := proto.WithPayload(proto.MethodInitialize, proto.SourceEnroll, initReq(t))
	if err != nil {
		t.Fatalf("构造初始化请求失败: %v", err)
	}
	initRequest.ID = "drill-init"
	if resp := drillHarness(t, dir, first).post(initRequest); !resp.OK {
		t.Fatalf("初始化失败: %s %s", resp.Code, resp.Error)
	}
	firstKeyID := first.Bootstrapper().State().KeyID
	first.closeAll()

	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("初始化后应留下根密钥文件: %v", err)
	}

	// ---- 阶段二：正常重启，根密钥应自动加载 ----
	second := startInstance(t, dir, vault)
	state := second.Bootstrapper().State()
	if state.RecoveryMode {
		t.Fatal("根密钥完好时不应进入恢复模式")
	}
	if state.KeyID != firstKeyID {
		t.Fatalf("重启后根密钥应仍是 %s，实际 %q", firstKeyID, state.KeyID)
	}
	second.closeAll()

	// ---- 阶段三：删掉根密钥文件，模拟文件丢失 ----
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("删除根密钥失败: %v", err)
	}
	third := startInstance(t, dir, vault)
	defer third.closeAll()

	if !third.Bootstrapper().State().RecoveryMode {
		t.Fatal("根密钥文件丢失后必须进入恢复模式（需求 9.4）")
	}
	if third.Bootstrapper().State().BootstrapOpen {
		t.Fatal("已有设备登记时不应打开初始化入口")
	}

	// ---- 阶段四：恢复模式下，除恢复外的业务接口必须被拒绝（需求 9.4）----
	//
	// 这一条是恢复模式的核心价值：根密钥丢了意味着所有密文都打不开，
	// 此时还能正常 sync 会让代理把「解密失败」当成真实内容下发。
	cert := drillHarness(t, dir, third).registerDevice("dev-drill", "演练设备")
	syncRequest := &proto.Request{ID: "drill-sync", Protocol: proto.Version, Method: proto.MethodSync}
	if err := syncRequest.EncodePayload(&proto.SyncRequest{}); err != nil {
		t.Fatalf("构造 sync 载荷失败: %v", err)
	}
	if resp := drillHarness(t, dir, third).postTLS(syncRequest, cert); resp.OK {
		t.Error("恢复模式下 sync 必须被拒绝，实际成功（需求 9.4）")
	}

	// ---- 阶段五：走恢复接口取回根密钥 ----
	recoverRequest, err := proto.WithPayload(proto.MethodRecover, proto.SourceEnroll, &proto.RecoverRequest{
		Email:           "me@example.com",
		Password:        "pw",
		DevicePublicKey: initReq(t).DevicePublicKey,
		DeviceName:      "恢复用设备",
		OS:              "darwin",
		Arch:            "arm64",
	})
	if err != nil {
		t.Fatalf("构造恢复请求失败: %v", err)
	}
	recoverRequest.ID = "drill-recover"
	if resp := drillHarness(t, dir, third).post(recoverRequest); !resp.OK {
		t.Fatalf("恢复失败: %s %s", resp.Code, resp.Error)
	}
	if third.Bootstrapper().State().RecoveryMode {
		t.Fatal("恢复成功后应退出恢复模式")
	}
	if third.Bootstrapper().State().KeyID != firstKeyID {
		t.Fatalf("恢复应拿回原根密钥 %s，实际 %q", firstKeyID, third.Bootstrapper().State().KeyID)
	}
	third.closeAll()

	// ---- 阶段六：恢复后再重启一次，确认根密钥真的落盘了 ----
	fourth := startInstance(t, dir, vault)
	defer fourth.closeAll()
	if fourth.Bootstrapper().State().RecoveryMode {
		t.Fatal("恢复后重启不应再进入恢复模式")
	}
	if fourth.Bootstrapper().State().KeyID != firstKeyID {
		t.Fatalf("恢复后重启的根密钥应为 %s，实际 %q", firstKeyID, fourth.Bootstrapper().State().KeyID)
	}
}

// TestDrillVaultSeesRootKeyBackup 确认演练前提成立：初始化确实往冷备写了根密钥。
//
// 如果这一步不成立，后面的恢复演练会因为「冷备是空的」而失败，
// 但那是演练脚手架的问题，不是产品的问题，必须能一眼分开。
func TestDrillVaultSeesRootKeyBackup(t *testing.T) {
	dir := t.TempDir()
	vault := newDrillVault()

	srv := startInstance(t, dir, vault)
	defer srv.closeAll()

	initRequest, err := proto.WithPayload(proto.MethodInitialize, proto.SourceEnroll, initReq(t))
	if err != nil {
		t.Fatalf("构造初始化请求失败: %v", err)
	}
	initRequest.ID = "drill-init"
	if resp := drillHarness(t, dir, srv).post(initRequest); !resp.OK {
		t.Fatalf("初始化失败: %s %s", resp.Code, resp.Error)
	}

	notes, err := vault.ListNotes(context.Background(), "_dec-cloud")
	if err != nil {
		t.Fatalf("列出冷备条目失败: %v", err)
	}
	if len(notes) == 0 {
		t.Fatal("初始化后冷备里必须有根密钥条目")
	}
}
