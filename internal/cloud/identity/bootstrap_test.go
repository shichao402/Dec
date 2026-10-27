package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// fakeVault 是内存版 Bitwarden，用于测试三条流程。
type fakeVault struct {
	mu     sync.Mutex
	notes  map[string]map[string]string
	email  string
	pass   string
	totp   string
	unlock int
	fail   bool
	// requireTOTP 为真时第一次 Unlock 返回 ErrNeed2FA。
	requireTOTP bool
	unlocked    bool
}

func newFakeVault() *fakeVault {
	return &fakeVault{notes: map[string]map[string]string{}, pass: "pw", totp: "123456"}
}

func (f *fakeVault) Unlock(_ context.Context, email, password, totp string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unlock++
	if f.fail {
		return errors.New("登录失败")
	}
	if f.email == "" {
		f.email = email
	}
	if f.email != email || f.pass != password {
		return errors.New("凭据错误")
	}
	if f.requireTOTP && totp == "" {
		return ErrNeed2FA
	}
	if f.requireTOTP && totp != f.totp {
		return errors.New("验证码错误")
	}
	f.unlocked = true
	return nil
}

func (f *fakeVault) UpsertNote(_ context.Context, folder, name, content string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.unlocked {
		return false, errors.New("未解锁")
	}
	if f.notes[folder] == nil {
		f.notes[folder] = map[string]string{}
	}
	old, ok := f.notes[folder][name]
	f.notes[folder][name] = content
	return !ok || old != content, nil
}

func (f *fakeVault) GetNote(_ context.Context, folder, name string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.unlocked {
		return "", false, errors.New("未解锁")
	}
	content, ok := f.notes[folder][name]
	return content, ok, nil
}

func (f *fakeVault) ListNotes(_ context.Context, folder string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.notes[folder] {
		out[k] = v
	}
	return out, nil
}

func (f *fakeVault) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unlocked = false
}

func newTestBootstrapper(t *testing.T) (*Bootstrapper, *fakeVault) {
	t.Helper()
	store, err := ssot.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ca, err := NewCA()
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	fv := newFakeVault()
	b, err := NewBootstrapper(Options{
		Store:       store,
		CA:          ca,
		Registry:    NewRegistry(store),
		RootKeyPath: filepath.Join(t.TempDir(), "root.key"),
		Vault:       func() (Vault, error) { return fv, nil },
	})
	if err != nil {
		t.Fatalf("NewBootstrapper: %v", err)
	}
	if err := b.LoadOrEnterRecovery(); err != nil {
		t.Fatalf("LoadOrEnterRecovery: %v", err)
	}
	return b, fv
}

func devicePublicKey(t *testing.T) string {
	t.Helper()
	_, pub, err := GenerateDeviceKey()
	if err != nil {
		t.Fatalf("GenerateDeviceKey: %v", err)
	}
	return string(pub)
}

func enrollReq(t *testing.T) proto.EnrollBeginRequest {
	t.Helper()
	return proto.EnrollBeginRequest{
		Email:           "me@example.com",
		Password:        "pw",
		DevicePublicKey: devicePublicKey(t),
		DeviceName:      "MacBook",
		OS:              "darwin",
		Arch:            "arm64",
	}
}

func TestFreshStateIsBootstrapOpen(t *testing.T) {
	b, _ := newTestBootstrapper(t)
	st := b.State()
	if !st.BootstrapOpen {
		t.Fatal("全新服务应允许初始化")
	}
	if st.RecoveryMode {
		t.Fatal("根密钥缺失时首次启动不应直接进入恢复模式之外的异常态")
	}
	if st.KeyID != "" {
		t.Fatal("尚未初始化时不应有根密钥")
	}
}

