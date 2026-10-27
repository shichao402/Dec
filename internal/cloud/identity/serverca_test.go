package identity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// ADR 0052：服务端 CA 只在本机与 Bitwarden，缺失时不得静默重建。

func recoverReq(t *testing.T) proto.RecoverRequest {
	t.Helper()
	return proto.RecoverRequest{
		Email:           "me@example.com",
		Password:        "pw",
		DevicePublicKey: devicePublicKey(t),
		DeviceName:      "MacBook",
		OS:              "darwin",
		Arch:            "arm64",
	}
}

// caEnv 是一个"按生产方式装配"的环境：Bootstrapper 用 CADir 自己判定 CA。
type caEnv struct {
	t       *testing.T
	store   *ssot.Store
	caDir   string
	keyPath string
	vault   *fakeVault
}

func newCAEnv(t *testing.T) *caEnv {
	t.Helper()
	store, err := ssot.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return &caEnv{
		t:       t,
		store:   store,
		caDir:   filepath.Join(t.TempDir(), "certs"),
		keyPath: filepath.Join(t.TempDir(), "root.key"),
		vault:   newFakeVault(),
	}
}

// boot 模拟一次进程启动。
func (e *caEnv) boot() *Bootstrapper {
	e.t.Helper()
	b, err := NewBootstrapper(Options{
		Store:       e.store,
		CADir:       e.caDir,
		Registry:    NewRegistry(e.store),
		RootKeyPath: e.keyPath,
		Vault:       func() (Vault, error) { return e.vault, nil },
	})
	if err != nil {
		e.t.Fatalf("NewBootstrapper: %v", err)
	}
	if err := b.LoadOrEnterRecovery(); err != nil {
		e.t.Fatalf("LoadOrEnterRecovery: %v", err)
	}
	return b
}

func (e *caEnv) removeCAKey() {
	e.t.Helper()
	if err := os.Remove(filepath.Join(e.caDir, CAKeyFile)); err != nil {
		e.t.Fatalf("删除 ca.key: %v", err)
	}
}

func (e *caEnv) caNotes() []string {
	var out []string
	for name := range e.vault.notes[ssot.DirRootKeyBackup] {
		if strings.HasPrefix(name, ssot.FolderServerCA+"/") {
			out = append(out, name)
		}
	}
	return out
}

// TestFreshInstallInitializeBacksUpCA 全新部署：初始化在同一个会话里写入 CA 冷备。
func TestFreshInstallInitializeBacksUpCA(t *testing.T) {
	e := newCAEnv(t)
	b := e.boot()
	st := b.CAStatus()
	if st.State != CAReady || !st.BackupPending {
		t.Fatalf("全新部署应生成 CA 且标记待备份: %+v", st)
	}
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if b.CAStatus().BackupPending {
		t.Fatal("初始化后 CA 应已写入 Bitwarden")
	}
	notes := e.caNotes()
	if len(notes) != 1 || notes[0] != CABackupName(st.Fingerprint) {
		t.Fatalf("Bitwarden 里应恰有一条按指纹命名的 CA 条目: %v", notes)
	}
	rec, ok, err := ReadCARecord(e.store)
	if err != nil || !ok || rec.BackupState != ssot.CABackupBitwarden || rec.BackedUpAt == nil {
		t.Fatalf("git 记录应标记已备份: %+v ok=%v err=%v", rec, ok, err)
	}
}

// TestCARecordHasNoPrivateKey git 里的 CA 记录只能有证书，绝不能有私钥（ADR 0052）。
func TestCARecordHasNoPrivateKey(t *testing.T) {
	e := newCAEnv(t)
	b := e.boot()
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	data, err := e.store.ReadFile(ssot.FileServerCA)
	if err != nil {
		t.Fatalf("读取 CA 记录: %v", err)
	}
	if strings.Contains(string(data), "PRIVATE KEY") {
		t.Fatal("git 里的 CA 记录出现了私钥")
	}
	files, err := e.store.WalkFiles(".")
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	for _, f := range files {
		raw, err := e.store.ReadFile(f)
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), "BEGIN PRIVATE KEY") {
			t.Fatalf("仓库文件 %s 含有私钥", f)
		}
	}
}

