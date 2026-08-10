package tui

import (
	"fmt"
	"testing"
)

func TestTreeList_CollapsedSubtreeKeepsSelectIndexAlignment(t *testing.T) {
	roots := []*TreeNode{
		{
			ID: "root", Label: ".dec", SelectMode: TreeSelectBranch,
			Children: []*TreeNode{
				{
					ID: "tencent", Label: "tencent-cloud", SelectMode: TreeSelectBranch,
					Children: []*TreeNode{
						{ID: "t-leaf", Label: "skill/t", SelectMode: TreeSelectLeaf, Payload: 0},
					},
				},
				{
					ID: "default", Label: "default", SelectMode: TreeSelectBranch,
					Children: []*TreeNode{
						{ID: "d-leaf1", Label: "skill/a", SelectMode: TreeSelectLeaf, Payload: 1},
						{ID: "d-leaf2", Label: "skill/b", SelectMode: TreeSelectLeaf, Payload: 2},
					},
				},
			},
		},
	}
	tree := &TreeList{Roots: roots}
	tree.DefaultExpandAll()
	// 折叠 tencent-cloud（模拟截图：旁系折叠时勾选 default）
	tree.Expanded["tencent"] = false

	rows := tree.VisibleRows()
	var leaf1Row TreeRow
	foundDefault, foundLeaf := false, false
	for _, row := range rows {
		if row.Node.ID == "default" {
			foundDefault = true
		}
		if row.Node.ID == "d-leaf1" {
			leaf1Row, foundLeaf = row, true
		}
	}
	if !foundDefault || !foundLeaf {
		t.Fatalf("应能看到 default 与其子叶子, rows=%d", len(rows))
	}
	// selectable 顺序：root, tencent, t-leaf, default, d-leaf1, d-leaf2
	if leaf1Row.SelectIndex != 4 {
		t.Fatalf("折叠旁系后 d-leaf1 SelectIndex = %d, want 4（避免与 t-leaf 抢 0）", leaf1Row.SelectIndex)
	}

	tree.Cursor = indexOfVisibleRow(rows, "d-leaf1")
	tree.ToggleSelectAtCursor()
	if !tree.IsSelected(4) {
		t.Fatal("应勾选全局下标 4，而不是 0")
	}
	if tree.IsSelected(2) {
		t.Fatal("不应误勾选折叠分支下的 t-leaf")
	}

	tree.Cursor = indexOfVisibleRow(rows, "default")
	tree.ToggleSelectAtCursor()
	if !tree.IsSelected(4) || !tree.IsSelected(5) {
		t.Fatalf("勾选 default 应级联全选子叶子, selected=%v", tree.Selected)
	}
}

func indexOfVisibleRow(rows []TreeRow, id string) int {
	for i, row := range rows {
		if row.Node.ID == id {
			return i
		}
	}
	return 0
}

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
			ID: "root", Label: "secrets (Bitwarden)", SelectMode: TreeSelectNone,
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

func TestTreeList_ViewportFollowsCursorAndPage(t *testing.T) {
	roots := make([]*TreeNode, 0, 30)
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("n%02d", i)
		roots = append(roots, &TreeNode{ID: id, Label: id, SelectMode: TreeSelectLeaf})
	}
	tree := &TreeList{Roots: roots}
	tree.SetViewport(5)
	if len(tree.WindowRows()) != 5 {
		t.Fatalf("WindowRows = %d, want 5", len(tree.WindowRows()))
	}
	tree.MoveCursor(7)
	if tree.Cursor != 7 {
		t.Fatalf("Cursor = %d, want 7", tree.Cursor)
	}
	if tree.Offset != 3 { // 7 落在 [3,8)
		t.Fatalf("Offset = %d, want 3", tree.Offset)
	}
	win := tree.WindowRows()
	if len(win) != 5 || win[0].Node.ID != "n03" {
		t.Fatalf("window start = %s, want n03", win[0].Node.ID)
	}
	tree.PageCursor(1)
	if tree.Cursor != 12 {
		t.Fatalf("PageCursor Cursor = %d, want 12", tree.Cursor)
	}
	if tree.Cursor < tree.Offset || tree.Cursor >= tree.Offset+tree.Viewport {
		t.Fatalf("翻页后光标应在视口内 cursor=%d offset=%d vp=%d", tree.Cursor, tree.Offset, tree.Viewport)
	}
}
