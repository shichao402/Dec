package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/internal/app"
)

func TestBuildDeleteTree_UnfiledReadOnlyCollapsed(t *testing.T) {
	roots := buildDeleteTree([]app.DeleteCandidate{
		{
			Kind:          app.DeleteKindSecret,
			SecretPath:    "env/a.env",
			SecretsBundle: "Dec",
			LocalRoot:     ".secrets/project",
			Partition:     app.PartitionRemote,
			GroupTitle:    "Dec (project)",
		},
		{
			Kind:       app.DeleteKindSecret,
			Type:       "login",
			Name:       "loose",
			SecretPath: "loose",
			ReadOnly:   true,
			Unmanaged:  true,
			Partition:  app.PartitionRemote,
			GroupTitle: "无文件夹 · 非Dec管理",
		},
	})
	var hasUnfiled bool
	for _, r := range roots {
		if r != nil && r.ID == "delete-root:unfiled" {
			hasUnfiled = true
			if r.SelectMode != TreeSelectNone {
				t.Fatalf("无文件夹根应为 TreeSelectNone")
			}
			if len(r.Children) != 1 || r.Children[0].SelectMode != TreeSelectReadOnly {
				t.Fatalf("只读叶子异常: %#v", r.Children)
			}
		}
	}
	if !hasUnfiled {
		t.Fatalf("缺少无文件夹根: %#v", roots)
	}

	m := newModel(t.TempDir(), "v1")
	m.pages = []string{"Remote"}
	m.pageIndex = 0
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindSecret, SecretPath: "env/a.env", SecretsBundle: "Dec", LocalRoot: ".secrets/project", Partition: app.PartitionRemote, GroupTitle: "Dec"},
		{Kind: app.DeleteKindSecret, Type: "note", Name: "x", SecretPath: "x", ReadOnly: true, Unmanaged: true, Partition: app.PartitionRemote},
	}
	m.rebuildDeleteTree()
	if m.deleteTree.Expanded["delete-root:unfiled"] {
		t.Fatal("无文件夹区默认应折叠")
	}
}

func TestDeleteTypedConfirm_RequiresInput(t *testing.T) {
	m := newModel(t.TempDir(), "v1")
	m.pages = []string{"Remote"}
	m.pageIndex = 0
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{{
		Kind:          app.DeleteKindSecret,
		SecretPath:    "env/x.env",
		SecretsBundle: "relkit",
		Partition:     app.PartitionRemote,
		Unmanaged:     true,
	}}
	m.rebuildDeleteTree()
	// 勾选叶子
	for i := range m.deleteTree.Selected {
		m.deleteTree.Selected[i] = true
	}
	m.deleteStage = "summary"
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	after := updated.(model)
	if after.deleteStage != "typed" {
		t.Fatalf("跨上下文 summary y 后应进 typed, got %q", after.deleteStage)
	}
	view := after.View()
	if !strings.Contains(view, "relkit") || !strings.Contains(view, "DELETE") {
		t.Fatalf("typed 页应提示输入:\n%s", view)
	}
	// 错误输入
	updated, _ = after.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("nope")})
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.deleteStage != "typed" {
		t.Fatalf("错误输入后应留在 typed, got %q", after.deleteStage)
	}
	if cmd != nil {
		t.Fatal("错误输入不应启动删除")
	}
	// 正确输入 DELETE
	after.deleteTypedInput = ""
	updated = typeRunes(after, "DELETE")
	updated, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.deleteStage != "running" {
		t.Fatalf("DELETE 后应 running, got %q", after.deleteStage)
	}
	if cmd == nil {
		t.Fatal("应启动删除命令")
	}
}

func TestRemoteAddSecret_OpensArbitraryFolderFlow(t *testing.T) {
	m := newModel(t.TempDir(), "v1")
	m.pages = []string{"Remote"}
	m.pageIndex = 0
	m.focus = focusContent
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	after := updated.(model)
	if !after.addSecretRemoteMode || after.addSecretStage != addSecretStageTarget {
		t.Fatalf("Remote A 应开启 remote 登记: mode=%v stage=%q", after.addSecretRemoteMode, after.addSecretStage)
	}
	after.addSecretTargets = []app.SecretTargetOption{{Folder: "relkit", Label: "relkit（裸 folder）"}}
	updated, _ = after.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretStage != addSecretStagePath {
		t.Fatalf("stage=%q want path", after.addSecretStage)
	}
	updated = typeRunes(after, "env/x.env")
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretStage != addSecretStageSource {
		t.Fatalf("stage=%q want source", after.addSecretStage)
	}
}
