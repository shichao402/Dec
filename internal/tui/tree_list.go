package tui

import (
	"sort"
	"strings"
)

// TreeSelectMode 描述树节点是否可勾选及勾选语义。
type TreeSelectMode int

const (
	TreeSelectNone TreeSelectMode = iota // 目录：仅导航/展开
	TreeSelectLeaf                       // 叶子：独立勾选
	TreeSelectBranch                     // 分支自身可勾选（如 bundle 启用/删除整包）
	TreeSelectReadOnly                   // 只读子项（bundle 成员展示）
)

// TreeNode 为通用目录树节点；Children 为空且 SelectMode 为 Leaf/Branch 时视为叶子。
type TreeNode struct {
	ID         string
	Label      string
	SelectMode TreeSelectMode
	Children   []*TreeNode
	Payload    any
}

// TreeRow 为当前可见的一行（已按展开状态扁平化）。
type TreeRow struct {
	Node        *TreeNode
	Depth       int
	SelectIndex int // 对应 TreeList.Selected 下标；-1 表示不可勾选
}

// TreeList 通用目录树状态：光标、展开、勾选、筛选。
type TreeList struct {
	Roots    []*TreeNode
	Cursor   int
	Expanded map[string]bool
	Selected []bool
	Filter   string
}

func (t *TreeList) ensureExpanded() {
	if t.Expanded == nil {
		t.Expanded = make(map[string]bool)
	}
}

// Reset 清空树状态（保留 Expanded 时可传 keepExpanded=false 全清）。
func (t *TreeList) Reset(roots []*TreeNode, keepExpanded bool) {
	t.Roots = roots
	t.Cursor = 0
	if !keepExpanded {
		t.Expanded = nil
	}
	t.rebuildSelection(len(t.selectableIDs()))
}

func (t *TreeList) selectableIDs() []string {
	var ids []string
	var walk func(nodes []*TreeNode)
	walk = func(nodes []*TreeNode) {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			if treeNodeSelectable(n) {
				ids = append(ids, n.ID)
			}
			if len(n.Children) > 0 {
				walk(n.Children)
			}
		}
	}
	walk(t.Roots)
	return ids
}

func treeNodeSelectable(n *TreeNode) bool {
	switch n.SelectMode {
	case TreeSelectLeaf, TreeSelectBranch:
		return true
	default:
		return false
	}
}

func treeNodeExpandable(n *TreeNode) bool {
	if n == nil {
		return false
	}
	if n.SelectMode == TreeSelectBranch {
		return len(n.Children) > 0
	}
	return len(n.Children) > 0 && n.SelectMode != TreeSelectReadOnly
}

func (t *TreeList) rebuildSelection(selectableCount int) {
	if selectableCount < 0 {
		selectableCount = 0
	}
	if len(t.Selected) != selectableCount {
		t.Selected = make([]bool, selectableCount)
	}
}

// SetFilter 更新筛选并重建勾选位图（保留仍存在的 ID 勾选状态）。
func (t *TreeList) SetFilter(filter string, roots []*TreeNode) {
	prev := make(map[string]bool)
	for i, id := range t.selectableIDs() {
		if i < len(t.Selected) && t.Selected[i] {
			prev[id] = true
		}
	}
	t.Filter = filter
	t.Roots = roots
	t.rebuildSelection(len(t.collectSelectableIDs(roots)))
	for i, id := range t.collectSelectableIDs(roots) {
		if prev[id] {
			t.Selected[i] = true
		}
	}
	t.normalizeCursor()
}

func (t *TreeList) collectSelectableIDs(roots []*TreeNode) []string {
	old := t.Roots
	t.Roots = roots
	ids := t.selectableIDs()
	t.Roots = old
	return ids
}

func (t *TreeList) VisibleRows() []TreeRow {
	t.ensureExpanded()
	filter := strings.ToLower(strings.TrimSpace(t.Filter))
	var rows []TreeRow
	selectIdx := 0
	var walk func(nodes []*TreeNode, depth int, ancestors []*TreeNode)
	walk = func(nodes []*TreeNode, depth int, ancestors []*TreeNode) {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			if filter != "" && !treeNodeMatchesFilter(n, filter) {
				continue
			}
			idx := -1
			if treeNodeSelectable(n) {
				idx = selectIdx
				selectIdx++
			}
			rows = append(rows, TreeRow{Node: n, Depth: depth, SelectIndex: idx})
			if len(n.Children) == 0 {
				continue
			}
			if !t.Expanded[n.ID] && filter == "" {
				continue
			}
			walk(n.Children, depth+1, append(ancestors, n))
		}
	}
	walk(t.Roots, 0, nil)
	return rows
}

