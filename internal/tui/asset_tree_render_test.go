package tui

import (
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/app"
)

func TestRenderAssetList_SelectedRowNotDuplicated(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.assets = assetsStateWithBundle()
	m.refreshAssetTree()
	m.assetTree.Cursor = 0

	list := m.renderAssetList(20)
	count := strings.Count(list, "▸ vikunja (default)")
	if count != 1 {
		t.Fatalf("vikunja bundle 行应只出现 1 次, 实际 %d 次:\n%s", count, list)
	}
	if strings.Count(list, ">") != 1 {
		t.Fatalf("光标标记 > 应只出现 1 次:\n%s", list)
	}
}

func TestRenderAssetList_DoesNotRenderCrossPlaneUserTag(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.assets = assetsStateWithBundle()
	m.refreshAssetTree()

	list := m.renderAssetList(20)
	if strings.Contains(list, "user") {
		t.Fatalf("Bundles 页不应渲染跨平面 user 标签:\n%s", list)
	}
	page := m.renderBundlesPage(100, 40)
	if !strings.Contains(page, "项目已启用") {
		t.Fatalf("摘要应标明项目启用计数:\n%s", page)
	}
	if strings.Contains(page, "本机启用") || strings.Contains(page, "pull 为并集") {
		t.Fatalf("摘要不应再提示跨平面并集:\n%s", page)
	}
}

func TestFormatAssetBundleLabel_IncludesSecretMembers(t *testing.T) {
	got := formatAssetBundleLabel(app.AssetBundleOption{
		Name:  "agents-board",
		Vault: "agents-board",
		Members: []app.AssetSelectionItem{
			{Name: "board", Type: "skill", Vault: "agents-board"},
			{Name: ".env/foo.env", Type: app.AssetMemberTypeSecret, Vault: "agents-board"},
			{Name: ".sshkey/deploy", Type: app.AssetMemberTypeSecret, Vault: "agents-board"},
		},
	})
	if got != "agents-board · 3 个成员" {
		t.Fatalf("label = %q", got)
	}

	// 项目模型的行只说角色与资产数；四象限计数留给详情区，避免行内出现读不出含义的四个数字。
	project := formatAssetBundleLabel(app.AssetBundleOption{
		Name:      "agents-board",
		Vault:     "agents-board",
		Model:     "p",
		Quadrants: map[string]int{"public/global": 2},
		Members:   []app.AssetSelectionItem{{Name: "board", Type: "skill", Vault: "agents-board"}},
	})
	if project != "agents-board · 可引用 · 1 个资产" {
		t.Fatalf("project label = %q", project)
	}

	secretsOnly := formatAssetBundleLabel(app.AssetBundleOption{
		Name:        "pkv",
		SecretsOnly: true,
		Members:     []app.AssetSelectionItem{{Name: ".env/pkv.env", Type: app.AssetMemberTypeSecret, Vault: "pkv"}},
	})
	if secretsOnly != "pkv · 仓库未登记 · 1 个成员" {
		t.Fatalf("secrets-only label = %q", secretsOnly)
	}
}

func TestBuildAssetTreeRoots_ExpandsSecretPaths(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.assets = &app.AssetSelectionState{
		Bundles: []app.AssetBundleOption{
			{
				Name:  "agents-board",
				Vault: "agents-board",
				Members: []app.AssetSelectionItem{
					{Name: "board", Type: "skill", Vault: "agents-board"},
					{Name: ".env/foo.env", Type: app.AssetMemberTypeSecret, Vault: "agents-board"},
					{Name: ".sshkey/deploy", Type: app.AssetMemberTypeSecret, Vault: "agents-board"},
				},
			},
		},
	}
	roots := m.buildAssetTreeRoots()
	if len(roots) != 1 {
		t.Fatalf("roots = %d", len(roots))
	}
	labels := collectTreeLabels(roots[0])
	for _, want := range []string{"agents-board · 3 个成员", "skills", "board", ".env", "foo.env", ".sshkey", "deploy"} {
		if !containsLabel(labels, want) {
			t.Fatalf("树缺少 %q: %v", want, labels)
		}
	}
}

func collectTreeLabels(node *TreeNode) []string {
	if node == nil {
		return nil
	}
	out := []string{node.Label}
	for _, child := range node.Children {
		out = append(out, collectTreeLabels(child)...)
	}
	return out
}

func containsLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