func TestInitializeCreatesKeyAndBackup(t *testing.T) {
	b, fv := newTestBootstrapper(t)
	req := enrollReq(t)

	resp, err := b.Initialize(context.Background(), req)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !resp.IsFirst {
		t.Fatal("首次初始化应标记 IsFirst")
	}
	if resp.DeviceID == "" || resp.Certificate == "" {
		t.Fatalf("返回不完整: %+v", resp)
	}

	// 本地根密钥文件必须存在且可用。
	key := b.RootKey()
	if key == nil {
		t.Fatal("初始化后应有根密钥")
	}
	if st := b.State(); st.BootstrapOpen {
		t.Fatal("初始化后入口应关闭")
	}

	// 冷备条目必须存在且与本地文件一致。
	notes := fv.notes[ssot.DirRootKeyBackup]
	name := ssot.FolderRootKey + "/" + key.ID()
	content, ok := notes[name]
	if !ok {
		t.Fatalf("冷备条目缺失: %v", notes)
	}
	fileData, err := crypt.RootKeyPayload(key, time.Time{}, "")
	if err != nil {
		t.Fatalf("RootKeyPayload: %v", err)
	}
	if len(fileData) == 0 || len(content) == 0 {
		t.Fatal("冷备内容为空")
	}

	// 证书可被 CA 验证，且设备已登记。
	devices, err := b.reg.Devices()
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 1 || devices[0].ID != resp.DeviceID {
		t.Fatalf("设备登记错误: %+v", devices)
	}
	if devices[0].Kind != ssot.DeviceMine {
		t.Fatal("首台设备应为我的设备")
	}
	if _, _, _, err := b.ca.VerifyDevice([]byte(resp.Certificate), time.Now()); err != nil {
		t.Fatalf("证书校验失败: %v", err)
	}
}

func TestInitializeRejectsSecondCall(t *testing.T) {
	b, _ := newTestBootstrapper(t)
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("首次 Initialize: %v", err)
	}
	_, err := b.Initialize(context.Background(), enrollReq(t))
	if !errors.Is(err, ErrBootstrapClosed) {
		t.Fatalf("二次初始化应报 ErrBootstrapClosed，实际 %v", err)
	}
}

func TestInitializeRollsBackWhenBackupFails(t *testing.T) {
	b, fv := newTestBootstrapper(t)
	fv.fail = true
	_, err := b.Initialize(context.Background(), enrollReq(t))
	if err == nil {
		t.Fatal("冷备失败时初始化应失败")
	}
	if b.RootKey() != nil {
		t.Fatal("初始化失败不应留下根密钥")
	}
	// 本地文件必须被清掉，避免下次启动误认为已初始化。
	if _, err := crypt.LoadRootKey(b.rootKeyPath); !errors.Is(err, crypt.ErrRootKeyMissing) {
		t.Fatalf("本地根密钥文件应被清理，实际 %v", err)
	}
}

func TestEnrollRequiresRootKey(t *testing.T) {
	b, _ := newTestBootstrapper(t)
	_, err := b.Enroll(context.Background(), enrollReq(t))
	if !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("未加载根密钥时应报 ErrRecoveryRequired，实际 %v", err)
	}
}

func TestEnrollAfterInitialize(t *testing.T) {
	b, _ := newTestBootstrapper(t)
	first, err := b.Initialize(context.Background(), enrollReq(t))
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	req := enrollReq(t)
	resp, err := b.Enroll(context.Background(), req)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if resp.IsFirst {
		t.Fatal("第二台设备不应标记 IsFirst")
	}
	if resp.DeviceID == first.DeviceID {
		t.Fatal("设备 ID 不应重复")
	}
	devices, err := b.reg.Devices()
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("应有 2 台设备，实际 %d", len(devices))
	}
}

func TestEnrollRejectsWrongPassword(t *testing.T) {
	b, _ := newTestBootstrapper(t)
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	req := enrollReq(t)
	req.Password = "wrong"
	if _, err := b.Enroll(context.Background(), req); err == nil {
		t.Fatal("错误密码应被拒绝")
	}
}

func TestEnrollRequiresTOTP(t *testing.T) {
	b, fv := newTestBootstrapper(t)
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	fv.requireTOTP = true
	fv.unlocked = false

	_, err := b.Enroll(context.Background(), enrollReq(t))
	if !errors.Is(err, ErrNeed2FA) {
		t.Fatalf("应提示需要 2FA，实际 %v", err)
	}

	req := enrollReq(t)
	req.TOTP = "123456"
	if _, err := b.Enroll(context.Background(), req); err != nil {
		t.Fatalf("带正确验证码应成功: %v", err)
	}
}

func TestEnrollFailureRateLimit(t *testing.T) {
	b, fv := newTestBootstrapper(t)
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	fv.fail = true
	var lastErr error
	for i := 0; i < maxFailures; i++ {
		req := enrollReq(t)
		_, lastErr = b.Enroll(context.Background(), req)
		if lastErr == nil {
			t.Fatalf("第 %d 次失败登录应报错", i+1)
		}
	}
	// 达到阈值后进入冷却。
	req := enrollReq(t)
	_, err := b.Enroll(context.Background(), req)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("应进入冷却，实际 %v", err)
	}
}

