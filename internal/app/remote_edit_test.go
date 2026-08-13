package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/secrets"
)

func TestPrepareAndCommitRemoteNoteEdit(t *testing.T) {
	secrets.SetSession("test-session")
	projectRoot := t.TempDir()
	target, err := secrets.NewBundleSyncTarget("vikunja", "bundle/vikunja")
	if err != nil {
		t.Fatal(err)
	}
	noteRel := "env/vikunja.env"
	abs, err := secrets.AbsolutePath(projectRoot, target, noteRel)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("OLD=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"bundle/vikunja": {{RelativePath: noteRel, Content: "OLD=1\n"}},
	}}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	item := DeleteSelectionItem{
		Kind:          DeleteKindSecret,
		SecretPath:    noteRel,
		LocalRoot:     target.LocalRoot,
		SecretsBundle: target.Folder,
		DecBundleName: target.Name,
	}
	sess, err := PrepareRemoteNoteEdit(t.Context(), projectRoot, item, nil)
	if err != nil {
		t.Fatalf("PrepareRemoteNoteEdit: %v", err)
	}
	if sess.Path != abs {
		t.Fatalf("Path = %q, want %q", sess.Path, abs)
	}
	if err := os.WriteFile(abs, []byte("NEW=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CommitRemoteNoteEdit(t.Context(), *sess, nil); err != nil {
		t.Fatalf("CommitRemoteNoteEdit: %v", err)
	}
	notes := stub.NotesByFolder["bundle/vikunja"]
	if len(notes) != 1 || notes[0].Content != "NEW=2\n" {
		t.Fatalf("notes = %#v", notes)
	}
}

func TestCommitRemoteSSHHostsEdit(t *testing.T) {
	secrets.SetSession("test-session")
	stub := &secrets.StubClient{SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
		"bundle/vikunja": {{
			Name:       "deploy",
			PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----\n",
			PublicKey:  "ssh-ed25519 AAAA deploy",
			Hosts:      []string{"old.example.com"},
		}},
	}}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	tmp := filepath.Join(t.TempDir(), "hosts.txt")
	if err := os.WriteFile(tmp, []byte("new.example.com\n# comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := CommitRemoteSSHHostsEdit(t.Context(), RemoteSSHHostsEditSession{
		Path:          tmp,
		Folder:        "bundle/vikunja",
		KeyName:       "deploy",
		DecBundleName: "vikunja",
		TempFile:      false,
	}, nil)
	if err != nil {
		t.Fatalf("CommitRemoteSSHHostsEdit: %v", err)
	}
	got := stub.SSHKeysByFolder["bundle/vikunja"][0].Hosts
	if len(got) != 1 || got[0] != "new.example.com" {
		t.Fatalf("hosts = %#v", got)
	}
}