// TestLegacyCAGetsRecordedAsPending 存量 CA：本机有、git 没有，补记录并标记待备份，业务照常。
func TestLegacyCAGetsRecordedAsPending(t *testing.T) {
	e := newCAEnv(t)
	legacy, err := NewCA()
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	if err := WriteLocalCA(e.caDir, legacy); err != nil {
		t.Fatalf("WriteLocalCA: %v", err)
	}
	b := e.boot()
	st := b.CAStatus()
	if st.State != CAReady || !st.BackupPending {
		t.Fatalf("存量 CA 应可用且标记待备份: %+v", st)
	}
	if st.Fingerprint != CAFingerprint(legacy) {
		t.Fatal("不能替换存量 CA")
	}
	rec, ok, _ := ReadCARecord(e.store)
	if !ok || rec.BackupState != ssot.CABackupPending {
		t.Fatalf("应补写 pending 记录: %+v", rec)
	}
}

// TestMissingCAEntersRestoreNotRegenerate CA 私钥丢了，绝不能静默生成新的（需求 2.1）。
func TestMissingCAEntersRestoreNotRegenerate(t *testing.T) {
	e := newCAEnv(t)
	b := e.boot()
	fp := b.CAStatus().Fingerprint
	e.removeCAKey()

	b2 := e.boot()
	st := b2.CAStatus()
	if st.State != CANeedsRestore {
		t.Fatalf("应进入待恢复状态，实际 %+v", st)
	}
	if b2.CurrentCA() != nil {
		t.Fatal("待恢复时不应有可用 CA")
	}
	if _, err := os.Stat(filepath.Join(e.caDir, CAKeyFile)); !os.IsNotExist(err) {
		t.Fatal("不应自动生成新的 ca.key")
	}
	rec, _, _ := ReadCARecord(e.store)
	if rec.Fingerprint != fp {
		t.Fatal("git 记录不应被改写")
	}
}

// TestMismatchedCAIsRejected 本机 CA 与 git 记录不一致，两边都不动。
func TestMismatchedCAIsRejected(t *testing.T) {
	e := newCAEnv(t)
	b := e.boot()
	fp := b.CAStatus().Fingerprint
	other, _ := NewCA()
	if err := WriteLocalCA(e.caDir, other); err != nil {
		t.Fatalf("WriteLocalCA: %v", err)
	}
	b2 := e.boot()
	st := b2.CAStatus()
	if st.State != CAMismatch {
		t.Fatalf("应判为不一致，实际 %+v", st)
	}
	if !strings.Contains(st.Detail, "不一致") {
		t.Fatalf("原因应说明不一致: %s", st.Detail)
	}
	rec, _, _ := ReadCARecord(e.store)
	if rec.Fingerprint != fp {
		t.Fatal("不一致时不得覆盖 git 记录")
	}
	if _, err := b2.Recover(context.Background(), recoverReq(t)); !errors.Is(err, ErrCAUnavailable) {
		t.Fatalf("不一致时恢复应拒绝，实际 %v", err)
	}
}

