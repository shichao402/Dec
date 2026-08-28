package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
)

func setupRemoteRegisterRepo(t *testing.T, files map[string]string) {
	t.Helper()
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, files)
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
}

// declaredDemoP 是 vault 里声明了 demo 这个 P 的仓库内容。
func declaredDemoP() map[string]string {
	return map[string]string{"demo/dec.yaml": "name: demo\ntitle: Demo\n"}
}

func demoProjectScope() secrets.RemoteScope {
	return secrets.RemoteScope{P: "demo", Plane: secrets.SyncPlaneProject}
}

func TestPrepareAndCommitRemoteRegister_NotePath(t *testing.T) {
	setupRemoteRegisterRepo(t, declaredDemoP())
	secrets.SetSession("test-session")
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{}}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	projectRoot := t.TempDir()
	src := filepath.Join(projectRoot, "a.env")
	if err := os.WriteFile(src, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scope := demoProjectScope()
	sess, err := PrepareRemoteRegister(context.Background(), RemoteRegisterInput{
		ProjectRoot: projectRoot,
		Scope:       scope,
		TypeID:      secrets.SecretTypeEnv,
		Name:        "app",
		SourceMode:  secrets.SourcePath,
		LocalPath:   src,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sess.NeedsEditor || sess.NoteContent != "A=1\n" {
		t.Fatalf("sess = %#v", sess)
	}
	result, err := CommitRemoteRegister(context.Background(), *sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.NoteRelPath != ".env/app.env" || result.Address != scope.String() {
		t.Fatalf("result = %#v", result)
	}
	notes := stub.NotesByFolder[scope.String()]
	if len(notes) != 1 || notes[0].RelativePath != ".env/app.env" {
		t.Fatalf("notes = %#v", notes)
	}
}

func TestPrepareAndCommitRemoteRegister_SSHGenerate(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	home := t.TempDir()
	// os.UserHomeDir 在 Unix 读取 HOME、Windows 读取 USERPROFILE。
	// 使用统一 helper 同时隔离两者，避免测试把生成的私钥写进开发者真实 ~/.ssh。
	setEnvForProjectTest(t, "HOME", home)
	setupRemoteRegisterRepo(t, declaredDemoP())

	secrets.SetSession("test-session")
	stub := &secrets.StubClient{SSHKeysByFolder: map[string][]secrets.SSHKeyItem{}}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	scope := demoProjectScope()
	sess, err := PrepareRemoteRegister(context.Background(), RemoteRegisterInput{
		ProjectRoot: t.TempDir(),
		Scope:       scope,
		TypeID:      secrets.SecretTypeSSHKey,
		Name:        "deploy",
		SourceMode:  secrets.SourceGenerate,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sess.NeedsEditor || sess.SSHKey == nil {
		t.Fatalf("sess = %#v", sess)
	}
	if sess.SSHKey.Name != ".sshkey/deploy" {
		t.Fatalf("name = %q", sess.SSHKey.Name)
	}
	result, err := CommitRemoteRegister(context.Background(), *sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.NoteRelPath != ".sshkey/deploy" {
		t.Fatalf("result = %#v", result)
	}
	keys := stub.SSHKeysByFolder[scope.String()]
	if len(keys) != 1 || keys[0].Name != ".sshkey/deploy" {
		t.Fatalf("keys = %#v", keys)
	}
	if !strings.Contains(keys[0].PrivateKey, "PRIVATE KEY") {
		t.Fatal("private key should be stored")
	}
	// 项目平面私钥按工作区 scope 命名，避免与本机 key 撞名。
	privs, err := filepath.Glob(filepath.Join(home, ".ssh", "dec_project_*_demo_deploy"))
	if err != nil {
		t.Fatal(err)
	}
	if len(privs) != 1 {
		t.Fatalf("项目平面私钥应落地一份, got %v", privs)
	}
}

// 未在 vault 声明的 P 不能登记：Remote 页不是创建 P 的入口。
func TestCommitRemoteRegister_RejectsUndeclaredP(t *testing.T) {
	setupRemoteRegisterRepo(t, declaredDemoP())
	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)

	_, err := CommitRemoteRegister(t.Context(), RemoteRegisterSession{
		ProjectRoot: t.TempDir(),
		Scope:       secrets.RemoteScope{P: "relkit", Plane: secrets.SyncPlaneProject},
		TypeID:      secrets.SecretTypePlain,
		Name:        "token.txt",
		NoteContent: "secret",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "relkit") {
		t.Fatalf("未声明的 P 应被拒绝, got %v", err)
	}
}

// 项目平面的工作区不能往 private/user 写，反之亦然：平面串台会让 note 落错同步根。
func TestValidateRemoteRegisterScope_RejectsCrossPlane(t *testing.T) {
	setupRemoteRegisterRepo(t, declaredDemoP())

	projectWS := NewWorkspace(WorkspaceProject, t.TempDir())
	if err := ValidateRemoteRegisterScope(projectWS, secrets.RemoteScope{P: "demo", Plane: secrets.SyncPlaneMachine}); err == nil ||
		!strings.Contains(err.Error(), "user secrets") {
		t.Fatalf("项目平面不应允许 user 归属, got %v", err)
	}
	if err := ValidateRemoteRegisterScope(projectWS, demoProjectScope()); err != nil {
		t.Fatalf("同平面的已声明 P 应通过: %v", err)
	}

	userWS := NewWorkspace(WorkspaceUser, "")
	if err := ValidateRemoteRegisterScope(userWS, demoProjectScope()); err == nil ||
		!strings.Contains(err.Error(), "project secrets") {
		t.Fatalf("用户平面不应允许 project 归属, got %v", err)
	}
}

func TestValidateRemoteRegisterScope_RejectsInvalidPName(t *testing.T) {
	setupRemoteRegisterRepo(t, declaredDemoP())
	ws := NewWorkspace(WorkspaceProject, t.TempDir())
	if err := ValidateRemoteRegisterScope(ws, secrets.RemoteScope{P: "Dec", Plane: secrets.SyncPlaneProject}); err == nil {
		t.Fatal("大写 P 名应被拒绝")
	}
}
