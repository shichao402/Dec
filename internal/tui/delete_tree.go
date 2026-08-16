package tui

import (
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/app"
)

func buildDeleteTree(candidates []app.DeleteCandidate) []*TreeNode {
	decRemote := &TreeNode{ID: "delete-root:.dec", Label: "远端 · Dec (Git vault)", SelectMode: TreeSelectBranch}
	secRemote := &TreeNode{ID: "delete-root:secrets", Label: "远端 · Secrets (Bitwarden) · 将改远端、不碰本地", SelectMode: TreeSelectBranch}
	decLocal := &TreeNode{ID: "delete-root:local-dec", Label: "本地 · Dec cache · 只清本机，不写 vault", SelectMode: TreeSelectBranch}
	secLocal := &TreeNode{ID: "delete-root:local-secrets", Label: "本地 · Secrets · 只清本机，不写 Bitwarden", SelectMode: TreeSelectBranch}
	unfiledRoot := &TreeNode{
		ID:         "delete-root:unfiled",
		Label:      "无文件夹 · 非Dec管理 · 只读（请到 Bitwarden Web）",
		SelectMode: TreeSelectNone,
	}
	hasDecRemote, hasSecRemote, hasDecLocal, hasSecLocal, hasUnfiled := false, false, false, false, false

	targetGroups := make(map[string]*secretsGroupNode)
	getSecretsGroup := func(c app.DeleteCandidate, root *TreeNode) *TreeNode {
		key := secretsGroupKey(c)
		group, ok := targetGroups[key]
		if !ok {
			group = &secretsGroupNode{node: &TreeNode{
				ID:         "delete-secrets-group:" + key,
				SelectMode: TreeSelectBranch,
			}}
			targetGroups[key] = group
			root.Children = append(root.Children, group.node)
		}
		group.absorb(c)
		return group.node
	}

	for i, c := range candidates {
		if c.ReadOnly {
			hasUnfiled = true
			leaf := &TreeNode{
				ID:         fmt.Sprintf("readonly-%d", i),
				Label:      deleteLeafLabel(c),
				SelectMode: TreeSelectReadOnly,
				Payload:    i,
			}
			unfiledRoot.Children = append(unfiledRoot.Children, leaf)
			continue
		}
		leaf := &TreeNode{
			ID:         fmt.Sprintf("c%d", i),
			Label:      deleteLeafLabel(c),
			SelectMode: TreeSelectLeaf,
			Payload:    i,
		}
		local := c.Partition == app.PartitionLocal
		switch c.Kind {
		case app.DeleteKindSecret:
			if local {
				hasSecLocal = true
				group := getSecretsGroup(c, secLocal)
				insertTreePath(group, secretsParentSegments(c.SecretPath), leaf)
			} else {
				hasSecRemote = true
				group := getSecretsGroup(c, secRemote)
				insertTreePath(group, secretsParentSegments(c.SecretPath), leaf)
			}
		case app.DeleteKindSSHKey:
			if local {
				hasSecLocal = true
				group := getSecretsGroup(c, secLocal)
				insertTreePath(group, secretsParentSegments(c.SSHKeyName), leaf)
			} else {
				hasSecRemote = true
				group := getSecretsGroup(c, secRemote)
				insertTreePath(group, secretsParentSegments(c.SSHKeyName), leaf)
			}
		case app.DeleteKindBundle:
			hasDecRemote = true
			bundle := strings.TrimSpace(c.BundleName)
			if bundle == "" {
				bundle = strings.TrimSpace(c.TreeBranch)
			}
			insertTreePath(decRemote, []string{"cache", bundle}, leaf)
		case app.DeleteKindDecAsset:
			if local {
				hasDecLocal = true
				insertTreePath(decLocal, decCacheParentSegments(c.Vault, c.Type, c.Name), leaf)
			} else {
				hasDecRemote = true
				insertTreePath(decRemote, decCacheParentSegments(c.Vault, c.Type, c.Name), leaf)
			}
		default:
			hasDecRemote = true
			branch := strings.TrimSpace(c.TreeBranch)
			if branch == "" {
				branch = "other"
			}
			insertTreePath(decRemote, []string{"cache", branch}, leaf)
		}
	}

	for _, group := range targetGroups {
		group.node.Label = group.label()
		group.node.Payload = group.ref()
	}

	sortPathTreeChildren(decRemote.Children)
	sortPathTreeChildren(secRemote.Children)
	sortPathTreeChildren(decLocal.Children)
	sortPathTreeChildren(secLocal.Children)
	sortPathTreeChildren(unfiledRoot.Children)
	for _, group := range targetGroups {
		sortPathTreeChildren(group.node.Children)
	}

	var roots []*TreeNode
	if hasDecRemote {
		roots = append(roots, decRemote)
	}
	if hasSecRemote {
		roots = append(roots, secRemote)
	}
	if hasUnfiled {
		roots = append(roots, unfiledRoot)
	}
	if hasDecLocal {
		roots = append(roots, decLocal)
	}
	if hasSecLocal {
		roots = append(roots, secLocal)
	}
	return roots
}