// TestRecoverCAFromBitwardenKeepsDevices 从 Bitwarden 取回原 CA，旧设备证书继续有效（需求 3.4）。
func TestRecoverCAFromBitwardenKeepsDevices(t *testing.T) {
	e := newCAEnv(t)
	b := e.boot()
	first, err := b.Initialize(context.Background(), enrollReq(t))
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	fp := b.CAStatus().Fingerprint
	e.removeCAKey()

	b2 := e.boot()
	res, err := b2.Recover(context.Background(), proto.RecoverRequest{
		Email: "me@example.com", Password: "pw", RestoreOnly: true,
	})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if !res.CARestored || res.CARebuilt || res.Enrolled != nil || res.RootKeyRestored {
		t.Fatalf("只应取回 CA，不登记、不动根密钥: %+v", res)
	}
	ca := b2.CurrentCA()
	if ca == nil || CAFingerprint(ca) != fp {
		t.Fatal("取回的应是同一份 CA")
	}
	if _, _, _, err := ca.VerifyDevice([]byte(first.Certificate), time.Now()); err != nil {
		t.Fatalf("原设备证书应继续有效: %v", err)
	}
	// 重启后直接就绪，不再要求恢复。
	if st := e.boot().CAStatus(); st.State != CAReady || st.BackupPending {
		t.Fatalf("恢复后重启应直接就绪: %+v", st)
	}
	if info, err := os.Stat(filepath.Join(e.caDir, CAKeyFile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("恢复写回的 ca.key 应为 0600: %v %v", info, err)
	}
}

// TestRecoverPicksCAByFingerprint Bitwarden 里有别的 CA 条目时，不能误选（需求 3.1）。
func TestRecoverPicksCAByFingerprint(t *testing.T) {
	e := newCAEnv(t)
	b := e.boot()
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	fp := b.CAStatus().Fingerprint
	decoy, _ := NewCA()
	payload, _ := CABackupPayload(decoy)
	e.vault.notes[ssot.DirRootKeyBackup][CABackupName(CAFingerprint(decoy))] = payload
	e.removeCAKey()

	b2 := e.boot()
	if _, err := b2.Recover(context.Background(), proto.RecoverRequest{
		Email: "me@example.com", Password: "pw", RestoreOnly: true,
	}); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if CAFingerprint(b2.CurrentCA()) != fp {
		t.Fatal("应按指纹取回原 CA，而不是别的条目")
	}
}

// TestRecoverCANotInBackupNeedsExplicitDeclare 找不到条目时不得自作主张重建（需求 3.3）。
func TestRecoverCANotInBackupNeedsExplicitDeclare(t *testing.T) {
	e := newCAEnv(t)
	legacy, _ := NewCA()
	if err := WriteLocalCA(e.caDir, legacy); err != nil {
		t.Fatalf("WriteLocalCA: %v", err)
	}
	b := e.boot()
	// 存量 CA 没进过 Bitwarden；先模拟初始化，但不让 CA 进冷备：
	// 直接用 backupRootKey 写根密钥，复现"生产上 CA 从未备份"的局面。
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	for name := range e.vault.notes[ssot.DirRootKeyBackup] {
		if strings.HasPrefix(name, ssot.FolderServerCA+"/") {
			delete(e.vault.notes[ssot.DirRootKeyBackup], name)
		}
	}
	e.removeCAKey()

	b2 := e.boot()
	_, err := b2.Recover(context.Background(), proto.RecoverRequest{
		Email: "me@example.com", Password: "pw", RestoreOnly: true,
	})
	if !errors.Is(err, ErrCANotInBackup) {
		t.Fatalf("应返回找不到条目，实际 %v", err)
	}
	if !strings.Contains(err.Error(), CABackupName(CAFingerprint(legacy))) {
		t.Fatalf("错误应写明要找的条目: %v", err)
	}
	if b2.CAStatus().State != CANeedsRestore {
		t.Fatal("失败后应保持待恢复")
	}
	if _, statErr := os.Stat(filepath.Join(e.caDir, CAKeyFile)); !os.IsNotExist(statErr) {
		t.Fatal("未确认时不得生成 ca.key")
	}
}

// TestDeclareCALostRebuildsAndKeepsDevice 宣告丢失后重建；同一台设备用原密钥重新登记，沿用原 ID 与订阅。
func TestDeclareCALostRebuildsAndKeepsDevice(t *testing.T) {
	e := newCAEnv(t)
	b := e.boot()
	privPEM, pubPEM, err := GenerateDeviceKey()
	if err != nil {
		t.Fatalf("GenerateDeviceKey: %v", err)
	}
	_ = privPEM
	req := enrollReq(t)
	req.DevicePublicKey = string(pubPEM)
	first, err := b.Initialize(context.Background(), req)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := b.reg.Update(first.DeviceID, adminAuthor(), "订阅", func(d *ssot.Device) {
		d.UserBundles = []string{"woa", "cnb"}
	}); err != nil {
		t.Fatalf("设置订阅: %v", err)
	}
	oldFP := b.CAStatus().Fingerprint
	// Bitwarden 里的 CA 条目也没了：只能宣告丢失。
	for name := range e.vault.notes[ssot.DirRootKeyBackup] {
		if strings.HasPrefix(name, ssot.FolderServerCA+"/") {
			delete(e.vault.notes[ssot.DirRootKeyBackup], name)
		}
	}
	e.removeCAKey()

	b2 := e.boot()
	res, err := b2.Recover(context.Background(), proto.RecoverRequest{
		Email: "me@example.com", Password: "pw", RestoreOnly: true, DeclareCALost: true,
	})
	if err != nil {
		t.Fatalf("Recover(declare): %v", err)
	}
	if !res.CARebuilt || res.ReenrollDevices != 1 {
		t.Fatalf("应重建 CA 并标记 1 台设备: %+v", res)
	}
	newFP := b2.CAStatus().Fingerprint
	if newFP == oldFP {
		t.Fatal("重建后指纹应变化")
	}
	rec, _, _ := ReadCARecord(e.store)
	if rec.ReplacedFingerprint != oldFP || rec.BackupState != ssot.CABackupBitwarden {
		t.Fatalf("git 记录应写明被替换的旧 CA 且已备份: %+v", rec)
	}
	if len(e.caNotes()) != 1 || e.caNotes()[0] != CABackupName(newFP) {
		t.Fatalf("新 CA 应已写入 Bitwarden: %v", e.caNotes())
	}
	if _, _, _, err := b2.CurrentCA().VerifyDevice([]byte(first.Certificate), time.Now()); err == nil {
		t.Fatal("旧证书在新 CA 下不应通过")
	}
	dev, _ := b2.reg.Device(first.DeviceID)
	if !dev.NeedsReenroll {
		t.Fatal("旧设备应标记为需要重新登记")
	}

	// 同一把设备公钥重新登记：沿用原 ID，订阅保留。
	again := enrollReq(t)
	again.DevicePublicKey = string(pubPEM)
	resp, err := b2.Enroll(context.Background(), again)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if resp.DeviceID != first.DeviceID {
		t.Fatalf("应沿用原设备 ID %s，实际 %s", first.DeviceID, resp.DeviceID)
	}
	if _, _, _, err := b2.CurrentCA().VerifyDevice([]byte(resp.Certificate), time.Now()); err != nil {
		t.Fatalf("新证书应由新 CA 签发: %v", err)
	}
	dev, _ = b2.reg.Device(first.DeviceID)
	if dev.NeedsReenroll {
		t.Fatal("重新登记后应清除标记")
	}
	if strings.Join(dev.UserBundles, ",") != "woa,cnb" {
		t.Fatalf("订阅应原样保留: %v", dev.UserBundles)
	}
	devices, _ := b2.reg.Devices()
	if len(devices) != 1 {
		t.Fatalf("不应多出重复设备: %d", len(devices))
	}
}

// TestDeclareIgnoredWhenCAInBackup Bitwarden 里能找到时，宣告丢失无效，一定用找到的那份。
func TestDeclareIgnoredWhenCAInBackup(t *testing.T) {
	e := newCAEnv(t)
	b := e.boot()
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	fp := b.CAStatus().Fingerprint
	e.removeCAKey()
	b2 := e.boot()
	res, err := b2.Recover(context.Background(), proto.RecoverRequest{
		Email: "me@example.com", Password: "pw", RestoreOnly: true, DeclareCALost: true,
	})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if res.CARebuilt || !res.CARestored || CAFingerprint(b2.CurrentCA()) != fp {
		t.Fatalf("能找到原 CA 时不得重建: %+v", res)
	}
}

// TestRootKeyAndCALostTogetherOneLogin 根密钥与 CA 同时丢：一次登录全部取回（需求 2.5）。
func TestRootKeyAndCALostTogetherOneLogin(t *testing.T) {
	e := newCAEnv(t)
	b := e.boot()
	first, err := b.Initialize(context.Background(), enrollReq(t))
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	keyID := b.RootKey().ID()
	e.removeCAKey()
	if err := os.Remove(e.keyPath); err != nil {
		t.Fatalf("删除根密钥: %v", err)
	}
	b2 := e.boot()
	if !b2.State().RecoveryMode || b2.CAStatus().State != CANeedsRestore {
		t.Fatal("应同时处于根密钥恢复模式与 CA 待恢复")
	}
	before := e.vault.unlock
	res, err := b2.Recover(context.Background(), proto.RecoverRequest{
		Email: "me@example.com", Password: "pw", RestoreOnly: true,
	})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if e.vault.unlock-before != 1 {
		t.Fatalf("应只登录一次，实际 %d 次", e.vault.unlock-before)
	}
	if !res.RootKeyRestored || !res.CARestored {
		t.Fatalf("两样都应取回: %+v", res)
	}
	if b2.RootKey().ID() != keyID || b2.State().RecoveryMode {
		t.Fatal("根密钥应恢复并退出恢复模式")
	}
	if _, _, _, err := b2.CurrentCA().VerifyDevice([]byte(first.Certificate), time.Now()); err != nil {
		t.Fatalf("原设备证书应继续有效: %v", err)
	}
}

// TestEnrollBacksUpPendingCA 存量 CA 待备份时，正常登记流程顺手写入 Bitwarden，不需要单独操作。
func TestEnrollBacksUpPendingCA(t *testing.T) {
	e := newCAEnv(t)
	b := e.boot()
	if _, err := b.Initialize(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	// 回到"存量、未备份"的局面：删掉 Bitwarden 里的 CA 条目，git 标回 pending。
	for name := range e.vault.notes[ssot.DirRootKeyBackup] {
		if strings.HasPrefix(name, ssot.FolderServerCA+"/") {
			delete(e.vault.notes[ssot.DirRootKeyBackup], name)
		}
	}
	rec, _, _ := ReadCARecord(e.store)
	rec.BackupState = ssot.CABackupPending
	rec.BackedUpAt = nil
	if err := WriteCARecord(e.store, rec, "回退为待备份", adminAuthor()); err != nil {
		t.Fatalf("WriteCARecord: %v", err)
	}
	b2 := e.boot()
	if !b2.CAStatus().BackupPending {
		t.Fatal("应为待备份")
	}
	if _, err := b2.Enroll(context.Background(), enrollReq(t)); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if b2.CAStatus().BackupPending || len(e.caNotes()) != 1 {
		t.Fatalf("登记后 CA 应已写入 Bitwarden: %v", e.caNotes())
	}
}

// TestParseCABackupRejectsMismatchedPair 冷备条目里私钥与证书不配对时必须拒绝。
func TestParseCABackupRejectsMismatchedPair(t *testing.T) {
	a, _ := NewCA()
	c, _ := NewCA()
	keyPEM, _ := a.KeyPEM()
	if _, err := ParseCABackupPayload(string(keyPEM) + string(c.CertPEM())); err == nil {
		t.Fatal("私钥与证书不配对应拒绝")
	}
}

// TestLocalCAMissingCertIsCorrupt 只剩私钥、没有证书，视为损坏而不是全新。
func TestLocalCAMissingCertIsCorrupt(t *testing.T) {
	e := newCAEnv(t)
	e.boot()
	if err := os.Remove(filepath.Join(e.caDir, CACertFile)); err != nil {
		t.Fatalf("删除证书: %v", err)
	}
	st := e.boot().CAStatus()
	if st.State != CAMismatch {
		t.Fatalf("缺证书应判为不可用，实际 %+v", st)
	}
}
