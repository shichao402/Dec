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

func TestModelRunPageRendersExecutionState(t *testing.T) {
	m := newModel("/tmp/dec-project")
	m.pageIndex = 3
	m.width = 120
	m.height = 32
	m.runProgress = &app.Progress{Phase: "pull", Current: 1, Total: 2}
	m.runResult = &app.PullProjectAssetsResult{
		RequestedCount: 2,
		PulledCount:    1,
		FailedCount:    1,
		EffectiveIDEs:  []string{"cursor"},
		VersionCommit:  "abc123",
	}
	m.runEvents = []string{"开始拉取", "完成汇总"}

	view := m.View()
	checks := []string{
		"Run",
		"状态:",
		"阶段: pull (1/2)",
		"结果: 请求 2 | 成功 1 | 失败 1",
		"IDE: cursor",
		"Commit: abc123",
		"Execution Log",
		"开始拉取",
	}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Fatalf("Run View() 缺少 %q:\n%s", check, view)
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

func TestModelRunPageHotkeysStartPull(t *testing.T) {
	keys := []struct {
		name string
		msg  tea.KeyMsg
	}{
		{name: "p", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}},
		{name: "s", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}},
	}

	for _, tc := range keys {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel("/tmp/dec-project")
			m.pageIndex = 3

			updated, cmd := m.Update(tc.msg)
			m = updated.(model)
			if !m.runningPull {
				t.Fatal("Run 页触发 pull 后应进入 running 状态")
			}
			if m.runStream == nil {
				t.Fatal("Run 页触发 pull 后应创建消息流")
			}
			if cmd == nil {
				t.Fatal("Run 页触发 pull 后应返回执行命令")
			}
			if summary := m.currentSummary(); summary != "Pull running" {
				t.Fatalf("currentSummary() = %q, 期望 %q", summary, "Pull running")
			}
		})
	}
}

func TestModelRunPageProcessesStreamedEventsAndSchedulesRefresh(t *testing.T) {
	m := newModel("/tmp/dec-project")
	m.pageIndex = 3
	m.runningPull = true
	stream := make(chan tea.Msg, 1)
	m.runStream = stream
	stream <- runCompletedMsg{result: &app.PullProjectAssetsResult{RequestedCount: 1, PulledCount: 1}}
	close(stream)

	updated, waitCmd := m.Update(runEventMsg{event: app.OperationEvent{
		Message:  "开始拉取\n完成汇总",
		Progress: &app.Progress{Phase: "pull", Current: 1, Total: 1},
	}})
	m = updated.(model)
	if m.runProgress == nil || m.runProgress.Current != 1 || m.runProgress.Total != 1 {
		t.Fatalf("runProgress = %#v, 期望 1/1", m.runProgress)
	}
	if len(m.runEvents) != 2 || m.runEvents[0] != "开始拉取" || m.runEvents[1] != "完成汇总" {
		t.Fatalf("runEvents = %#v, 期望拆分后的两条日志", m.runEvents)
	}
	if waitCmd == nil {
		t.Fatal("处理 runEventMsg 时应继续等待后续消息")
	}

	msg := waitCmd()
	completed, ok := msg.(runCompletedMsg)
	if !ok {
		t.Fatalf("waitRunMsg 返回 = %T, 期望 runCompletedMsg", msg)
	}

	updated, refreshCmd := m.Update(completed)
	m = updated.(model)
	if m.runningPull {
		t.Fatal("runCompletedMsg 后应退出 running 状态")
	}
	if m.runResult == nil || m.runResult.PulledCount != 1 {
		t.Fatalf("runResult = %#v, 期望 pulled=1", m.runResult)
	}
	if refreshCmd == nil {
		t.Fatal("成功完成 pull 后应触发刷新命令")
	}

	batchMsg, ok := refreshCmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("refreshCmd() = %T, 期望 tea.BatchMsg", refreshCmd())
	}
	if len(batchMsg) != 3 {
		t.Fatalf("BatchMsg 长度 = %d, 期望 3", len(batchMsg))
	}
}

func TestModelSettingsPageRendersGlobalSettings(t *testing.T) {
	m := newModel("/tmp/dec-project")
	m.pageIndex = 4
	m.width = 120
	m.height = 32
	m.settings = &app.GlobalSettingsState{
		ConfigPath:       "/tmp/.dec/config.yaml",
		VarsPath:         "/tmp/.dec/local/vars.yaml",
		RepoConnected:    true,
		RepoURL:          "git@github.com:demo/dec.git",
		ConnectedRepoURL: "git@github.com:demo/dec.git",
		AvailableIDEs:    []string{"codex", "cursor"},
		SelectedIDEs:     []string{"cursor"},
		EffectiveIDEs:    []string{"cursor"},
	}
	m.settingsRepoInput = m.settings.RepoURL
	m.settingsSelectedIDEs = []string{"cursor"}
	m.normalizeSettingsCursor()

	view := m.View()
	checks := []string{
		"Global Settings",
		"Repo URL:",
		"当前远端:",
		"[x] cursor",
		"[ ] codex",
		"快捷键：j/k 移动",
	}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Fatalf("Settings View() 缺少 %q:\n%s", check, view)
		}
	}
}

