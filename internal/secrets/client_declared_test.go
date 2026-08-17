package secrets

import (
	"context"
	"strings"
	"testing"
)

func TestClientWritesRejectUndeclaredTarget(t *testing.T) {
	client := &StubClient{}
	target := SyncTarget{
		Kind:      SyncKindBundle,
		Name:      "demo",
		Folder:    "bundle/demo",
		LocalRoot: BundleSecretsLocalRelPrefix + "/demo",
	}

	if _, err := client.PushBundle(context.Background(), PushBundleRequest{Target: target}, []SecureNote{{
		RelativePath: "token.txt",
		Content:      "secret",
	}}); err == nil || !strings.Contains(err.Error(), "ADR 0013") {
		t.Fatalf("PushBundle 应拒绝未声明 target, got %v", err)
	}
	if err := client.CreateSSHKey(context.Background(), CreateSSHKeyRequest{Target: target}); err == nil ||
		!strings.Contains(err.Error(), "ADR 0013") {
		t.Fatalf("CreateSSHKey 应拒绝未声明 target, got %v", err)
	}
}

func TestDeleteSecureNoteAllowsUndeclaredFolder(t *testing.T) {
	client := &StubClient{NotesByFolder: map[string][]SecureNote{
		"legacy": {{RelativePath: "token.txt", Content: "secret"}},
	}}
	browse, err := NewBrowseFolder("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteSecureNote(context.Background(), DeleteSecureNoteRequest{
		Target:   browse,
		NotePath: "token.txt",
	}); err != nil {
		t.Fatalf("删除裸 folder 存量不应要求 Declared: %v", err)
	}
	if len(client.NotesByFolder["legacy"]) != 0 {
		t.Fatalf("note 未删除: %#v", client.NotesByFolder["legacy"])
	}
}
