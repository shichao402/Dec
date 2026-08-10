package tui

import (
	"strings"
	"testing"
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

func TestRenderAssetList_UserEnabledAccentTag(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.assets = assetsStateWithBundle()
	m.assets.Bundles[0].UserEnabled = true
	m.refreshAssetTree()

	list := m.renderAssetList(20)
	if !strings.Contains(list, "user") {
		t.Fatalf("user 级启用应带 user 标签:\n%s", list)
	}
	page := m.renderBundlesPage(100, 40)
	if !strings.Contains(page, "项目已启用") {
		t.Fatalf("摘要应标明项目启用计数:\n%s", page)
	}
	if !strings.Contains(page, "本机启用") {
		t.Fatalf("摘要应提示本机启用数:\n%s", page)
	}
}
