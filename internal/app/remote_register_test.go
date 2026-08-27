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

func TestPrepareAndCommitRemoteRegister_NotePath(t *testing.T) {
	setupRemoteRegisterRepo(t, map[string]string{
		"bundles/demo/bundle.yaml": "name: demo\nscope: project\nmembers: []\n",
	})
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
	sess, err := PrepareRemoteRegister(context.Background(), RemoteRegisterInput{
		ProjectRoot:  projectRoot,
		Folder:       "bundle/demo",
		TypeID:       secrets.SecretTypeEnv,
		Name:         "app",
		SourceMode:   secrets.SourcePath,
		LocalPath:    src,
		CreateFolder: true,
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
	if result.NoteRelPath != ".env/app.env" {
		t.Fatalf("result = %#v", result)
	}
	notes := stub.NotesByFolder["bundle/demo"]
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
	setupRemoteRegisterRepo(t, map[string]string{
		"bundles/demo/bundle.yaml": "name: demo\nscope: project\nmembers: []\n",
	})

	secrets.SetSession("test-session")
	stub := &secrets.StubClient{SSHKeysByFolder: map[string][]secrets.SSHKeyItem{}}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	sess, err := PrepareRemoteRegister(context.Background(), RemoteRegisterInput{
		ProjectRoot:  t.TempDir(),
		Folder:       "bundle/demo",
		TypeID:       secrets.SecretTypeSSHKey,
		Name:         "deploy",
		SourceMode:   secrets.SourceGenerate,
		CreateFolder: true,
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
	keys := stub.SSHKeysByFolder["bundle/demo"]
	if len(keys) != 1 || keys[0].Name != ".sshkey/deploy" {
		t.Fatalf("keys = %#v", keys)
	}
	if !strings.Contains(keys[0].PrivateKey, "PRIVATE KEY") {
		t.Fatal("private key should be stored")
	}
	priv := filepath.Join(home, ".ssh", "dec_demo_deploy")
	if _, err := os.Stat(priv); err != nil {
		t.Fatalf("expected local landing %s: %v", priv, err)
	}
}

func TestCommitRemoteRegister_RejectsUndeclaredBareFolder(t *testing.T) {
	setupRemoteRegisterRepo(t, map[string]string{
		"projects/Dec.yaml": "name: Dec\nbundles: []\n",
	})
	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)

	_, err := CommitRemoteRegister(t.Context(), RemoteRegisterSession{
		ProjectRoot: t.TempDir(),
		Folder:      "relkit",
		TypeID:      secrets.SecretTypePlain,
		Name:        "token.txt",
		NoteContent: "secret",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "bundle/relkit") {
		t.Fatalf("裸名应拒绝并提示 bundle/<名>, got %v", err)
	}
}

func TestCommitRemoteRegister_AcceptsBundleFolderAndCreatesPlaceholder(t *testing.T) {
	setupRemoteRegisterRepo(t, map[string]string{"README.md": "fixture\n"})
	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{}}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	result, err := CommitRemoteRegister(t.Context(), RemoteRegisterSession{
		ProjectRoot:  t.TempDir(),
		Folder:       "bundle/foo",
		TypeID:       secrets.SecretTypePlain,
		Name:         "token.txt",
		NoteContent:  "secret",
		CreateFolder: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Folder != "bundle/foo" || result.Kind != secrets.SyncKindBundle {
		t.Fatalf("result = %#v", result)
	}
	tx, err := repo.NewReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	data, err := os.ReadFile(filepath.Join(tx.WorkDir(), "bundles", "foo", "bundle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: foo") {
		t.Fatalf("placeholder = %s", data)
	}
}

func TestCommitRemoteRegister_RejectsBareProjectFolder(t *testing.T) {
	setupRemoteRegisterRepo(t, map[string]string{
		"projects/Dec.yaml": "name: Dec\nbundles: []\n",
	})
	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{}}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	_, err := CommitRemoteRegister(t.Context(), RemoteRegisterSession{
		ProjectRoot:  t.TempDir(),
		Folder:       "Dec",
		TypeID:       secrets.SecretTypePlain,
		Name:         "token.txt",
		NoteContent:  "secret",
		CreateFolder: true,
	}, nil)
	if err == nil {
		t.Fatal("expected reject bare project folder")
	}
	if !strings.Contains(err.Error(), "bundle/") {
		t.Fatalf("err = %v", err)
	}
}

// 裸 folder 名在两种仓库形态下都非法，但引导语不同：旧仓库指向 bundle/<name>，
// P 仓库指向 <p>/private/<plane>。必须自建仓库状态，否则会继承上个测试的仓库。
func TestValidateRemoteRegisterFolder_RejectsBareProjectOnPRepository(t *testing.T) {
	setupRemoteRegisterRepo(t, map[string]string{
		"dec/dec.yaml": "name: dec\ntitle: Dec\n",
	})
	err := ValidateRemoteRegisterFolder(NewWorkspace(WorkspaceProject, t.TempDir()), "Dec")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "private/project") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateRemoteRegisterFolder_RejectsBareProjectOnLegacyRepository(t *testing.T) {
	setupRemoteRegisterRepo(t, map[string]string{
		"projects/Dec.yaml": "name: Dec\nbundles: []\n",
	})
	err := ValidateRemoteRegisterFolder(NewWorkspace(WorkspaceProject, t.TempDir()), "Dec")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bundle/") {
		t.Fatalf("err = %v", err)
	}
}
