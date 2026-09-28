package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/secrets"
)

// stubCredentialAsker 测试用凭据请求 stub。
type stubCredentialAsker struct {
	loginUser     string
	loginPassword string
	loginErr      error
	passphrase    string
	passphraseErr error
	loginCalls    int
	passCalls     int
}

func (s *stubCredentialAsker) AskLoginPassword(ctx context.Context, prompt, operation string) (string, string, error) {
	s.loginCalls++
	if s.loginErr != nil {
		return "", "", s.loginErr
	}
	return s.loginUser, s.loginPassword, nil
}

func (s *stubCredentialAsker) AskKeyPassphrase(ctx context.Context, prompt, operation string) (string, error) {
	s.passCalls++
	if s.passphraseErr != nil {
		return "", s.passphraseErr
	}
	return s.passphrase, nil
}

func withStubAsker(t *testing.T, asker CredentialAsker) {
	t.Helper()
	old := credentialAsker
	credentialAsker = asker
	t.Cleanup(func() { credentialAsker = old })
}

func TestParseDevicePasswordNote(t *testing.T) {
	u, p, err := parseDevicePasswordNote("username: alice\npassword: hunter2\n")
	if err != nil {
		t.Fatal(err)
	}
	if u != "alice" || p != "hunter2" {
		t.Fatalf("u=%q p=%q", u, p)
	}
	if _, _, err := parseDevicePasswordNote("username: alice\n"); err == nil {
		t.Fatal("缺 password 行应报错")
	}
	if _, _, err := parseDevicePasswordNote("# 任意\npassword: x\n"); err == nil {
		t.Fatal("缺 username 行应报错")
	}
}

func TestWriteDevicePasswordNoteRoundTrip(t *testing.T) {
	stub := &secrets.StubClient{}
	target, err := secrets.NewPSyncTarget("woa", secrets.SyncPlaneGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDevicePasswordNote(context.Background(), stub, target, "dev-box", "alice", "hunter2"); err != nil {
		t.Fatal(err)
	}
	note, err := stub.GetNote(context.Background(), target, ".password/dev-box")
	if err != nil {
		t.Fatal(err)
	}
	u, p, err := parseDevicePasswordNote(note.Content)
	if err != nil {
		t.Fatal(err)
	}
	if u != "alice" || p != "hunter2" {
		t.Fatalf("round trip u=%q p=%q", u, p)
	}
}

func TestGenerateSSHKeyRegistersAndLands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	stub := &secrets.StubClient{}
	withStubClient(t, stub)
	withTestSession(t)

	result, err := GenerateSSHKey(context.Background(), GenerateSSHKeyInput{
		Project: "woa", Name: "dev-box", Comment: "dev-box",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.KeyName != ".sshkey/dev-box" {
		t.Fatalf("key_name = %q", result.KeyName)
	}
	if result.PublicKey == "" || result.Fingerprint == "" {
		t.Fatal("公钥与指纹不应为空")
	}
	if strings.Contains(fmt.Sprintf("%v", result), "BEGIN") {
		t.Fatal("结果不得含私钥材料")
	}
	// BW 侧条目存在
	if len(stub.SSHKeysByFolder["woa/private/global"]) != 1 {
		t.Fatalf("BW folder 应有 1 条 key: %v", stub.SSHKeysByFolder)
	}
	// 本机 ~/.ssh 落地
	sshDir, err := secrets.SSHDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sshDir, "dec_woa_dev-box")); err != nil {
		t.Fatalf("私钥未落地: %v", err)
	}
}

func TestGenerateSSHKeyRequiresProject(t *testing.T) {
	if _, err := GenerateSSHKey(context.Background(), GenerateSSHKeyInput{Name: "x"}, nil); err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("缺 project 应报错: %v", err)
	}
}

func TestImportSSHKeyFromPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	stub := &secrets.StubClient{}
	withStubClient(t, stub)
	withTestSession(t)

	priv := testGeneratePrivateKeyFile(t)
	result, err := ImportSSHKey(context.Background(), ImportSSHKeyInput{
		Project: "woa", Name: "cloud-1", PrivateKeyPath: priv,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.KeyName != ".sshkey/cloud-1" {
		t.Fatalf("key_name = %q", result.KeyName)
	}
	if len(stub.SSHKeysByFolder["woa/private/global"]) != 1 {
		t.Fatal("BW 侧应已登记")
	}
}

func TestImportSSHKeyRejectsMissingPath(t *testing.T) {
	if _, err := ImportSSHKey(context.Background(), ImportSSHKeyInput{Project: "woa", Name: "x"}, nil); err == nil {
		t.Fatal("缺 private_key_path 应报错")
	}
}

func TestInstallSSHKeyFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	stub := &secrets.StubClient{}
	withStubClient(t, stub)
	withTestSession(t)
	asker := &stubCredentialAsker{loginUser: "alice", loginPassword: "hunter2"}
	withStubAsker(t, asker)

	// ssh 不真连：桩掉 PasswordSSHAppendAuthorizedKey
	installed := ""
	oldAppend := passwordSSHAppend
	passwordSSHAppend = func(ctx context.Context, target, user, pass, line string) error {
		installed = line
		return nil
	}
	t.Cleanup(func() { passwordSSHAppend = oldAppend })

	result, err := InstallSSHKey(context.Background(), InstallSSHKeyInput{
		Project: "woa", Alias: "dev-box", SSHTarget: "dev-box.internal", Confirmed: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Marker != "# dec:dev-box" {
		t.Fatalf("marker = %q", result.Marker)
	}
	if !strings.Contains(installed, "# dec:dev-box") {
		t.Fatalf("追加行缺标记: %q", installed)
	}
	if result.PasswordStored != true {
		t.Fatal("密码应已回写")
	}
	// 密码 note 已入库且可解析
	note, err := stub.GetNote(context.Background(), mustTarget(t, "woa"), ".password/dev-box")
	if err != nil {
		t.Fatal(err)
	}
	if u, p, _ := parseDevicePasswordNote(note.Content); u != "alice" || p != "hunter2" {
		t.Fatalf("note = %q/%q", u, p)
	}
	if asker.loginCalls != 1 {
		t.Fatalf("loginCalls = %d", asker.loginCalls)
	}
}

func TestInstallSSHKeyAutoFillsStoredPassword(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	stub := &secrets.StubClient{}
	withStubClient(t, stub)
	withTestSession(t)
	asker := &stubCredentialAsker{loginPassword: "should-not-be-asked"}
	withStubAsker(t, asker)

	// 预存密码条目
	target := mustTarget(t, "woa")
	if err := writeDevicePasswordNote(context.Background(), stub, target, "dev-box", "bob", "stored-pw"); err != nil {
		t.Fatal(err)
	}
	oldAppend := passwordSSHAppend
	passwordSSHAppend = func(ctx context.Context, target, user, pass, line string) error {
		if user != "bob" || pass != "stored-pw" {
			return fmt.Errorf("应自动填充存量密码: %q/%q", user, pass)
		}
		return nil
	}
	t.Cleanup(func() { passwordSSHAppend = oldAppend })

	result, err := InstallSSHKey(context.Background(), InstallSSHKeyInput{
		Project: "woa", Alias: "dev-box", SSHTarget: "dev-box.internal", Confirmed: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Username != "bob" {
		t.Fatalf("username = %q", result.Username)
	}
	if asker.loginCalls != 0 {
		t.Fatal("存量密码存在时不应询问用户")
	}
}

func TestInstallSSHKeyRequiresConfirm(t *testing.T) {
	if _, err := InstallSSHKey(context.Background(), InstallSSHKeyInput{
		Project: "woa", Alias: "x", SSHTarget: "h", Confirmed: false,
	}, nil); err == nil || !strings.Contains(err.Error(), "confirmed") {
		t.Fatalf("缺 confirmed 应报错: %v", err)
	}
}

func TestInstallSSHKeyHeadlessRejected(t *testing.T) {
	withStubAsker(t, nil)
	stub := &secrets.StubClient{}
	withStubClient(t, stub)
	withTestSession(t)
	_, err := InstallSSHKey(context.Background(), InstallSSHKeyInput{
		Project: "woa", Alias: "x", SSHTarget: "h", Confirmed: true,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "headless") {
		t.Fatalf("headless 应结构化拒绝: %v", err)
	}
}

func TestLoadSSHKeyMaterialInteractivePlainKey(t *testing.T) {
	priv := testGeneratePrivateKeyFile(t)
	mat, err := loadSSHKeyMaterialInteractive(context.Background(), priv, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mat.PublicKey == "" {
		t.Fatal("公钥为空")
	}
}

// withStubClient 注入 StubClient 并注册恢复。
func withStubClient(t *testing.T, stub secrets.Client) {
	t.Helper()
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })
}

// withTestSession 注入已解锁会话（绕过 ensureBitwardenSession 的真实解锁）。
func withTestSession(t *testing.T) {
	t.Helper()
	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)
}

// runTestSSHKeygen 用系统 ssh-keygen 生成一把测试私钥。
func runTestSSHKeygen(path string) (string, error) {
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "dec-test", "-f", path)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func testGeneratePrivateKeyFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "id_ed25519")
	out, err := runTestSSHKeygen(path)
	if err != nil {
		t.Fatalf("ssh-keygen: %v (%s)", err, out)
	}
	return path
}

func mustTarget(t *testing.T, project string) secrets.SyncTarget {
	t.Helper()
	target, err := secrets.NewPSyncTarget(project, secrets.SyncPlaneGlobal)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
