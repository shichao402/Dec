package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/secrets"
)

func TestAnalyzeDeleteTypedConfirm_UnmanagedRequiresTyped(t *testing.T) {
	spec := AnalyzeDeleteTypedConfirm([]DeleteSelectionItem{{
		Kind:          DeleteKindSecret,
		SecretsBundle: "relkit",
		SecretPath:    ".env/x.env",
		Partition:     PartitionRemote,
		Unmanaged:     true,
	}}, NewWorkspace(WorkspaceProject, t.TempDir()))
	if !spec.Required {
		t.Fatal("Unmanaged 应要求 typed confirm")
	}
	if spec.Expect != "relkit" {
		t.Fatalf("Expect = %q, want relkit", spec.Expect)
	}
	if !MatchDeleteTypedConfirm("relkit", spec) {
		t.Fatal("输入 folder 名应通过")
	}
	if !MatchDeleteTypedConfirm("DELETE", spec) {
		t.Fatal("输入 DELETE 应通过")
	}
	if MatchDeleteTypedConfirm("wrong", spec) {
		t.Fatal("错误输入不应通过")
	}
}

func TestAnalyzeDeleteTypedConfirm_SameContextNoTyped(t *testing.T) {
	spec := AnalyzeDeleteTypedConfirm([]DeleteSelectionItem{{
		Kind:          DeleteKindSecret,
		SecretsBundle: "Dec",
		SecretPath:    ".env/a.env",
		Partition:     PartitionRemote,
	}}, NewWorkspace(WorkspaceProject, t.TempDir()))
	if spec.Required {
		t.Fatalf("同上下文不应要求 typed: %#v", spec)
	}
}

func TestAnalyzeDeleteTypedConfirm_CrossPlaneScope(t *testing.T) {
	spec := AnalyzeDeleteTypedConfirm([]DeleteSelectionItem{{
		Kind:      DeleteKindDecAsset,
		Type:      "skill",
		Name:      "tencent-cloud",
		Vault:     "tencent-cloud",
		Partition: PartitionRemote,
		ScopeTag:  "user",
	}}, NewWorkspace(WorkspaceProject, t.TempDir()))
	if !spec.Required || spec.Expect != "tencent-cloud" {
		t.Fatalf("跨平面 Dec 资产应要求 typed: %#v", spec)
	}
}

func TestAnalyzeDeleteTypedConfirm_MultiFolderExpectsDELETE(t *testing.T) {
	spec := AnalyzeDeleteTypedConfirm([]DeleteSelectionItem{
		{Kind: DeleteKindSecret, SecretsBundle: "relkit", Partition: PartitionRemote, Unmanaged: true},
		{Kind: DeleteKindSecret, SecretsBundle: "MyQuant", Partition: PartitionRemote, Unmanaged: true},
	}, NewWorkspace(WorkspaceProject, t.TempDir()))
	if !spec.Required || spec.Expect != "DELETE" {
		t.Fatalf("多 folder 应 Expect DELETE: %#v", spec)
	}
}

func TestRegisterRemoteNoteFromPath_PushesWithoutLocalRoot(t *testing.T) {
	setupRemoteRegisterRepo(t, map[string]string{
		"relkit/dec.yaml": "name: relkit\n",
	})
	secrets.SetSession("test-session")
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{}}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	projectRoot := t.TempDir()
	src := filepath.Join(projectRoot, "source.env")
	if err := os.WriteFile(src, []byte("TOKEN=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scope := secrets.RemoteScope{P: "relkit", Plane: secrets.SyncPlaneProject}
	result, err := RegisterRemoteNoteFromPath(context.Background(), projectRoot, scope, ".env/token.env", src, nil)
	if err != nil {
		t.Fatalf("RegisterRemoteNoteFromPath: %v", err)
	}
	if result.Address != scope.String() || result.NoteRelPath != ".env/token.env" {
		t.Fatalf("result = %#v", result)
	}
	notes := stub.NotesByFolder[scope.String()]
	if len(notes) != 1 || notes[0].Content != "TOKEN=1\n" {
		t.Fatalf("notes = %#v", notes)
	}
}

func TestPrepareRemoteNoteRegister_UsesTemp(t *testing.T) {
	setupRemoteRegisterRepo(t, map[string]string{
		"vikunja/dec.yaml": "name: vikunja\n",
	})
	secrets.SetSession("test-session")
	stub := &secrets.StubClient{}
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = orig })

	scope := secrets.RemoteScope{P: "vikunja", Plane: secrets.SyncPlaneProject}
	sess, err := PrepareRemoteNoteRegister(context.Background(), t.TempDir(), scope, ".env/new.env", "", nil)
	if err != nil {
		t.Fatalf("PrepareRemoteNoteRegister: %v", err)
	}
	if !sess.TempFile || sess.Path == "" {
		t.Fatalf("应使用临时文件: %#v", sess)
	}
	if err := os.WriteFile(sess.Path, []byte("NEW=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CommitRemoteNoteRegister(context.Background(), *sess, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got := stub.NotesByFolder[scope.String()]
	if len(got) != 1 || got[0].Content != "NEW=1\n" {
		t.Fatalf("notes = %#v", got)
	}
}

func TestListRemoteInventory_IncludesUnfiledReadOnly(t *testing.T) {
	writeRemoteBrowseSecretsConfig(t, secrets.Config{})
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			UnfiledItems: []secrets.UnfiledItem{
				{ID: "u1", Name: "orphan-login", Type: "login"},
				{ID: "u2", Name: "loose-note", Type: "note"},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	candidates, err := ListRemoteInventory(context.Background(), NewWorkspace(WorkspaceProject, t.TempDir()), true, nil)
	if err != nil {
		t.Fatalf("ListRemoteInventory: %v", err)
	}
	found := 0
	for _, c := range candidates {
		if !c.ReadOnly {
			continue
		}
		found++
		if !c.Unmanaged || c.GroupTitle != unfiledGroupTitle {
			t.Fatalf("只读项标注错误: %#v", c)
		}
	}
	if found != 2 {
		t.Fatalf("只读无文件夹项 = %d, want 2；candidates=%#v", found, candidates)
	}
}
