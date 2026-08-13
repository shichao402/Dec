package tui

import (
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/app"
)

func buildDeleteTree(candidates []app.DeleteCandidate) []*TreeNode {
	decRoot := &TreeNode{ID: "delete-root:.dec", Label: "Dec (Git vault)", SelectMode: TreeSelectBranch}
	secRoot := &TreeNode{ID: "delete-root:secrets", Label: "Secrets (Bitwarden)", SelectMode: TreeSelectBranch}
	hasDec, hasSec := false, false

	targetGroups := make(map[string]*TreeNode)
	getSecretsGroup := func(c app.DeleteCandidate) *TreeNode {
		key := strings.TrimSpace(c.LocalRoot)
		if key == "" {
			key = strings.TrimSpace(c.GroupTitle)
		}
		if key == "" {
			key = strings.TrimSpace(c.SecretsBundle)
		}
		if node, ok := targetGroups[key]; ok {
			return node
		}
		label := deleteSyncTargetGroupLabel(c)
		node := &TreeNode{
			ID:         "delete-secrets-group:" + key,
			Label:      label,
			SelectMode: TreeSelectBranch,
		}
		targetGroups[key] = node
		secRoot.Children = append(secRoot.Children, node)
		return node
	}

	for i, c := range candidates {
		leaf := &TreeNode{
			ID:         fmt.Sprintf("c%d", i),
			Label:      deleteLeafLabel(c),
			SelectMode: TreeSelectLeaf,
			Payload:    i,
		}
		switch c.Kind {
		case app.DeleteKindSecret:
			hasSec = true
			group := getSecretsGroup(c)
			insertTreePath(group, secretsParentSegments(c.SecretPath), leaf)
		case app.DeleteKindSSHKey:
			hasSec = true
			group := getSecretsGroup(c)
			insertTreePath(group, []string{"SSH · machine"}, leaf)
		case app.DeleteKindBundle:
			hasDec = true
			bundle := strings.TrimSpace(c.BundleName)
			if bundle == "" {
				bundle = strings.TrimSpace(c.TreeBranch)
			}
			insertTreePath(decRoot, []string{"cache", bundle}, leaf)
		case app.DeleteKindDecAsset:
			hasDec = true
			insertTreePath(decRoot, decCacheParentSegments(c.Vault, c.Type, c.Name), leaf)
		default:
			hasDec = true
			branch := strings.TrimSpace(c.TreeBranch)
			if branch == "" {
				branch = "other"
			}
			insertTreePath(decRoot, []string{"cache", branch}, leaf)
		}
	}

	sortPathTreeChildren(decRoot.Children)
	sortPathTreeChildren(secRoot.Children)
	for _, group := range targetGroups {
		sortPathTreeChildren(group.Children)
	}

	var roots []*TreeNode
	if hasDec {
		roots = append(roots, decRoot)
	}
	if hasSec {
		roots = append(roots, secRoot)
	}
	return roots
}

func deleteSyncTargetGroupLabel(c app.DeleteCandidate) string {
	title := strings.TrimSpace(c.GroupTitle)
	if title == "" {
		title = strings.TrimSpace(c.SecretsBundle)
	}
	root := strings.TrimSpace(c.LocalRoot)
	if root != "" {
		if title != "" {
			return fmt.Sprintf("%s → %s", title, root)
		}
		return root
	}
	return title
}

func deleteLeafLabel(c app.DeleteCandidate) string {
	switch c.Kind {
	case app.DeleteKindBundle:
		return "[bundle] " + strings.TrimPrefix(c.Label, "[bundle] ")
	case app.DeleteKindSecret:
		leaf := secretsLeafName(c.SecretPath)
		if c.Orphan {
			leaf += " · 仅远端"
		}
		return leaf
	case app.DeleteKindSSHKey:
		leaf := "[ssh] " + strings.TrimSpace(c.SSHKeyName)
		if c.Orphan {
			leaf += " · 仅远端"
		}
		return leaf
	case app.DeleteKindDecAsset:
		return decCacheLeafName(c.Type, c.Name)
	default:
		return c.Label
	}
}

func (m *model) rebuildDeleteTree() {
	visible := m.visibleDeleteCandidates()
	roots := buildDeleteTree(visible)
	prevSelected := make(map[string]bool)
	for i, id := range m.deleteTree.selectableIDs() {
		if i < len(m.deleteTree.Selected) && m.deleteTree.Selected[i] {
			prevSelected[id] = true
		}
	}
	m.deleteTree.SetFilter(m.deleteFilter, roots)
	for i, id := range m.deleteTree.selectableIDs() {
		if prevSelected[id] {
			m.deleteTree.Selected[i] = true
		}
	}
	if len(m.deleteTree.Expanded) == 0 {
		m.deleteTree.DefaultExpandAll()
	}
	m.deleteTree.FocusFirstSelectable()
	m.deleteTree.normalizeCursor()
}

func (m model) selectedDeleteItems() []app.DeleteSelectionItem {
	visible := m.visibleDeleteCandidates()
	items := make([]app.DeleteSelectionItem, 0)
	selectIdx := 0
	var walk func(nodes []*TreeNode)
	walk = func(nodes []*TreeNode) {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			if treeNodeSelectable(n) {
				// 目录分支勾选只驱动级联，真正删除项只来自叶子 Payload。
				if n.SelectMode == TreeSelectLeaf && m.deleteTree.IsSelected(selectIdx) {
					idx, ok := n.Payload.(int)
					if ok && idx >= 0 && idx < len(visible) {
						items = append(items, selectionFromCandidate(visible[idx]))
					}
				}
				selectIdx++
			}
			walk(n.Children)
		}
	}
	walk(m.deleteTree.Roots)
	return items
}

func renderDeleteTreeLine(row TreeRow, cursor int, tree *TreeList, focused bool) string {
	indent := strings.Repeat("  ", row.Depth)
	marker := " "
	if focused {
		marker = ">"
	}
	expand := "  "
	if treeNodeExpandable(row.Node) {
		if tree.Expanded[row.Node.ID] {
			expand = "▾ "
		} else {
			expand = "▸ "
		}
	} else if row.Depth > 0 {
		expand = "↳ "
	}
	check := "   "
	if row.SelectIndex >= 0 {
		if row.Node.SelectMode == TreeSelectBranch {
			all, any := tree.BranchCheckState(row.Node)
			switch {
			case all:
				check = "[x]"
			case any:
				check = "[-]"
			default:
				check = "[ ]"
			}
		} else if tree.IsSelected(row.SelectIndex) {
			check = "[x]"
		} else {
			check = "[ ]"
		}
	}
	return fmt.Sprintf("%s %s %s%s%s", marker, check, indent, expand, row.Node.Label)
}
