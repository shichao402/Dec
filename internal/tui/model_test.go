package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/pkg/app"
)

func TestModelViewRendersHomeOverview(t *testing.T) {
	m := newModel("/tmp/dec-project")
	m.width = 120
	m.height = 36

	updated, _ := m.Update(overviewLoadedMsg{overview: &app.ProjectOverview{
		ProjectRoot:        "/tmp/dec-project",
		RepoConnected:      true,
		RepoRemoteURL:      "git@github.com:demo/dec.git",
		ProjectConfigPath:  "/tmp/dec-project/.dec/config.yaml",
		ProjectConfigReady: true,
		VarsPath:           "/tmp/dec-project/.dec/vars.yaml",
		VarsFileReady:      true,
		AvailableCount:     5,
		EnabledCount:       2,
		IDEs:               []string{"codex", "cursor"},
		Editor:             "code --wait",
	}})
	m = updated.(model)

	view := m.View()
	checks := []string{
		"Dec Shell",
		"Home",
		"Assets",
		"仓库:",
		"git@github.com:demo/dec.git",
		"已启用资产: 2",
		"默认 IDE: codex, cursor",
		"编辑器: code --wait",
		"Logs",
	}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Fatalf("View() 缺少 %q:\n%s", check, view)
		}
	}
}

func TestModelAssetsPageRendersSelectionState(t *testing.T) {
	m := newModel("/tmp/dec-project")
	m.pageIndex = 1
	m.width = 110
	m.height = 32
	m.assets = &app.AssetSelectionState{
		ExistingConfig: true,
		ConfigPath:     "/tmp/dec-project/.dec/config.yaml",
		VarsPath:       "/tmp/dec-project/.dec/vars.yaml",
		Items: []app.AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default", Enabled: true},
			{Name: "cli-release-rules", Type: "rule", Vault: "cli", Enabled: false},
		},
	}
	m.normalizeAssetCursor()

	view := m.View()
	checks := []string{
		"Asset List",
		"Details",
		"[x] default / skill / project-workflow",
		"[ ] cli / rule / cli-release-rules",
		"快捷键：j/k 移动",
	}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Fatalf("Assets View() 缺少 %q:\n%s", check, view)
		}
	}
}

func TestModelToggleCurrentAssetMarksDirty(t *testing.T) {
	m := newModel("/tmp/dec-project")
	m.pageIndex = 1
	m.assets = &app.AssetSelectionState{
		Items: []app.AssetSelectionItem{{Name: "project-workflow", Type: "skill", Vault: "default"}},
	}
	m.normalizeAssetCursor()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if !m.assets.Items[0].Enabled {
		t.Fatal("space 应切换当前资产为 enabled")
	}
	if !m.assetsDirty {
		t.Fatal("切换资产后应标记为 dirty")
	}
}

func TestModelFilterInputNarrowsAssets(t *testing.T) {
	m := newModel("/tmp/dec-project")
	m.pageIndex = 1
	m.assets = &app.AssetSelectionState{
		Items: []app.AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default"},
			{Name: "cli-release-rules", Type: "rule", Vault: "cli"},
		},
	}
	m.normalizeAssetCursor()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(model)
	if !m.assetFilterInput {
		t.Fatal("/ 应进入筛选输入状态")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c', 'l', 'i'}})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	visible := m.filteredAssetIndices()
	if len(visible) != 1 {
		t.Fatalf("筛选后可见资产数 = %d, 期望 1", len(visible))
	}
	if got := m.assets.Items[visible[0]].Name; got != "cli-release-rules" {
		t.Fatalf("筛选命中资产 = %q, 期望 %q", got, "cli-release-rules")
	}
}

func TestModelAssetsPageDoesNotLeavePageWithoutVisibleAssets(t *testing.T) {
	m := newModel("/tmp/dec-project")
	m.pageIndex = 1
	m.assets = &app.AssetSelectionState{
		Items: []app.AssetSelectionItem{{Name: "project-workflow", Type: "skill", Vault: "default"}},
	}
	m.assetFilter = "missing"
	m.normalizeAssetCursor()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.pageIndex != 1 {
		t.Fatalf("无可见资产时按 down 不应切出 Assets 页, pageIndex = %d", m.pageIndex)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(model)
	if m.pageIndex != 1 {
		t.Fatalf("无可见资产时按 up 不应切出 Assets 页, pageIndex = %d", m.pageIndex)
	}
}

func TestSuggestNextAction(t *testing.T) {
	if got := suggestNextAction(&app.ProjectOverview{}); !strings.Contains(got, "dec config repo") {
		t.Fatalf("未连接仓库时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true}); !strings.Contains(got, "Assets 页") {
		t.Fatalf("未初始化项目时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true, ProjectConfigReady: true, EnabledCount: 0}); !strings.Contains(got, "Assets 页") {
		t.Fatalf("无已启用资产时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true, ProjectConfigReady: true, EnabledCount: 2}); !strings.Contains(got, "dec pull") {
		t.Fatalf("项目就绪时建议动作错误: %q", got)
	}
}