// secretsFolderRef 挂在 folder 分组节点上，供 Remote 登记从光标就近反推归属。
type secretsFolderRef struct {
	Folder    string
	Title     string
	Partition app.RemotePartition
}

// secretsGroupNode 汇总同一 Bitwarden folder 下的展示信息。
// Secure Note 带 LocalRoot、SSH Key 落 ~/.ssh 没有 LocalRoot，
// 二者必须收敛到同一个 folder 节点，标题按「有本地映射优先」取。
type secretsGroupNode struct {
	node      *TreeNode
	title     string
	localRoot string
	folder    string
	partition app.RemotePartition
}

func (g *secretsGroupNode) absorb(c app.DeleteCandidate) {
	if g.title == "" {
		title := strings.TrimSpace(c.GroupTitle)
		if title == "" {
			title = strings.TrimSpace(c.SecretsBundle)
		}
		g.title = title
	}
	if g.localRoot == "" {
		g.localRoot = strings.TrimSpace(c.LocalRoot)
	}
	if g.folder == "" {
		g.folder = strings.TrimSpace(c.SecretsBundle)
	}
	if g.partition == "" {
		g.partition = c.Partition
	}
}

func (g *secretsGroupNode) ref() secretsFolderRef {
	return secretsFolderRef{Folder: g.folder, Title: g.title, Partition: g.partition}
}

func (g *secretsGroupNode) label() string {
	if g.localRoot != "" {
		if g.title != "" {
			return fmt.Sprintf("%s → %s", g.title, g.localRoot)
		}
		return g.localRoot
	}
	return g.title
}

// secretsGroupKey 以 Bitwarden folder 为分组身份；folder 缺失才退回本地根 / 标题。
func secretsGroupKey(c app.DeleteCandidate) string {
	id := strings.TrimSpace(c.SecretsBundle)
	if id == "" {
		id = strings.TrimSpace(c.LocalRoot)
	}
	if id == "" {
		id = strings.TrimSpace(c.GroupTitle)
	}
	return string(c.Partition) + "\x00" + id
}

func deleteLeafLabel(c app.DeleteCandidate) string {
	switch c.Kind {
	case app.DeleteKindBundle:
		return "[bundle] " + strings.TrimPrefix(c.Label, "[bundle] ")
	case app.DeleteKindSecret:
		if c.ReadOnly {
			typ := strings.TrimSpace(c.Type)
			if typ == "" {
				typ = "item"
			}
			return fmt.Sprintf("[%s] %s", typ, strings.TrimSpace(c.Name))
		}
		leaf := secretsLeafName(c.SecretPath)
		if c.Orphan {
			leaf += " · 仅远端"
		}
		if c.Unmanaged {
			leaf += " · 非Dec管理"
		}
		return leaf
	case app.DeleteKindSSHKey:
		leaf := "[ssh] " + secretsLeafName(c.SSHKeyName)
		if c.Orphan {
			leaf += " · 仅远端"
		}
		if c.Unmanaged {
			leaf += " · 非Dec管理"
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
		// 「无文件夹」默认折叠
		m.deleteTree.ensureExpanded()
		m.deleteTree.Expanded["delete-root:unfiled"] = false
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
