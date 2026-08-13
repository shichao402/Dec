package tui

import (
	"testing"
)

func TestInsertTreePath_CreatesIntermediateDirs(t *testing.T) {
	root := &TreeNode{ID: "root:secrets", Label: "secrets (SyncTarget)", SelectMode: TreeSelectNone}
	group := &TreeNode{ID: "group:vikunja", Label: "bundle vikunja → .secrets/bundles/vikunja", SelectMode: TreeSelectNone}
	root.Children = append(root.Children, group)
	insertTreePath(group, secretsParentSegments("env/vikunja.env"), &TreeNode{
		ID:         "leaf1",
		Label:      secretsLeafName("env/vikunja.env"),
		SelectMode: TreeSelectLeaf,
	})
	rows := (&TreeList{Roots: []*TreeNode{root}, Expanded: map[string]bool{
		"root:secrets":      true,
		"group:vikunja":     true,
		"group:vikunja/env": true,
	}}).VisibleRows()
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	last := rows[len(rows)-1]
	if last.Node.Label != "vikunja.env" || last.SelectIndex != 1 {
		t.Fatalf("leaf row = %#v", last)
	}
	env := rows[2]
	if env.Node.Label != "env" || env.SelectIndex != 0 {
		t.Fatalf("中间目录应可勾选: %#v", env)
	}
}

func TestDecCachePathSegments(t *testing.T) {
	got := decCachePathSegments("vikunja", "skill", "vikunja-issue")
	want := []string{"cache", "vikunja", "skills", "vikunja-issue"}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}
