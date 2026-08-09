package tui

import (
	"path/filepath"
	"strings"
)

// insertTreePath 在 root 下沿 segments 创建目录节点，并在末端挂上 leaf。
func insertTreePath(root *TreeNode, segments []string, leaf *TreeNode) {
	if root == nil || leaf == nil {
		return
	}
	parent := root
	prefix := root.ID
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" || seg == "." {
			continue
		}
		id := prefix + "/" + seg
		branch := findTreeChildByID(parent, id)
		if branch == nil {
			branch = &TreeNode{
				ID:         id,
				Label:      seg,
				SelectMode: TreeSelectBranch, // 目录可勾选：选中=全选子项
			}
			parent.Children = append(parent.Children, branch)
		}
		parent = branch
		prefix = id
	}
	leaf.ID = prefix + "/leaf:" + leaf.ID
	parent.Children = append(parent.Children, leaf)
}

func findTreeChildByID(parent *TreeNode, id string) *TreeNode {
	for _, child := range parent.Children {
		if child != nil && child.ID == id {
			return child
		}
	}
	return nil
}

func sortPathTreeChildren(nodes []*TreeNode) {
	if len(nodes) == 0 {
		return
	}
	// 目录在前、可选项在后；同层按标签排序。
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			if treeChildLess(nodes[i], nodes[j]) {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
		}
		sortPathTreeChildren(nodes[i].Children)
	}
}

func treeChildLess(a, b *TreeNode) bool {
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	// 有子节点的目录排在叶子前，便于先勾选父目录再看细节。
	aBranch := len(a.Children) > 0
	bBranch := len(b.Children) > 0
	if aBranch != bBranch {
		return aBranch
	}
	return a.Label < b.Label
}

func assetTypeSubDir(itemType string) string {
	switch itemType {
	case "skill":
		return "skills"
	case "command":
		return "commands"
	case "rule":
		return "rules"
	case "mcp":
		return "mcp"
	default:
		return itemType
	}
}

func decCacheParentSegments(vault, itemType, name string) []string {
	sub := assetTypeSubDir(itemType)
	if sub == "" {
		return []string{"cache", vault}
	}
	return []string{"cache", vault, sub}
}

func decCacheLeafName(itemType, name string) string {
	switch itemType {
	case "rule":
		return name + ".mdc"
	case "mcp":
		return name + ".json"
	default:
		return name
	}
}

func decCachePathSegments(vault, itemType, name string) []string {
	segs := decCacheParentSegments(vault, itemType, name)
	return append(segs, decCacheLeafName(itemType, name))
}

// secretsParentSegments 按 Note 相对同步根的路径逐层展开目录。
func secretsParentSegments(noteRel string) []string {
	rel := strings.TrimSpace(strings.ReplaceAll(noteRel, "\\", "/"))
	if rel == "" {
		return nil
	}
	parts := strings.Split(rel, "/")
	if len(parts) <= 1 {
		return nil
	}
	return parts[:len(parts)-1]
}

func secretsLeafName(landingPath string) string {
	rel := strings.TrimSpace(filepath.ToSlash(landingPath))
	if rel == "" {
		return rel
	}
	parts := strings.Split(rel, "/")
	return parts[len(parts)-1]
}

func memberPathSegments(mbType, name string) []string {
	sub := assetTypeSubDir(mbType)
	switch mbType {
	case "rule":
		return []string{sub, name + ".mdc"}
	case "mcp":
		return []string{sub, name + ".json"}
	default:
		return []string{sub, name}
	}
}