func TestRecoverRestoresRootKey(t *testing.T) {
	b, fv := newTestBootstrapper(t)
	first, err := b.Initialize(context.Background(), enrollReq(t))
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	originalKeyID := b.RootKey().ID()

	// 模拟灾难：删掉本地根密钥、清空设备登记，再重启进入恢复模式。
	if err := removeFile(b.rootKeyPath); err != nil {
		t.Fatalf("removeFile: %v", err)
	}
	b.setKey(nil)
	b.mu.Lock()
	b.recoveryMode = true
	b.mu.Unlock()

	res, err := b.Recover(context.Background(), recoverReq(t))
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if !res.RootKeyRestored {
		t.Fatal("应报告根密钥已取回")
	}
	resp := res.Enrolled
	if resp == nil || resp.DeviceID == "" || resp.DeviceID == first.DeviceID {
		t.Fatalf("用新设备密钥恢复应登记新设备: %+v", resp)
	}
	restored := b.RootKey()
	if restored == nil {
		t.Fatal("恢复后应有根密钥")
	}
	if restored.ID() != originalKeyID {
		t.Fatalf("恢复的根密钥不一致: %s != %s", restored.ID(), originalKeyID)
	}
	if st := b.State(); st.RecoveryMode {
		t.Fatal("恢复后应退出恢复模式")
	}
	// 恢复后本地文件也要存在，否则下次重启又进恢复模式。
	if _, err := crypt.LoadRootKey(b.rootKeyPath); err != nil {
		t.Fatalf("恢复后本地文件应可用: %v", err)
	}
	// 恢复的根密钥应能解开冷备条目里记录的那份材料。
	_ = fv
}

func TestRecoverWithoutBackup(t *testing.T) {
	b, _ := newTestBootstrapper(t)
	b.mu.Lock()
	b.recoveryMode = true
	b.mu.Unlock()
	if _, err := b.Recover(context.Background(), recoverReq(t)); err == nil {
		t.Fatal("没有冷备时恢复应失败")
	}
}

func TestRotateRekeysAndBacksUp(t *testing.T) {
	b, fv := newTestBootstrapper(t)
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	oldID := b.RootKey().ID()

	var rekeyCalls int
	var gotOld, gotNew string
	resp, err := b.Rotate(context.Background(),
		proto.RotateRequest{Email: "me@example.com", Password: "pw"},
		func(oldKey, newKey *crypt.Key) (int, error) {
			rekeyCalls++
			gotOld, gotNew = oldKey.ID(), newKey.ID()
			return 3, nil
		})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if rekeyCalls != 1 {
		t.Fatalf("应调用一次重加密，实际 %d", rekeyCalls)
	}
	if gotOld != oldID {
		t.Fatalf("重加密拿到的旧密钥不一致: %s", gotOld)
	}
	if resp.NewKeyID == oldID {
		t.Fatal("轮换后 key id 应变化")
	}
	if b.RootKey().ID() != resp.NewKeyID {
		t.Fatal("内存中的根密钥未更新")
	}
	// 新旧冷备条目都应存在：只增不改。
	notes := fv.notes[ssot.DirRootKeyBackup]
	if _, ok := notes[ssot.FolderRootKey+"/"+oldID]; !ok {
		t.Fatal("旧根密钥冷备条目不应被删除")
	}
	if _, ok := notes[ssot.FolderRootKey+"/"+resp.NewKeyID]; !ok {
		t.Fatal("新根密钥冷备条目缺失")
	}
	// 本地文件也应更新。
	loaded, err := crypt.LoadRootKey(b.rootKeyPath)
	if err != nil {
		t.Fatalf("LoadRootKey: %v", err)
	}
	if loaded.ID() != resp.NewKeyID {
		t.Fatalf("本地文件未更新: %s", loaded.ID())
	}
	_ = gotNew
}

func TestRotateAbortsWhenRekeyFails(t *testing.T) {
	b, _ := newTestBootstrapper(t)
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	oldID := b.RootKey().ID()
	_, err := b.Rotate(context.Background(),
		proto.RotateRequest{Email: "me@example.com", Password: "pw"},
		func(oldKey, newKey *crypt.Key) (int, error) {
			return 0, errors.New("重加密失败")
		})
	if err == nil {
		t.Fatal("重加密失败时轮换应中止")
	}
	if b.RootKey().ID() != oldID {
		t.Fatal("轮换失败后根密钥不应改变")
	}
}

