package tui

import (
	"fmt"
	"strings"

	"github.com/shichao402/Dec/pkg/app"
)

func buildDeleteTree(candidates []app.DeleteCandidate) []*TreeNode {
	decRoot := &TreeNode{ID: "delete-root:.dec", Label: ".dec", SelectMode: TreeSelectNone}
	// secrets 分支按 Bitwarden folder 分组，而不是按某个本地目录：
	// 落地路径散在项目根，folder 才是唯一的归属维度。
	secRoot := &TreeNode{ID: "delete-root:secrets", Label: "secrets (Bitwarden)", SelectMode: TreeSelectNone}
	hasDec, hasSec := false, false

	for i, c := range candidates {
		leaf := &TreeNode{
			ID:         fmt.Sprintf("c%d", i),
			Label:      deleteLeafLabel(c),
			SelectMode: TreeSelectLeaf,
			Payload:    i,
		}
		// 按 Kind 分派，不看 TreeRoot 字符串：字符串对不上就会把一条 secret
		// 静默塞进 .dec 树，Kind 是唯一不会漂的归属依据。
		switch c.Kind {
		case app.DeleteKindSecret:
			hasSec = true
			insertTreePath(secRoot, secretsParentSegments(c.SecretsBundle, c.SecretPath), leaf)
		case app.DeleteKindSSHKey:
			hasSec = true
			// SSH Key 不套用 Secure Note 路径树，直接挂在 folder 下。
			folder := strings.TrimSpace(c.SecretsBundle)
			if folder == "" {
				folder = "ssh"
			}
			insertTreePath(secRoot, []string{folder}, leaf)
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
		if tree.IsSelected(row.SelectIndex) {
			check = "[x]"
		} else {
			check = "[ ]"
		}
	}
	return fmt.Sprintf("%s %s %s%s%s", marker, check, indent, expand, row.Node.Label)
}
