package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/secrets"
)

func TestPrepareAndCommitRemoteNoteEdit_UsesTempFile(t *testing.T) {
	secrets.SetSession("test-session")
	projectRoot := t.TempDir()
	noteRel := ".env/vikunja.env"
	target, err := secrets.NewPSyncTarget("vikunja", secrets.SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	// 本地同步根已有旧内容：编辑不得覆盖它。
	localAbs := filepath.Join(projectRoot, filepath.FromSlash(target.LocalRoot), filepath.FromSlash(noteRel))
	if err := os.MkdirAll(filepath.Dir(localAbs), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localAbs, []byte("LOCAL=unchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		target.Address: {{RelativePath: noteRel, Content: "OLD=1\n"}},
	}}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	item := DeleteSelectionItem{
		Kind:          DeleteKindSecret,
		SecretPath:    noteRel,
		LocalRoot:     target.LocalRoot,
		SecretsBundle: target.Address,
		DecBundleName: target.Name,
	}
	sess, err := PrepareRemoteNoteEdit(t.Context(), projectRoot, item, nil)
	if err != nil {
		t.Fatalf("PrepareRemoteNoteEdit: %v", err)
	}
	if !sess.TempFile {
		t.Fatal("应使用临时文件")
	}
	if sess.Path == localAbs {
		t.Fatalf("不应编辑本地同步根: %q", sess.Path)
	}
	if err := os.WriteFile(sess.Path, []byte("NEW=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CommitRemoteNoteEdit(t.Context(), *sess, nil); err != nil {
		t.Fatalf("CommitRemoteNoteEdit: %v", err)
	}
	notes := stub.NotesByFolder[target.Address]
	if len(notes) != 1 || notes[0].Content != "NEW=2\n" {
		t.Fatalf("notes = %#v", notes)
	}
	got, err := os.ReadFile(localAbs)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "LOCAL=unchanged\n" {
		t.Fatalf("本地同步根不应被编辑写回: %q", got)
	}
}

func TestPrepareRemoteNoteEdit_BareFolderWithoutLocalRoot(t *testing.T) {
	secrets.SetSession("test-session")
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"relkit": {{RelativePath: ".env/x.env", Content: "X=1\n"}},
	}}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	sess, err := PrepareRemoteNoteEdit(t.Context(), t.TempDir(), DeleteSelectionItem{
		Kind:          DeleteKindSecret,
		SecretPath:    ".env/x.env",
		SecretsBundle: "relkit",
	}, nil)
	if err != nil {
		t.Fatalf("PrepareRemoteNoteEdit bare: %v", err)
	}
	if !sess.TempFile || sess.Path == "" {
		t.Fatalf("应落到临时文件: %#v", sess)
	}
	raw, _ := os.ReadFile(sess.Path)
	if string(raw) != "X=1\n" {
		t.Fatalf("temp content = %q", raw)
	}
	_ = os.WriteFile(sess.Path, []byte("X=2\n"), 0o600)
	if err := CommitRemoteNoteEdit(t.Context(), *sess, nil); err == nil || !strings.Contains(err.Error(), "未声明") {
		t.Fatalf("未声明裸 folder 的编辑提交应拒绝, got %v", err)
	}
	if stub.NotesByFolder["relkit"][0].Content != "X=1\n" {
		t.Fatalf("bare folder note = %#v", stub.NotesByFolder["relkit"])
	}
}

// 机器平面候选项的 LocalRoot 相对 ~/.dec/secrets。
// 还原 SyncTarget 时若丢掉 Plane，编辑路径会误落到项目根内。
func TestSyncTargetFromRemoteItem_PreservesMachinePlane(t *testing.T) {
	target, err := secrets.NewPSyncTarget("tencent-cloud", secrets.SyncPlaneMachine)
	if err != nil {
		t.Fatal(err)
	}
	got, err := syncTargetFromRemoteItem(DeleteSelectionItem{
		Kind:          DeleteKindSecret,
		SecretPath:    ".env/tencent.env",
		LocalRoot:     target.LocalRoot,
		Plane:         target.Plane,
		SecretsBundle: target.Address,
		DecBundleName: target.Name,
	})
	if err != nil {
		t.Fatalf("syncTargetFromRemoteItem: %v", err)
	}
	if !secrets.IsMachinePlane(got.Plane) {
		t.Fatalf("Plane = %q, 期望机器平面", got.Plane)
	}

	projectRoot := t.TempDir()
	abs, err := secrets.AbsolutePath(projectRoot, got, ".env/tencent.env")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(abs, projectRoot) {
		t.Fatalf("机器平面 note 落到了项目根内: %q", abs)
	}
}

func TestCommitRemoteSSHHostsEdit(t *testing.T) {
	secrets.SetSession("test-session")
	target, err := secrets.NewPSyncTarget("vikunja", secrets.SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	stub := &secrets.StubClient{SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
		target.Address: {{
			Name:       ".sshkey/deploy",
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
	err = CommitRemoteSSHHostsEdit(t.Context(), RemoteSSHHostsEditSession{
		Path:          tmp,
		ProjectRoot:   t.TempDir(),
		Target:        target,
		KeyName:       ".sshkey/deploy",
		DecBundleName: target.Name,
		TempFile:      false,
	}, nil)
	if err != nil {
		t.Fatalf("CommitRemoteSSHHostsEdit: %v", err)
	}
	got := stub.SSHKeysByFolder[target.Address][0].Hosts
	if len(got) != 1 || got[0] != "new.example.com" {
		t.Fatalf("hosts = %#v", got)
	}
}

func TestListRemoteInventory_ListsBareFolder(t *testing.T) {
	writeRemoteBrowseSecretsConfig(t, secrets.Config{})
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"relkit": {{RelativePath: ".env/r.env", Content: "R=1\n"}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	candidates, err := ListRemoteInventory(context.Background(), NewWorkspace(WorkspaceProject, projectRoot), true, nil)
	if err != nil {
		t.Fatalf("ListRemoteInventory: %v", err)
	}
	if !hasSecretCandidate(candidates, "relkit", ".env/r.env") {
		t.Fatalf("裸 folder 应出现在 Remote: %#v", candidates)
	}
	for _, c := range candidates {
		if c.SecretsBundle == "relkit" && c.SecretPath == ".env/r.env" {
			if c.Partition != PartitionRemote {
				t.Fatalf("裸 folder 应属远端分区")
			}
			if !c.Unmanaged {
				t.Fatalf("裸 folder 应标注非 Dec管理")
			}
			return
		}
	}
}