func treeNodeMatchesFilter(n *TreeNode, filter string) bool {
	if strings.Contains(strings.ToLower(n.Label), filter) {
		return true
	}
	for _, child := range n.Children {
		if treeNodeMatchesFilter(child, filter) {
			return true
		}
	}
	return false
}

func (t *TreeList) FocusFirstSelectable() {
	rows := t.VisibleRows()
	for i, row := range rows {
		if row.SelectIndex >= 0 {
			t.Cursor = i
			return
		}
	}
	t.normalizeCursor()
}

func (t *TreeList) normalizeCursor() {
	rows := t.VisibleRows()
	if len(rows) == 0 {
		t.Cursor = 0
		return
	}
	if t.Cursor >= len(rows) {
		t.Cursor = len(rows) - 1
	}
	if t.Cursor < 0 {
		t.Cursor = 0
	}
}

func (t *TreeList) MoveCursor(delta int) {
	rows := t.VisibleRows()
	if len(rows) == 0 {
		return
	}
	t.Cursor += delta
	t.normalizeCursor()
}

func (t *TreeList) currentRow() (TreeRow, bool) {
	rows := t.VisibleRows()
	if len(rows) == 0 || t.Cursor < 0 || t.Cursor >= len(rows) {
		return TreeRow{}, false
	}
	return rows[t.Cursor], true
}

func (t *TreeList) ToggleSelectAtCursor() bool {
	row, ok := t.currentRow()
	if !ok || row.SelectIndex < 0 || row.SelectIndex >= len(t.Selected) {
		return false
	}
	t.Selected[row.SelectIndex] = !t.Selected[row.SelectIndex]
	return true
}

func (t *TreeList) SelectAllAtCursor() {
	rows := t.VisibleRows()
	if len(rows) == 0 {
		return
	}
	for i := range t.Selected {
		t.Selected[i] = true
	}
}

func (t *TreeList) CountSelectable() int {
	return len(t.Selected)
}

func (t *TreeList) CountSelected() int {
	n := 0
	for _, on := range t.Selected {
		if on {
			n++
		}
	}
	return n
}

func (t *TreeList) IsSelected(selectIndex int) bool {
	return selectIndex >= 0 && selectIndex < len(t.Selected) && t.Selected[selectIndex]
}

func (t *TreeList) ExpandAtCursor() bool {
	t.ensureExpanded()
	row, ok := t.currentRow()
	if !ok || !treeNodeExpandable(row.Node) {
		return false
	}
	if t.Expanded[row.Node.ID] {
		return false
	}
	t.Expanded[row.Node.ID] = true
	t.normalizeCursor()
	return true
}

func (t *TreeList) CollapseAtCursor() bool {
	t.ensureExpanded()
	row, ok := t.currentRow()
	if !ok {
		return false
	}
	rows := t.VisibleRows()
	node := row.Node
	targetDepth := row.Depth

	if treeNodeExpandable(node) && t.Expanded[node.ID] {
		delete(t.Expanded, node.ID)
		t.normalizeCursor()
		return true
	}

	// 叶子或已折叠目录：折叠最近的已展开祖先（便于 Delete / Bundles 深层导航）。
	for i := t.Cursor - 1; i >= 0; i-- {
		parent := rows[i]
		if parent.Depth < targetDepth && treeNodeExpandable(parent.Node) && t.Expanded[parent.Node.ID] {
			delete(t.Expanded, parent.Node.ID)
			t.Cursor = i
			t.normalizeCursor()
			return true
		}
	}
	return false
}

func (t *TreeList) CursorOnExpandable() bool {
	row, ok := t.currentRow()
	return ok && treeNodeExpandable(row.Node)
}

func (t *TreeList) CursorExpanded() bool {
	row, ok := t.currentRow()
	if !ok {
		return false
	}
	t.ensureExpanded()
	return t.Expanded[row.Node.ID]
}

// DefaultExpandAll 展开所有有子节点的分支（用于 Delete 页初次加载）。
func (t *TreeList) DefaultExpandAll() {
	t.ensureExpanded()
	var walk func(nodes []*TreeNode)
	walk = func(nodes []*TreeNode) {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			if treeNodeExpandable(n) {
				t.Expanded[n.ID] = true
			}
			walk(n.Children)
		}
	}
	walk(t.Roots)
	t.rebuildSelection(len(t.selectableIDs()))
}

func sortTreeChildren(nodes []*TreeNode, less func(a, b *TreeNode) bool) {
	sort.Slice(nodes, func(i, j int) bool {
		return less(nodes[i], nodes[j])
	})
	for _, n := range nodes {
		if len(n.Children) > 0 {
			sortTreeChildren(n.Children, less)
		}
	}
}
