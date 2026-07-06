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

	list := m.renderAssetList(nil)
	count := strings.Count(list, "▸ vikunja (default)")
	if count != 1 {
		t.Fatalf("vikunja bundle 行应只出现 1 次, 实际 %d 次:\n%s", count, list)
	}
	if strings.Count(list, ">") != 1 {
		t.Fatalf("光标标记 > 应只出现 1 次:\n%s", list)
	}
}
