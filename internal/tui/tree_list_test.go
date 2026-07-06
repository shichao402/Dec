package tui

import "testing"

func TestTreeList_ExpandCollapseAndSelect(t *testing.T) {
	roots := []*TreeNode{
		{
			ID: "root", Label: ".dec", SelectMode: TreeSelectNone,
			Children: []*TreeNode{
				{
					ID: "b1", Label: "vikunja", SelectMode: TreeSelectNone,
					Children: []*TreeNode{
						{ID: "leaf1", Label: "skill/a", SelectMode: TreeSelectLeaf},
					},
				},
			},
		},
	}
	tree := &TreeList{Roots: roots}
	tree.DefaultExpandAll()
	rows := tree.VisibleRows()
	if len(rows) != 3 {
		t.Fatalf("expanded rows = %d, want 3: %#v", len(rows), rows)
	}

	tree.Cursor = 0
	if !tree.CollapseAtCursor() {
		t.Fatal("应能折叠 root")
	}
	if len(tree.VisibleRows()) != 1 {
		t.Fatalf("collapsed root rows = %d, want 1", len(tree.VisibleRows()))
	}

	tree.ExpandAtCursor()
	if len(tree.VisibleRows()) != 3 {
		t.Fatalf("re-expanded rows = %d, want 3", len(tree.VisibleRows()))
	}

	tree.Cursor = 2
	if !tree.ToggleSelectAtCursor() {
		t.Fatal("应能勾选叶子")
	}
	if tree.CountSelected() != 1 {
		t.Fatalf("selected = %d, want 1", tree.CountSelected())
	}
}

func TestTreeList_CollapseFromLeafFoldsParent(t *testing.T) {
	roots := []*TreeNode{
		{
			ID: "root", Label: ".dec", SelectMode: TreeSelectNone,
			Children: []*TreeNode{
				{
					ID: "cache", Label: "cache", SelectMode: TreeSelectNone,
					Children: []*TreeNode{
						{ID: "leaf1", Label: "skill/a", SelectMode: TreeSelectLeaf},
					},
				},
			},
		},
	}
	tree := &TreeList{Roots: roots}
	tree.DefaultExpandAll()
	tree.Cursor = 2 // 叶子行
	if !tree.CollapseAtCursor() {
		t.Fatal("叶子行按 h 应折叠父目录")
	}
	rows := tree.VisibleRows()
	if len(rows) != 2 {
		t.Fatalf("折叠 cache 后 rows = %d, want 2", len(rows))
	}
	if tree.Cursor != 1 {
		t.Fatalf("光标应落在 cache, cursor=%d", tree.Cursor)
	}
}

func TestTreeList_FilterPreservesSelection(t *testing.T) {
	roots := []*TreeNode{
		{
			ID: "root", Label: ".secrets", SelectMode: TreeSelectNone,
			Children: []*TreeNode{
				{ID: "l1", Label: "alpha.toml", SelectMode: TreeSelectLeaf},
				{ID: "l2", Label: "beta.toml", SelectMode: TreeSelectLeaf},
			},
		},
	}
	tree := &TreeList{Roots: roots}
	tree.DefaultExpandAll()
	tree.rebuildSelection(2)
	tree.Selected[0] = true
	tree.SetFilter("alpha", roots)
	if tree.CountSelected() != 1 {
		t.Fatalf("筛选后应保留 alpha 勾选, selected=%d", tree.CountSelected())
	}
	if len(tree.VisibleRows()) < 2 {
		t.Fatalf("筛选应展示匹配路径, rows=%d", len(tree.VisibleRows()))
	}
}
