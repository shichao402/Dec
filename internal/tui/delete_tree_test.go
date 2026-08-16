package tui

import (
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/app"
)

// SSH Key 落 ~/.ssh，候选里没有 LocalRoot；不能因此把同一 folder 拆成两个顶层节点。
func TestBuildDeleteTree_MergesNoteAndSSHUnderSameFolder(t *testing.T) {
	roots := buildDeleteTree([]app.DeleteCandidate{
		{
			Kind:          app.DeleteKindSecret,
			SecretPath:    "env/github.env",
			SecretsBundle: "bundle/github",
			LocalRoot:     "bundles/github",
			Partition:     app.PartitionRemote,
			GroupTitle:    "bundle/github",
			Orphan:        true,
		},
		{
			Kind:          app.DeleteKindSSHKey,
			SSHKeyName:    "github_commit",
			DecBundleName: "github",
			SecretsBundle: "bundle/github",
			Partition:     app.PartitionRemote,
			GroupTitle:    "bundle/github",
			Orphan:        true,
		},
	})

	var secRemote *TreeNode
	for _, r := range roots {
		if r != nil && r.ID == "delete-root:secrets" {
			secRemote = r
		}
	}
	if secRemote == nil {
		t.Fatalf("缺少远端 secrets 根: %#v", roots)
	}
	if len(secRemote.Children) != 1 {
		var labels []string
		for _, child := range secRemote.Children {
			labels = append(labels, child.Label)
		}
		t.Fatalf("同一 folder 应只有一个分组节点, got %d: %v", len(secRemote.Children), labels)
	}

	group := secRemote.Children[0]
	if group.Label != "bundle/github → bundles/github" {
		t.Fatalf("分组标题应带本地映射, got %q", group.Label)
	}

	var envDir, sshDir *TreeNode
	for _, child := range group.Children {
		switch child.Label {
		case "env":
			envDir = child
		case "SSH · machine":
			sshDir = child
		}
	}
	if envDir == nil || sshDir == nil {
		t.Fatalf("同一分组下应同时含 env 与 SSH 子分组: %#v", group.Children)
	}
	if len(envDir.Children) != 1 || !strings.Contains(envDir.Children[0].Label, "github.env") {
		t.Fatalf("env 子分组内容异常: %#v", envDir.Children)
	}
	if len(sshDir.Children) != 1 || !strings.Contains(sshDir.Children[0].Label, "github_commit") {
		t.Fatalf("SSH 子分组内容异常: %#v", sshDir.Children)
	}
}

// 不同 folder 即便本地根相同也不应被合并。
func TestBuildDeleteTree_KeepsDistinctFoldersSeparate(t *testing.T) {
	roots := buildDeleteTree([]app.DeleteCandidate{
		{
			Kind: app.DeleteKindSecret, SecretPath: "a.env", SecretsBundle: "bundle/one",
			LocalRoot: "bundles/shared", Partition: app.PartitionRemote, GroupTitle: "bundle/one",
		},
		{
			Kind: app.DeleteKindSecret, SecretPath: "b.env", SecretsBundle: "bundle/two",
			LocalRoot: "bundles/shared", Partition: app.PartitionRemote, GroupTitle: "bundle/two",
		},
	})
	for _, r := range roots {
		if r == nil || r.ID != "delete-root:secrets" {
			continue
		}
		if len(r.Children) != 2 {
			t.Fatalf("两个 folder 应有两个分组, got %d", len(r.Children))
		}
	}
}

// SSH 先于 Note 出现时，分组标题仍要补上本地映射。
func TestBuildDeleteTree_GroupLabelPrefersLocalRoot(t *testing.T) {
	roots := buildDeleteTree([]app.DeleteCandidate{
		{
			Kind: app.DeleteKindSSHKey, SSHKeyName: "k", SecretsBundle: "bundle/github",
			Partition: app.PartitionRemote, GroupTitle: "bundle/github",
		},
		{
			Kind: app.DeleteKindSecret, SecretPath: "env/x.env", SecretsBundle: "bundle/github",
			LocalRoot: "bundles/github", Partition: app.PartitionRemote, GroupTitle: "bundle/github",
		},
	})
	for _, r := range roots {
		if r == nil || r.ID != "delete-root:secrets" {
			continue
		}
		if len(r.Children) != 1 || r.Children[0].Label != "bundle/github → bundles/github" {
			t.Fatalf("分组标题应为带本地映射的单节点: %#v", r.Children)
		}
	}
}
