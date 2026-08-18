package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/app"
)

type assetTreePayload struct {
	kind          assetRowKind
	bundleIndex   int
	memberIndex   int
	bundleEnabled bool
	// otherPlane 让渲染层画出「不可勾选」的复选框，无需回查 AssetSelectionState。
	otherPlane bool
}

func assetBundleNodeID(name string) string {
	return "bundle:" + name
}

func (m *model) refreshAssetTree() {
	if m.assets == nil {
		m.assetTree.Roots = nil
		return
	}
	m.assetTree.ensureExpanded()
	m.assetTree.Roots = m.buildAssetTreeRoots()
	m.assetTree.Filter = m.assetFilter
	m.assetTree.normalizeCursor()
}

func (m model) buildAssetTreeRoots() []*TreeNode {
	if m.assets == nil {
		return nil
	}
	filter := strings.ToLower(strings.TrimSpace(m.assetFilter))
	roots := make([]*TreeNode, 0, len(m.assets.Bundles))

	for i, bo := range m.assets.Bundles {
		if filter != "" {
			haystack := strings.ToLower(strings.Join([]string{bo.Name, bo.Vault, bo.Description}, " "))
			if !strings.Contains(haystack, filter) {
				continue
			}
		}
		enabled := m.bundleSelected(bo.Name)
		nodeID := assetBundleNodeID(bo.Name)
		node := &TreeNode{
			ID:         nodeID,
			Label:      formatAssetBundleLabel(bo),
			SelectMode: TreeSelectBranch,
			Payload: assetTreePayload{
				kind:          assetRowBundle,
				bundleIndex:   i,
				bundleEnabled: enabled,
				otherPlane:    bo.OtherPlane,
			},
		}
		typeGroups := make(map[string][]int)
		for mi, mb := range bo.Members {
			sub := assetTypeSubDir(mb.Type)
			typeGroups[sub] = append(typeGroups[sub], mi)
		}
		subs := make([]string, 0, len(typeGroups))
		for sub := range typeGroups {
			subs = append(subs, sub)
		}
		sort.Strings(subs)
		for _, sub := range subs {
			indices := typeGroups[sub]
			typeID := nodeID + "/" + sub
			typeNode := &TreeNode{
				ID:         typeID,
				Label:      sub,
				SelectMode: TreeSelectNone,
			}
			for _, mi := range indices {
				mb := bo.Members[mi]
				typeNode.Children = append(typeNode.Children, &TreeNode{
					ID:         fmt.Sprintf("%s:member:%d", typeID, mi),
					Label:      memberLeafLabel(mb.Type, mb.Name),
					SelectMode: TreeSelectReadOnly,
					Payload: assetTreePayload{
						kind:        assetRowBundleMember,
						bundleIndex: i,
						memberIndex: mi,
					},
				})
			}
			sortPathTreeChildren(typeNode.Children)
			node.Children = append(node.Children, typeNode)
		}
		sortPathTreeChildren(node.Children)
		roots = append(roots, node)
	}

	return roots
}

func memberLeafLabel(itemType, name string) string {
	segs := memberPathSegments(itemType, name)
	if len(segs) == 0 {
		return name
	}
	return segs[len(segs)-1]
}

// secretsOnlyBundleHint 描述「vault 尚无 manifest」的候选到底处在哪种状态。
// 一律写「仓库未登记」会让本机残留记录看起来像远端已有 secrets 等着被启用。
func secretsOnlyBundleHint(bo app.AssetBundleOption) string {
	switch {
	case bo.RemoteMissing:
		return "远端无内容 · 本机残留"
	case bo.RemoteUnverified:
		return "仓库未登记 · 未核对远端"
	default:
		return "仓库未登记"
	}
}

func formatAssetBundleLabel(bo app.AssetBundleOption) string {
	if bo.OtherPlane {
		return fmt.Sprintf("%s · 属于项目平面", bo.Name)
	}
	if bo.SecretsOnly {
		return fmt.Sprintf("%s · %s", bo.Name, secretsOnlyBundleHint(bo))
	}
	label := bo.Name
	if bo.Name != bo.Vault {
		label = fmt.Sprintf("%s (%s)", bo.Name, bo.Vault)
	}
	return fmt.Sprintf("%s · %d 个成员", label, len(bo.Members))
}

func (m model) visibleAssetRows() []assetRow {
	mm := m
	mm.refreshAssetTree()
	rows := mm.assetTree.VisibleRows()
	out := make([]assetRow, 0, len(rows))
	for _, tr := range rows {
		p, ok := tr.Node.Payload.(assetTreePayload)
		if !ok {
			continue
		}
		out = append(out, assetRow{
			kind:          p.kind,
			bundleIndex:   p.bundleIndex,
			memberIndex:   p.memberIndex,
			bundleEnabled: p.bundleEnabled,
		})
	}
	return out
}

func (m model) assetTreeRowAtCursor() (TreeRow, bool) {
	mm := m
	mm.refreshAssetTree()
	return mm.assetTree.currentRow()
}

func (m model) assetPayloadAtCursor() (assetTreePayload, bool) {
	row, ok := m.assetTreeRowAtCursor()
	if !ok {
		return assetTreePayload{}, false
	}
	p, ok := row.Node.Payload.(assetTreePayload)
	return p, ok
}

func (m model) assetTreeVisibleCount() int {
	mm := m
	mm.refreshAssetTree()
	return len(mm.assetTree.VisibleRows())
}

func renderAssetTreeLine(row TreeRow, tree *TreeList, marker string, bundleEnabled bool) string {
	indent := strings.Repeat("  ", row.Depth)
	if p, ok := row.Node.Payload.(assetTreePayload); ok {
		switch p.kind {
		case assetRowBundle:
			checked := " "
			if bundleEnabled {
				checked = "x"
			}
			if p.otherPlane {
				checked = "-"
			}
			arrow := "▸"
			if tree.Expanded[row.Node.ID] {
				arrow = "▾"
			}
			return fmt.Sprintf("%s [%s] %s %s%s", marker, checked, arrow, indent, row.Node.Label)
		case assetRowBundleMember:
			return fmt.Sprintf("%s %s↳ %s", marker, indent, row.Node.Label)
		}
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
	return fmt.Sprintf("%s %s%s", marker, prefix, row.Node.Label)
}
