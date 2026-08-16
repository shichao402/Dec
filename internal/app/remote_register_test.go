package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/secrets"
)

func TestPrepareAndCommitRemoteRegister_NotePath(t *testing.T) {
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
	t.Setenv("HOME", home)

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
