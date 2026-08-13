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

// TreeList 通用目录树状态：光标、展开、勾选、筛选、列表视口滚动。
type TreeList struct {
	Roots    []*TreeNode
	Cursor   int
	Offset   int // 可见窗口起始行（相对 VisibleRows）
	Viewport int // 列表可视行数；0 = 不裁剪（测试/未初始化）
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
	t.Offset = 0
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

func countSelectableNodes(nodes []*TreeNode) int {
	n := 0
	var walk func([]*TreeNode)
	walk = func(nodes []*TreeNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			if treeNodeSelectable(node) {
				n++
			}
			if len(node.Children) > 0 {
				walk(node.Children)
			}
		}
	}
	walk(nodes)
	return n
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
				// 筛选掉的整支仍要推进 selectIdx，保持与 Selected / selectableIDs 对齐。
				selectIdx += countSelectableNodes([]*TreeNode{n})
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
				// 折叠子树：不渲染子行，但必须推进 selectIdx，避免勾选错位到其他分支。
				selectIdx += countSelectableNodes(n.Children)
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
		t.Offset = 0
		return
	}
	if t.Cursor >= len(rows) {
		t.Cursor = len(rows) - 1
	}
	if t.Cursor < 0 {
		t.Cursor = 0
	}
	t.EnsureCursorVisible()
}

func (t *TreeList) SetViewport(viewport int) {
	if viewport < 0 {
		viewport = 0
	}
	t.Viewport = viewport
	t.EnsureCursorVisible()
}

// scrollMargin 是光标与视口上下边缘之间保持的行数：约 1/3 视口高度，
// 使光标越过 ~33% / ~66% 处列表就提前滚动，而不是撞到边缘才动。
// 列表首尾附近由 EnsureCursorVisible 的 Offset 夹取回落，光标仍能走到第一/最后一行。
func (t *TreeList) scrollMargin() int {
	if t.Viewport <= 2 {
		return 0
	}
	margin := t.Viewport / 3
	// 上下留白之和必须小于视口，否则两侧约束互相打架。
	if limit := (t.Viewport - 1) / 2; margin > limit {
		margin = limit
	}
	return margin
}

// EnsureCursorVisible 调整 Offset，使 Cursor 连同上下 scrollMargin 行都落在视口内。
func (t *TreeList) EnsureCursorVisible() {
	rows := t.VisibleRows()
	n := len(rows)
	if n == 0 {
		t.Offset = 0
		return
	}
	if t.Viewport <= 0 {
		t.Offset = 0
		return
	}
	margin := t.scrollMargin()
	if top := t.Cursor - margin; top < t.Offset {
		t.Offset = top
	}
	if bottom := t.Cursor + margin; bottom >= t.Offset+t.Viewport {
		t.Offset = bottom - t.Viewport + 1
	}
	maxOff := n - t.Viewport
	if maxOff < 0 {
		maxOff = 0
	}
	if t.Offset > maxOff {
		t.Offset = maxOff
	}
	if t.Offset < 0 {
		t.Offset = 0
	}
}

// WindowRows 返回当前视口内的可见行（绝对下标通过 Offset+i 还原）。
func (t *TreeList) WindowRows() []TreeRow {
	rows := t.VisibleRows()
	t.EnsureCursorVisible()
	if t.Viewport <= 0 || len(rows) <= t.Viewport {
		return rows
	}
	end := t.Offset + t.Viewport
	if end > len(rows) {
		end = len(rows)
	}
	if t.Offset < 0 {
		t.Offset = 0
	}
	if t.Offset >= len(rows) {
		return nil
	}
	return rows[t.Offset:end]
}

func (t *TreeList) MoveCursor(delta int) {
	rows := t.VisibleRows()
	if len(rows) == 0 {
		return
	}
	t.Cursor += delta
	t.normalizeCursor()
}

// PageCursor 按当前 Viewport（或回退 10 行）翻页。
func (t *TreeList) PageCursor(dir int) {
	if dir == 0 {
		return
	}
	step := t.Viewport
	if step <= 0 {
		step = 10
	}
	if dir < 0 {
		t.MoveCursor(-step)
		return
	}
	t.MoveCursor(step)
}

func (t *TreeList) currentRow() (TreeRow, bool) {
	rows := t.VisibleRows()
	if len(rows) == 0 || t.Cursor < 0 || t.Cursor >= len(rows) {
		return TreeRow{}, false
	}
	return rows[t.Cursor], true
}

func (t *TreeList) selectIndexByID() map[string]int {
	ids := t.selectableIDs()
	out := make(map[string]int, len(ids))
	for i, id := range ids {
		out[id] = i
	}
	return out
}

func collectSelectableIDsUnder(node *TreeNode, includeSelf bool) []string {
	if node == nil {
		return nil
	}
	var ids []string
	var walk func(*TreeNode, bool)
	walk = func(n *TreeNode, self bool) {
		if n == nil {
			return
		}
		if self && treeNodeSelectable(n) {
			ids = append(ids, n.ID)
		}
		for _, child := range n.Children {
			walk(child, true)
		}
	}
	walk(node, includeSelf)
	return ids
}

func (t *TreeList) ToggleSelectAtCursor() bool {
	row, ok := t.currentRow()
	if !ok || row.SelectIndex < 0 || row.SelectIndex >= len(t.Selected) {
		return false
	}
	if row.Node.SelectMode == TreeSelectBranch {
		idxMap := t.selectIndexByID()
		ids := collectSelectableIDsUnder(row.Node, true)
		indices := make([]int, 0, len(ids))
		for _, id := range ids {
			if i, ok := idxMap[id]; ok && i >= 0 && i < len(t.Selected) {
				indices = append(indices, i)
			}
		}
		if len(indices) == 0 {
			return false
		}
		allOn := true
		for _, i := range indices {
			if !t.Selected[i] {
				allOn = false
				break
			}
		}
		for _, i := range indices {
			t.Selected[i] = !allOn
		}
		return true
	}
	t.Selected[row.SelectIndex] = !t.Selected[row.SelectIndex]
	return true
}

// BranchCheckState 返回分支勾选展示：空 / 全选 / 部分。
func (t *TreeList) BranchCheckState(node *TreeNode) (all, any bool) {
	if node == nil {
		return false, false
	}
	idxMap := t.selectIndexByID()
	leafIDs := collectSelectableIDsUnder(node, false)
	// 若分支下没有可选子项，退回自身勾选位。
	if len(leafIDs) == 0 {
		if i, ok := idxMap[node.ID]; ok {
			on := t.IsSelected(i)
			return on, on
		}
		return false, false
	}
	all = true
	for _, id := range leafIDs {
		i, ok := idxMap[id]
		if !ok {
			continue
		}
		if t.IsSelected(i) {
			any = true
		} else {
			all = false
		}
	}
	if !any {
		all = false
	}
	return all, any
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

// DefaultExpandAll 展开所有有子节点的分支（用于 Remote 页初次加载）。
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