func TestModelSettingsHotkeysToggleIDEAndStartEdit(t *testing.T) {
	m := newModel("/tmp/dec-project")
	m.pageIndex = 4
	m.settings = &app.GlobalSettingsState{
		RepoURL:       "git@github.com:demo/dec.git",
		AvailableIDEs: []string{"cursor", "codex"},
		SelectedIDEs:  []string{"cursor"},
	}
	m.settingsRepoInput = m.settings.RepoURL
	m.settingsSelectedIDEs = []string{"cursor"}
	m.settingsCursor = 2

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if !settingsContainsIDE(m.settingsSelectedIDEs, "codex") {
		t.Fatal("space 应切换当前 IDE 为选中")
	}
	if !m.settingsDirty {
		t.Fatal("切换 IDE 后应标记 settings dirty")
	}

	m.settingsCursor = 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(model)
	if !m.settingsRepoEditing {
		t.Fatal("e 应进入 repo URL 输入状态")
	}
}

func TestModelSettingsSaveUsesAppOperation(t *testing.T) {
	oldSave := saveGlobalSettingsOperation
	defer func() { saveGlobalSettingsOperation = oldSave }()

	called := false
	saveGlobalSettingsOperation = func(input app.SaveGlobalSettingsInput, reporter app.Reporter) (*app.SaveGlobalSettingsResult, error) {
		called = true
		if input.RepoURL != "git@github.com:demo/dec.git" {
			t.Fatalf("RepoURL = %q, 期望 %q", input.RepoURL, "git@github.com:demo/dec.git")
		}
		if len(input.IDEs) != 1 || input.IDEs[0] != "cursor" {
			t.Fatalf("IDEs = %#v, 期望 %#v", input.IDEs, []string{"cursor"})
		}
		return &app.SaveGlobalSettingsResult{IDEs: []string{"cursor"}}, nil
	}

	m := newModel("/tmp/dec-project")
	m.pageIndex = 4
	m.settings = &app.GlobalSettingsState{
		RepoURL:       "git@github.com:demo/dec.git",
		AvailableIDEs: []string{"cursor"},
		SelectedIDEs:  []string{"cursor"},
	}
	m.settingsRepoInput = m.settings.RepoURL
	m.settingsSelectedIDEs = []string{"cursor"}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(model)
	if !m.savingSettings {
		t.Fatal("Settings 页保存后应进入 saving 状态")
	}
	if cmd == nil {
		t.Fatal("Settings 页保存后应返回执行命令")
	}
	msg := cmd()
	resultMsg, ok := msg.(settingsSavedMsg)
	if !ok {
		t.Fatalf("saveSettingsCmd 返回 = %T, 期望 settingsSavedMsg", msg)
	}
	if resultMsg.err != nil {
		t.Fatalf("settingsSavedMsg.err = %v", resultMsg.err)
	}
	if !called {
		t.Fatal("应调用 saveGlobalSettingsOperation")
	}
}

func TestModelSettingsSavePreservesExplicitEmptyIDESelection(t *testing.T) {
	oldSave := saveGlobalSettingsOperation
	defer func() { saveGlobalSettingsOperation = oldSave }()

	called := false
	saveGlobalSettingsOperation = func(input app.SaveGlobalSettingsInput, reporter app.Reporter) (*app.SaveGlobalSettingsResult, error) {
		called = true
		if input.IDEs == nil {
			t.Fatal("IDEs 不应被折叠为 nil")
		}
		if len(input.IDEs) != 0 {
			t.Fatalf("IDEs = %#v, 期望显式空切片", input.IDEs)
		}
		return &app.SaveGlobalSettingsResult{}, nil
	}

	m := newModel("/tmp/dec-project")
	m.pageIndex = 4
	m.settings = &app.GlobalSettingsState{
		RepoURL:       "git@github.com:demo/dec.git",
		AvailableIDEs: []string{"cursor"},
		SelectedIDEs:  []string{"cursor"},
	}
	m.settingsRepoInput = m.settings.RepoURL
	m.settingsSelectedIDEs = []string{}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(model)
	if !m.savingSettings {
		t.Fatal("Settings 页保存后应进入 saving 状态")
	}
	if cmd == nil {
		t.Fatal("Settings 页保存后应返回执行命令")
	}
	msg := cmd()
	resultMsg, ok := msg.(settingsSavedMsg)
	if !ok {
		t.Fatalf("saveSettingsCmd 返回 = %T, 期望 settingsSavedMsg", msg)
	}
	if resultMsg.err != nil {
		t.Fatalf("settingsSavedMsg.err = %v", resultMsg.err)
	}
	if !called {
		t.Fatal("应调用 saveGlobalSettingsOperation")
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
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true, ProjectConfigReady: true, EnabledCount: 2}); !strings.Contains(got, "Run 页") {
		t.Fatalf("项目就绪时建议动作错误: %q", got)
	}
}
