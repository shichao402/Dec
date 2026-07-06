package tui

import (
	"fmt"
	"strings"

	"github.com/shichao402/Dec/pkg/app"
)

func buildDeleteTree(candidates []app.DeleteCandidate) []*TreeNode {
	decRoot := &TreeNode{ID: "delete-root:.dec", Label: ".dec", SelectMode: TreeSelectNone}
	secRoot := &TreeNode{ID: "delete-root:.secrets", Label: ".secrets", SelectMode: TreeSelectNone}
	hasDec, hasSec := false, false

	for i, c := range candidates {
		leaf := &TreeNode{
			ID:         fmt.Sprintf("c%d", i),
			Label:      deleteLeafLabel(c),
			SelectMode: TreeSelectLeaf,
			Payload:    i,
		}
		root := strings.TrimSpace(c.TreeRoot)
		switch root {
		case ".secrets":
			hasSec = true
			rel := strings.TrimSpace(c.RelWithinBundle)
			if rel == "" {
				rel = strings.TrimSpace(c.SecretPath)
			}
			insertTreePath(secRoot, secretsParentSegments(c.SecretsBundle, rel), leaf)
		default:
			hasDec = true
			switch c.Kind {
			case app.DeleteKindBundle:
				bundle := strings.TrimSpace(c.BundleName)
				if bundle == "" {
					bundle = strings.TrimSpace(c.TreeBranch)
				}
				insertTreePath(decRoot, []string{"cache", bundle}, leaf)
			case app.DeleteKindDecAsset:
				insertTreePath(decRoot, decCacheParentSegments(c.Vault, c.Type, c.Name), leaf)
			default:
				branch := strings.TrimSpace(c.TreeBranch)
				if branch == "" {
					branch = "other"
				}
				insertTreePath(decRoot, []string{"cache", branch}, leaf)
			}
		}
	}

	sortPathTreeChildren(decRoot.Children)
	sortPathTreeChildren(secRoot.Children)

	var roots []*TreeNode
	if hasDec {
		roots = append(roots, decRoot)
	}
	if hasSec {
		roots = append(roots, secRoot)
	}
	return roots
}

func deleteLeafLabel(c app.DeleteCandidate) string {
	switch c.Kind {
	case app.DeleteKindBundle:
		return "[bundle] " + strings.TrimPrefix(c.Label, "[bundle] ")
	case app.DeleteKindSecret:
		rel := strings.TrimSpace(c.RelWithinBundle)
		if rel == "" {
			rel = strings.TrimSpace(c.SecretPath)
		}
		return secretsLeafName(rel)
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
				if m.deleteTree.IsSelected(selectIdx) {
					idx, _ := n.Payload.(int)
					if idx >= 0 && idx < len(visible) {
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
	prefix := indent
	if treeNodeExpandable(row.Node) {
		if tree.Expanded[row.Node.ID] {
			prefix += "▾ "
		} else {
			prefix += "▸ "
		}
	} else if row.Depth > 0 {
		prefix += "↳ "
	}
	check := "   "
	if row.SelectIndex >= 0 {
		if tree.IsSelected(row.SelectIndex) {
			check = "[x]"
		} else {
			check = "[ ]"
		}
	}
	return fmt.Sprintf(" %s %s %s%s", marker, check, prefix, row.Node.Label)
}