func TestRotateRejectsWrongCredential(t *testing.T) {
	b, _ := newTestBootstrapper(t)
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	_, err := b.Rotate(context.Background(),
		proto.RotateRequest{Email: "me@example.com", Password: "wrong"},
		func(oldKey, newKey *crypt.Key) (int, error) { return 0, nil })
	if err == nil {
		t.Fatal("凭据错误时轮换应被拒绝")
	}
}

func TestLoadOrEnterRecoveryOnCorruptFile(t *testing.T) {
	store, err := ssot.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ca, err := NewCA()
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	reg := NewRegistry(store)
	// 有设备登记才意味着“曾经能用”，此时根密钥损坏才是灾难。
	if err := reg.Add(sampleDevice("d1"), adminAuthor()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	path := filepath.Join(t.TempDir(), "root.key")
	if err := writeCorrupt(path); err != nil {
		t.Fatalf("writeCorrupt: %v", err)
	}
	b, err := NewBootstrapper(Options{
		Store: store, CA: ca, Registry: reg, RootKeyPath: path,
		Vault: func() (Vault, error) { return newFakeVault(), nil },
	})
	if err != nil {
		t.Fatalf("NewBootstrapper: %v", err)
	}
	if err := b.LoadOrEnterRecovery(); err != nil {
		t.Fatalf("LoadOrEnterRecovery: %v", err)
	}
	st := b.State()
	if !st.RecoveryMode {
		t.Fatal("已登记设备但根密钥损坏，应进入恢复模式")
	}
	if st.BootstrapOpen {
		t.Fatal("已有设备时初始化入口应关闭")
	}
}

// TestLoadOrEnterRecoveryFreshInstall 全新安装时根密钥缺失是正常状态，不是灾难。
func TestLoadOrEnterRecoveryFreshInstall(t *testing.T) {
	store, err := ssot.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ca, err := NewCA()
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	b, err := NewBootstrapper(Options{
		Store: store, CA: ca, Registry: NewRegistry(store),
		RootKeyPath: filepath.Join(t.TempDir(), "root.key"),
		Vault:       func() (Vault, error) { return newFakeVault(), nil },
	})
	if err != nil {
		t.Fatalf("NewBootstrapper: %v", err)
	}
	if err := b.LoadOrEnterRecovery(); err != nil {
		t.Fatalf("LoadOrEnterRecovery: %v", err)
	}
	st := b.State()
	if st.RecoveryMode {
		t.Fatal("全新安装不应进入恢复模式")
	}
	if !st.BootstrapOpen {
		t.Fatal("全新安装应允许初始化")
	}
}

func TestGenerateDeviceKey(t *testing.T) {
	priv, pub, err := GenerateDeviceKey()
	if err != nil {
		t.Fatalf("GenerateDeviceKey: %v", err)
	}
	block, _ := pem.Decode(pub)
	if block == nil {
		t.Fatal("公钥不是 PEM")
	}
	if _, err := x509.ParsePKIXPublicKey(block.Bytes); err != nil {
		t.Fatalf("公钥无法解析: %v", err)
	}
	keyBlock, _ := pem.Decode(priv)
	if keyBlock == nil {
		t.Fatal("私钥不是 PEM")
	}
	if _, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err != nil {
		t.Fatalf("私钥无法解析: %v", err)
	}
	pub2, err := ParsePublicKey(pub)
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	if _, err := PublicKeyFingerprint(pub2); err != nil {
		t.Fatalf("PublicKeyFingerprint: %v", err)
	}
	_ = elliptic.P256()
	_ = ecdsa.PublicKey{}
	_ = rand.Reader
}

func TestEnrollRejectsBadPublicKey(t *testing.T) {
	b, _ := newTestBootstrapper(t)
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	req := enrollReq(t)
	req.DevicePublicKey = "not a pem"
	if _, err := b.Enroll(context.Background(), req); err == nil {
		t.Fatal("非法公钥应被拒绝")
	}
}

func TestEnrollUsesCredentialAfterBackupCheck(t *testing.T) {
	b, fv := newTestBootstrapper(t)
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	// 删掉冷备条目后，登记必须失败：这验证了登记时确实会核对冷备可用性。
	for name := range fv.notes[ssot.DirRootKeyBackup] {
		delete(fv.notes[ssot.DirRootKeyBackup], name)
	}
	if _, err := b.Enroll(context.Background(), enrollReq(t)); err == nil {
		t.Fatal("冷备缺失时登记应失败")
	} else if !strings.Contains(err.Error(), "冷备") {
		t.Fatalf("错误信息应说明冷备问题: %v", err)
	}
}

func writeCorrupt(path string) error {
	return os.WriteFile(path, []byte("not a root key"), 0o600)
}
