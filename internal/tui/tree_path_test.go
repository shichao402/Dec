package tui

import "testing"

func TestInsertTreePath_CreatesIntermediateDirs(t *testing.T) {
	root := &TreeNode{ID: "root:.secrets", Label: ".secrets", SelectMode: TreeSelectNone}
	insertTreePath(root, []string{"vikunja_workflow", "mise", "conf.d"}, &TreeNode{
		ID:         "leaf1",
		Label:      "vikunja.toml",
		SelectMode: TreeSelectLeaf,
	})
	rows := (&TreeList{Roots: []*TreeNode{root}, Expanded: map[string]bool{
		"root:.secrets":                      true,
		"root:.secrets/vikunja_workflow":     true,
		"root:.secrets/vikunja_workflow/mise": true,
		"root:.secrets/vikunja_workflow/mise/conf.d": true,
	}}).VisibleRows()
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	last := rows[len(rows)-1]
	if last.Node.Label != "vikunja.toml" || last.SelectIndex != 0 {
		t.Fatalf("leaf row = %#v", last)
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
