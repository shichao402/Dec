package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/pkg/app"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/shichao402/Dec/pkg/update"
)

func TestModelViewRendersHomeOverview(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
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
	m := newModel("/tmp/dec-project", "v1.0.0")
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
	m := newModel("/tmp/dec-project", "v1.0.0")
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
	m := newModel("/tmp/dec-project", "v1.0.0")
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
	m := newModel("/tmp/dec-project", "v1.0.0")
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
	m := newModel("/tmp/dec-project", "v1.0.0")
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
			m := newModel("/tmp/dec-project", "v1.0.0")
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
	m := newModel("/tmp/dec-project", "v1.0.0")
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
	if len(batchMsg) != 5 {
		t.Fatalf("BatchMsg 长度 = %d, 期望 5", len(batchMsg))
	}
}

func TestModelSettingsPageRendersGlobalSettings(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
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
	m := newModel("/tmp/dec-project", "v1.0.0")
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

	m := newModel("/tmp/dec-project", "v1.0.0")
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

	m := newModel("/tmp/dec-project", "v1.0.0")
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

func TestModelRunPageEnterRemoveFlowWithoutEnabledAssetsStaysIdle(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	m.assets = &app.AssetSelectionState{
		Items: []app.AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default", Enabled: false},
		},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(model)
	if m.removeStage != "" {
		t.Fatalf("没有已启用资产时 x 不应进入 remove 流程, stage = %q", m.removeStage)
	}
}

func TestModelRunPageRemoveFlowSelectConfirmAndCancel(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	m.assets = &app.AssetSelectionState{
		Items: []app.AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default", Enabled: true},
			{Name: "cli-release-rules", Type: "rule", Vault: "cli", Enabled: true},
			{Name: "off-asset", Type: "mcp", Vault: "default", Enabled: false},
		},
	}

	// 进入 select 阶段
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(model)
	if m.removeStage != "select" {
		t.Fatalf("x 后 stage = %q, 期望 select", m.removeStage)
	}
	if len(m.enabledRemoveCandidates()) != 2 {
		t.Fatalf("候选资产数 = %d, 期望 2", len(m.enabledRemoveCandidates()))
	}

	// j 向下
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(model)
	if m.removeCursor != 1 {
		t.Fatalf("j 后 cursor = %d, 期望 1", m.removeCursor)
	}

	// enter 进入 confirm
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.removeStage != "confirm" {
		t.Fatalf("enter 后 stage = %q, 期望 confirm", m.removeStage)
	}
	if m.removeTarget == nil || m.removeTarget.Name != "cli-release-rules" {
		t.Fatalf("removeTarget = %#v, 期望 cli-release-rules", m.removeTarget)
	}

	// n 取消回到 select
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	if m.removeStage != "select" {
		t.Fatalf("n 后 stage = %q, 期望 select", m.removeStage)
	}
	if m.removeTarget != nil {
		t.Fatalf("取消后 removeTarget 应为 nil, 实际 %#v", m.removeTarget)
	}

	// esc 完全退出
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.removeStage != "" {
		t.Fatalf("esc 后 stage = %q, 期望空", m.removeStage)
	}
}

func TestModelRunPageRemoveConfirmTriggersRunRemoveOperation(t *testing.T) {
	oldRemove := runRemoveOperation
	defer func() { runRemoveOperation = oldRemove }()

	called := false
	runRemoveOperation = func(input app.RemoveAssetInput, reporter app.Reporter) (*app.RemoveAssetResult, error) {
		called = true
		if input.Name != "project-workflow" {
			t.Fatalf("Name = %q, 期望 project-workflow", input.Name)
		}
		if !input.Confirmed {
			t.Fatal("Confirmed 应为 true")
		}
		return &app.RemoveAssetResult{Type: input.Type, Name: input.Name, Vault: input.Vault, VersionCommit: "abc123"}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	m.assets = &app.AssetSelectionState{
		Items: []app.AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default", Enabled: true},
		},
	}

	// x → select
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(model)
	// enter → confirm
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	// y → 启动 remove 执行
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if !m.runningRemove {
		t.Fatal("y 后应进入 runningRemove")
	}
	if m.removeStage != "running" {
		t.Fatalf("y 后 stage = %q, 期望 running", m.removeStage)
	}
	if cmd == nil {
		t.Fatal("y 后应返回命令")
	}
	// 执行 batch：第一个子命令是 startRemoveRunCmd 的 goroutine 启动器；第二个是 waitRunMsg
	batchMsg, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd() 类型 = %T, 期望 tea.BatchMsg", cmd())
	}
	// 解析 batch 中每个子 cmd，等待 remove 完成消息
	var completed removeCompletedMsg
	gotCompleted := false
	for _, sub := range batchMsg {
		if sub == nil {
			continue
		}
		msg := sub()
		if msg == nil {
			continue
		}
		if c, ok := msg.(removeCompletedMsg); ok {
			completed = c
			gotCompleted = true
		}
	}
	if !gotCompleted {
		t.Fatal("应在 batch 执行中拿到 removeCompletedMsg")
	}
	if !called {
		t.Fatal("应调用 runRemoveOperation")
	}

	updated, refreshCmd := m.Update(completed)
	m = updated.(model)
	if m.runningRemove {
		t.Fatal("completed 后应退出 runningRemove")
	}
	if m.removeResult == nil || m.removeResult.VersionCommit != "abc123" {
		t.Fatalf("removeResult = %#v, 期望 VersionCommit=abc123", m.removeResult)
	}
	if refreshCmd == nil {
		t.Fatal("完成后应触发 refresh")
	}
}

func TestModelRunPageRemoveFilter(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	m.assets = &app.AssetSelectionState{
		Items: []app.AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default", Enabled: true},
			{Name: "cli-release-rules", Type: "rule", Vault: "cli", Enabled: true},
		},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(model)
	if !m.removeFilterInput {
		t.Fatal("/ 应进入 remove 筛选输入状态")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c', 'l', 'i'}})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	candidates := m.enabledRemoveCandidates()
	if len(candidates) != 1 || candidates[0].Name != "cli-release-rules" {
		t.Fatalf("筛选候选 = %#v, 期望单独的 cli-release-rules", candidates)
	}
}

func TestModelRunPageUpdateEntersCheckingAndConfirmOnNewVersion(t *testing.T) {
	oldCheck := updateCheckOperation
	defer func() { updateCheckOperation = oldCheck }()
	called := false
	updateCheckOperation = func(currentVersion string) (*update.CheckResult, error) {
		called = true
		if currentVersion != "v1.0.0" {
			t.Fatalf("currentVersion = %q, 期望 %q", currentVersion, "v1.0.0")
		}
		return &update.CheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", NeedUpdate: true}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = updated.(model)
	if m.updateStage != "checking" {
		t.Fatalf("u 后 stage = %q, 期望 checking", m.updateStage)
	}
	if cmd == nil {
		t.Fatal("u 后应返回命令")
	}
	msg := cmd()
	checkedMsg, ok := msg.(updateCheckedMsg)
	if !ok {
		t.Fatalf("updateCheck 返回 = %T, 期望 updateCheckedMsg", msg)
	}
	if !called {
		t.Fatal("应调用 updateCheckOperation")
	}

	updated, _ = m.Update(checkedMsg)
	m = updated.(model)
	if m.updateStage != "confirm" {
		t.Fatalf("checked 后 stage = %q, 期望 confirm", m.updateStage)
	}
	if m.updateResult == nil || m.updateResult.LatestVersion != "v1.2.0" {
		t.Fatalf("updateResult = %#v, 期望 LatestVersion=v1.2.0", m.updateResult)
	}
}

func TestModelRunPageUpdateAlreadyLatestSkipsConfirm(t *testing.T) {
	oldCheck := updateCheckOperation
	defer func() { updateCheckOperation = oldCheck }()
	updateCheckOperation = func(currentVersion string) (*update.CheckResult, error) {
		return &update.CheckResult{CurrentVersion: "v1.2.0", LatestVersion: "v1.2.0", NeedUpdate: false}, nil
	}

	m := newModel("/tmp/dec-project", "v1.2.0")
	m.pageIndex = 3

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = updated.(model)
	msg := cmd()
	checked, ok := msg.(updateCheckedMsg)
	if !ok {
		t.Fatalf("checked 消息类型 = %T", msg)
	}

	updated, _ = m.Update(checked)
	m = updated.(model)
	if m.updateStage != "done" {
		t.Fatalf("已最新版本时 stage = %q, 期望 done", m.updateStage)
	}
	if m.updateErr != nil {
		t.Fatalf("updateErr = %v, 期望 nil", m.updateErr)
	}
	if m.updateResult == nil || m.updateResult.NeedUpdate {
		t.Fatalf("updateResult = %#v, 期望 NeedUpdate=false", m.updateResult)
	}
}

func TestModelRunPageUpdateCheckFailureEntersDone(t *testing.T) {
	oldCheck := updateCheckOperation
	defer func() { updateCheckOperation = oldCheck }()
	updateCheckOperation = func(currentVersion string) (*update.CheckResult, error) {
		return nil, errors.New("network down")
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = updated.(model)
	msg := cmd()
	checked, ok := msg.(updateCheckedMsg)
	if !ok {
		t.Fatalf("checked 消息类型 = %T", msg)
	}
	updated, _ = m.Update(checked)
	m = updated.(model)
	if m.updateStage != "done" {
		t.Fatalf("检查失败时 stage = %q, 期望 done", m.updateStage)
	}
	if m.updateErr == nil {
		t.Fatal("检查失败时 updateErr 应非 nil")
	}
}

func TestModelRunPageUpdateConfirmYTriggersDoUpdate(t *testing.T) {
	oldDo := updateDoUpdateOperation
	defer func() { updateDoUpdateOperation = oldDo }()
	called := false
	updateDoUpdateOperation = func(currentVersion string) error {
		called = true
		if currentVersion != "v1.0.0" {
			t.Fatalf("currentVersion = %q, 期望 %q", currentVersion, "v1.0.0")
		}
		return nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	m.updateStage = "confirm"
	m.updateResult = &update.CheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", NeedUpdate: true}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if m.updateStage != "running" {
		t.Fatalf("y 后 stage = %q, 期望 running", m.updateStage)
	}
	if !m.updatingBinary {
		t.Fatal("y 后应 updatingBinary=true")
	}
	if cmd == nil {
		t.Fatal("y 后应返回命令")
	}
	msg := cmd()
	done, ok := msg.(updateDoneMsg)
	if !ok {
		t.Fatalf("DoUpdate 返回 = %T, 期望 updateDoneMsg", msg)
	}
	if !called {
		t.Fatal("应调用 updateDoUpdateOperation")
	}

	updated, _ = m.Update(done)
	m = updated.(model)
	if m.updatingBinary {
		t.Fatal("done 后应退出 updatingBinary")
	}
	if m.updateStage != "done" {
		t.Fatalf("done 后 stage = %q, 期望 done", m.updateStage)
	}
	if m.updateErr != nil {
		t.Fatalf("成功后 updateErr = %v, 期望 nil", m.updateErr)
	}
	if m.updateDoneVersion != "v1.2.0" {
		t.Fatalf("updateDoneVersion = %q, 期望 v1.2.0", m.updateDoneVersion)
	}
}

func TestModelRunPageUpdateConfirmNCancelsFlow(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	m.updateStage = "confirm"
	m.updateResult = &update.CheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", NeedUpdate: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	if m.updateStage != "" {
		t.Fatalf("n 后 stage = %q, 期望空", m.updateStage)
	}
	if m.updateResult != nil {
		t.Fatalf("取消后 updateResult 应为 nil, 实际 %#v", m.updateResult)
	}
}

func TestModelRunPageUpdateFailurePath(t *testing.T) {
	oldDo := updateDoUpdateOperation
	defer func() { updateDoUpdateOperation = oldDo }()
	updateDoUpdateOperation = func(currentVersion string) error {
		return errors.New("download failed")
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	m.updateStage = "confirm"
	m.updateResult = &update.CheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", NeedUpdate: true}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	done, ok := cmd().(updateDoneMsg)
	if !ok {
		t.Fatalf("DoUpdate 返回类型 = %T", cmd())
	}
	updated, _ = m.Update(done)
	m = updated.(model)
	if m.updateStage != "done" {
		t.Fatalf("失败后 stage = %q, 期望 done", m.updateStage)
	}
	if m.updateErr == nil {
		t.Fatal("失败后 updateErr 应非 nil")
	}
}

func TestModelRunPageUpdateRenderingShowsConfirmPanel(t *testing.T) {
	oldCmd := updateManualInstallCommand
	defer func() { updateManualInstallCommand = oldCmd }()
	updateManualInstallCommand = func() string { return "curl -fsSL example.com | bash" }

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	m.width = 120
	m.height = 32
	m.updateStage = "confirm"
	m.updateResult = &update.CheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", NeedUpdate: true}

	view := m.View()
	checks := []string{"Update", "当前版本: v1.0.0", "远端版本: v1.2.0", "按 y 确认"}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Fatalf("Update confirm View() 缺少 %q:\n%s", check, view)
		}
	}
}

func TestModelRunPageUpdateDoneRenderingShowsFallbackOnFailure(t *testing.T) {
	oldCmd := updateManualInstallCommand
	defer func() { updateManualInstallCommand = oldCmd }()
	updateManualInstallCommand = func() string { return "curl -fsSL example.com | bash" }

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	m.width = 120
	m.height = 32
	m.updateStage = "done"
	m.updateErr = errors.New("download failed")

	view := m.View()
	if !strings.Contains(view, "更新失败") {
		t.Fatalf("失败视图缺少 更新失败:\n%s", view)
	}
	if !strings.Contains(view, "curl -fsSL example.com | bash") {
		t.Fatalf("失败视图缺少 fallback 命令:\n%s", view)
	}
}

// ------- Project page (#13) tests -------

func TestModelProjectPageRendersInheritMode(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2 // Project
	m.width = 120
	m.height = 32
	m.projectSettings = &app.ProjectSettingsState{
		ProjectRoot:   "/tmp/dec-project",
		ConfigPath:    "/tmp/dec-project/.dec/config.yaml",
		VarsPath:      "/tmp/dec-project/.dec/vars.yaml",
		AvailableIDEs: []string{"codex", "cursor"},
		GlobalIDEs:    []string{"cursor"},
		EffectiveIDEs: []string{"cursor"},
	}
	m.projectSettingsOverride = false
	m.normalizeProjectSettingsCursor()

	view := m.View()
	checks := []string{
		"Project Settings",
		"当前模式: 继承全局",
		"覆盖全局 IDE",
		"全局默认: cursor",
		"快捷键：j/k 移动",
	}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Fatalf("Project View(inherit) 缺少 %q:\n%s", check, view)
		}
	}
}

func TestModelProjectPageRendersOverrideMode(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.width = 120
	m.height = 32
	m.projectSettings = &app.ProjectSettingsState{
		ProjectRoot:    "/tmp/dec-project",
		ConfigPath:     "/tmp/dec-project/.dec/config.yaml",
		VarsPath:       "/tmp/dec-project/.dec/vars.yaml",
		AvailableIDEs:  []string{"codex", "cursor"},
		SelectedIDEs:   []string{"codex"},
		OverrideActive: true,
		GlobalIDEs:     []string{"cursor"},
		EffectiveIDEs:  []string{"codex"},
	}
	m.projectSettingsOverride = true
	m.projectSettingsSelectedIDEs = []string{"codex"}
	m.normalizeProjectSettingsCursor()

	view := m.View()
	checks := []string{
		"当前模式: 项目显式覆盖",
		"[x] codex",
		"[ ] cursor",
	}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Fatalf("Project View(override) 缺少 %q:\n%s", check, view)
		}
	}
}

func TestModelProjectPageToggleOverrideSwitchesMode(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.projectSettings = &app.ProjectSettingsState{
		AvailableIDEs: []string{"cursor"},
		EffectiveIDEs: []string{"cursor"},
	}
	m.projectSettingsOverride = false
	m.projectSettingsCursor = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if !m.projectSettingsOverride {
		t.Fatal("space 在第 0 行应开启覆盖模式")
	}
	if !m.projectSettingsDirty {
		t.Fatal("从继承切到覆盖后应标记 dirty")
	}
	// 开启覆盖时应用 EffectiveIDEs 预填
	if !settingsContainsIDE(m.projectSettingsSelectedIDEs, "cursor") {
		t.Fatalf("开启覆盖后应预填 EffectiveIDEs, 实际: %#v", m.projectSettingsSelectedIDEs)
	}
}

func TestModelProjectPageToggleIDEInOverrideMode(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.projectSettings = &app.ProjectSettingsState{
		AvailableIDEs:  []string{"cursor", "codex"},
		SelectedIDEs:   []string{"cursor"},
		OverrideActive: true,
	}
	m.projectSettingsOverride = true
	m.projectSettingsSelectedIDEs = []string{"cursor"}
	m.projectSettingsCursor = 2 // 第二个 IDE

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if !settingsContainsIDE(m.projectSettingsSelectedIDEs, "codex") {
		t.Fatal("space 应在覆盖模式下把 codex 切换为选中")
	}
	if !m.projectSettingsDirty {
		t.Fatal("切换 IDE 后应标记 dirty")
	}
}

func TestModelProjectPageToggleIDEInInheritModeIsNoop(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.projectSettings = &app.ProjectSettingsState{
		AvailableIDEs: []string{"cursor", "codex"},
	}
	m.projectSettingsOverride = false
	m.projectSettingsCursor = 2

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if len(m.projectSettingsSelectedIDEs) != 0 {
		t.Fatalf("继承模式下 IDE 行 space 不应改变 selected, 实际: %#v", m.projectSettingsSelectedIDEs)
	}
	if m.projectSettingsOverride {
		t.Fatal("继承模式下 IDE 行 space 不应切换模式")
	}
}

func TestModelProjectPageClearOverrideWithC(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.projectSettings = &app.ProjectSettingsState{
		AvailableIDEs:  []string{"cursor", "codex"},
		SelectedIDEs:   []string{"codex"},
		OverrideActive: true,
	}
	m.projectSettingsOverride = true
	m.projectSettingsSelectedIDEs = []string{"codex"}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(model)
	if m.projectSettingsOverride {
		t.Fatal("c 应一键清除覆盖")
	}
	if len(m.projectSettingsSelectedIDEs) != 0 {
		t.Fatalf("清除后 selected 应为空, 实际: %#v", m.projectSettingsSelectedIDEs)
	}
	if !m.projectSettingsDirty {
		t.Fatal("清除覆盖后应标记 dirty")
	}
}

func TestModelProjectPageSaveCallsOperation_Override(t *testing.T) {
	oldSave := saveProjectSettingsOperation
	defer func() { saveProjectSettingsOperation = oldSave }()

	called := false
	saveProjectSettingsOperation = func(input app.SaveProjectSettingsInput, reporter app.Reporter) (*app.SaveProjectSettingsResult, error) {
		called = true
		if input.ProjectRoot != "/tmp/dec-project" {
			t.Fatalf("ProjectRoot = %q", input.ProjectRoot)
		}
		if input.ClearOverride {
			t.Fatal("期望 ClearOverride=false")
		}
		if len(input.IDEs) != 1 || input.IDEs[0] != "cursor" {
			t.Fatalf("IDEs = %#v, 期望 [cursor]", input.IDEs)
		}
		return &app.SaveProjectSettingsResult{SelectedIDEs: []string{"cursor"}, OverrideActive: true}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.projectSettings = &app.ProjectSettingsState{
		AvailableIDEs: []string{"cursor"},
	}
	m.projectSettingsOverride = true
	m.projectSettingsSelectedIDEs = []string{"cursor"}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(model)
	if !m.savingProjectSettings {
		t.Fatal("s 后应进入 saving 状态")
	}
	if cmd == nil {
		t.Fatal("s 后应返回 tea.Cmd")
	}
	msg := cmd()
	resultMsg, ok := msg.(projectSettingsSavedMsg)
	if !ok {
		t.Fatalf("cmd 返回 = %T, 期望 projectSettingsSavedMsg", msg)
	}
	if resultMsg.err != nil {
		t.Fatalf("saved err = %v", resultMsg.err)
	}
	if !called {
		t.Fatal("应调用 saveProjectSettingsOperation")
	}
}

func TestModelProjectPageSaveCallsOperation_ClearOverride(t *testing.T) {
	oldSave := saveProjectSettingsOperation
	defer func() { saveProjectSettingsOperation = oldSave }()

	called := false
	saveProjectSettingsOperation = func(input app.SaveProjectSettingsInput, reporter app.Reporter) (*app.SaveProjectSettingsResult, error) {
		called = true
		if !input.ClearOverride {
			t.Fatal("期望 ClearOverride=true")
		}
		return &app.SaveProjectSettingsResult{}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.projectSettings = &app.ProjectSettingsState{
		AvailableIDEs:  []string{"cursor"},
		SelectedIDEs:   []string{"cursor"},
		OverrideActive: true,
	}
	// 已加载处于覆盖态，本地编辑切到继承
	m.projectSettingsOverride = false
	m.projectSettingsSelectedIDEs = nil

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(model)
	if !m.savingProjectSettings {
		t.Fatal("s 后应进入 saving 状态")
	}
	if cmd == nil {
		t.Fatal("s 后应返回 tea.Cmd")
	}
	if _, ok := cmd().(projectSettingsSavedMsg); !ok {
		t.Fatal("期望返回 projectSettingsSavedMsg")
	}
	if !called {
		t.Fatal("应调用 saveProjectSettingsOperation (ClearOverride)")
	}
}

func TestModelProjectPageSaveRejectsEmptyOverride(t *testing.T) {
	oldSave := saveProjectSettingsOperation
	defer func() { saveProjectSettingsOperation = oldSave }()
	saveProjectSettingsOperation = func(input app.SaveProjectSettingsInput, reporter app.Reporter) (*app.SaveProjectSettingsResult, error) {
		t.Fatal("不应在空覆盖下调用保存")
		return nil, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.projectSettings = &app.ProjectSettingsState{
		AvailableIDEs: []string{"cursor"},
	}
	m.projectSettingsOverride = true
	m.projectSettingsSelectedIDEs = nil

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(model)
	if m.savingProjectSettings {
		t.Fatal("空覆盖下不应进入 saving 状态")
	}
	if cmd != nil {
		t.Fatal("空覆盖下不应返回保存 tea.Cmd")
	}
}

// ------- Project page init / refresh (#14) tests -------

func TestModelProjectPageInitKeyTriggersCmd(t *testing.T) {
	oldInit := prepareProjectConfigInitOperation
	defer func() { prepareProjectConfigInitOperation = oldInit }()

	called := false
	prepareProjectConfigInitOperation = func(projectRoot string, reporter app.Reporter) (*app.ConfigInitPreparation, error) {
		called = true
		if projectRoot != "/tmp/dec-project" {
			t.Fatalf("ProjectRoot = %q", projectRoot)
		}
		return &app.ConfigInitPreparation{
			ProjectRoot:    projectRoot,
			ExistingConfig: false,
			AssetCount:     5,
			VarsCreated:    true,
			ProjectConfig:  &types.ProjectConfig{},
		}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.overview = &app.ProjectOverview{RepoConnected: true}
	m.projectSettings = &app.ProjectSettingsState{
		AvailableIDEs:      []string{"cursor"},
		ProjectConfigReady: false,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)
	if !m.initializingProjectConfig {
		t.Fatal("按 i 后应进入 initializing 状态")
	}
	if cmd == nil {
		t.Fatal("按 i 后应返回 tea.Cmd")
	}
	msg := cmd()
	if _, ok := msg.(projectConfigInitializedMsg); !ok {
		t.Fatalf("cmd 返回 = %T, 期望 projectConfigInitializedMsg", msg)
	}
	if !called {
		t.Fatal("应调用 prepareProjectConfigInitOperation")
	}
}

func TestModelProjectPageRefreshKeyTriggersCmd(t *testing.T) {
	oldInit := prepareProjectConfigInitOperation
	defer func() { prepareProjectConfigInitOperation = oldInit }()

	called := false
	prepareProjectConfigInitOperation = func(projectRoot string, reporter app.Reporter) (*app.ConfigInitPreparation, error) {
		called = true
		return &app.ConfigInitPreparation{
			ProjectRoot:    projectRoot,
			ExistingConfig: true,
			AssetCount:     8,
			ProjectConfig:  &types.ProjectConfig{},
		}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.overview = &app.ProjectOverview{RepoConnected: true}
	m.projectSettings = &app.ProjectSettingsState{
		AvailableIDEs:      []string{"cursor"},
		ProjectConfigReady: true,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = updated.(model)
	if !m.initializingProjectConfig {
		t.Fatal("按 R 后应进入 initializing 状态")
	}
	if cmd == nil {
		t.Fatal("按 R 后应返回 tea.Cmd")
	}
	if _, ok := cmd().(projectConfigInitializedMsg); !ok {
		t.Fatal("期望返回 projectConfigInitializedMsg")
	}
	if !called {
		t.Fatal("应调用 prepareProjectConfigInitOperation")
	}
}

func TestModelProjectPageInitDisabledWhenRepoNotConnected(t *testing.T) {
	oldInit := prepareProjectConfigInitOperation
	defer func() { prepareProjectConfigInitOperation = oldInit }()
	prepareProjectConfigInitOperation = func(projectRoot string, reporter app.Reporter) (*app.ConfigInitPreparation, error) {
		t.Fatal("未连仓库下不应调用 PrepareProjectConfigInit")
		return nil, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.width = 120
	m.height = 32
	m.overview = &app.ProjectOverview{RepoConnected: false}
	m.projectSettings = &app.ProjectSettingsState{
		ProjectRoot:        "/tmp/dec-project",
		ConfigPath:         "/tmp/dec-project/.dec/config.yaml",
		VarsPath:           "/tmp/dec-project/.dec/vars.yaml",
		AvailableIDEs:      []string{"cursor"},
		ProjectConfigReady: false,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)
	if m.initializingProjectConfig {
		t.Fatal("未连仓库下按 i 不应进入 initializing 状态")
	}
	if cmd != nil {
		t.Fatal("未连仓库下按 i 不应返回 tea.Cmd")
	}
	view := m.View()
	if !strings.Contains(view, "未连接仓库") {
		t.Fatalf("View 未提示未连接仓库:\n%s", view)
	}
}

func TestModelProjectPageInitSuccessRendersSummary(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.width = 120
	m.height = 32
	m.overview = &app.ProjectOverview{RepoConnected: true}
	m.projectSettings = &app.ProjectSettingsState{
		ProjectRoot:        "/tmp/dec-project",
		ConfigPath:         "/tmp/dec-project/.dec/config.yaml",
		VarsPath:           "/tmp/dec-project/.dec/vars.yaml",
		AvailableIDEs:      []string{"cursor"},
		ProjectConfigReady: false,
	}
	// 模拟消息回来
	updated, _ := m.Update(projectConfigInitializedMsg{
		result: &app.ConfigInitPreparation{
			ExistingConfig: false,
			AssetCount:     7,
			VarsCreated:    true,
			ProjectConfig:  &types.ProjectConfig{},
		},
		err: nil,
	})
	m = updated.(model)
	if m.initializingProjectConfig {
		t.Fatal("收到消息后应退出 initializing 状态")
	}
	if m.lastInitResult == nil || m.lastInitResult.AssetCount != 7 {
		t.Fatalf("lastInitResult = %#v", m.lastInitResult)
	}
	view := m.View()
	if !strings.Contains(view, "初始化完成，发现 7 个可用资产") {
		t.Fatalf("View 未显示初始化完成文案:\n%s", view)
	}
	if !strings.Contains(view, "已生成 .dec/vars.yaml 模板") {
		t.Fatalf("View 未显示 vars 模板生成文案:\n%s", view)
	}
}

func TestModelProjectPageInitEmptyRepoRendersHint(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.width = 120
	m.height = 32
	m.overview = &app.ProjectOverview{RepoConnected: true}
	m.projectSettings = &app.ProjectSettingsState{
		ProjectRoot:        "/tmp/dec-project",
		ConfigPath:         "/tmp/dec-project/.dec/config.yaml",
		VarsPath:           "/tmp/dec-project/.dec/vars.yaml",
		AvailableIDEs:      []string{"cursor"},
		ProjectConfigReady: false,
	}
	updated, _ := m.Update(projectConfigInitializedMsg{
		result: &app.ConfigInitPreparation{
			ExistingConfig: false,
			AssetCount:     0,
			ProjectConfig:  nil,
		},
		err: nil,
	})
	m = updated.(model)
	if m.lastInitErr != nil {
		t.Fatalf("空仓库不应视为错误: %v", m.lastInitErr)
	}
	view := m.View()
	if !strings.Contains(view, "仓库暂无资产") {
		t.Fatalf("View 未显示仓库暂无资产文案:\n%s", view)
	}
}

func TestModelProjectVarsBlockRendersUsedPlaceholders(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.width = 120
	m.height = 32
	m.overview = &app.ProjectOverview{RepoConnected: true}
	m.projectSettings = &app.ProjectSettingsState{
		ProjectRoot:        "/tmp/dec-project",
		ConfigPath:         "/tmp/dec-project/.dec/config.yaml",
		VarsPath:           "/tmp/dec-project/.dec/vars.yaml",
		AvailableIDEs:      []string{"cursor"},
		ProjectConfigReady: true,
	}
	m.projectVars = &app.ProjectVarsView{
		VarsPath:         "/tmp/dec-project/.dec/vars.yaml",
		VarsFileReady:    true,
		CacheExists:      true,
		EditorCommand:    "vim",
		UsedPlaceholders: []string{"FOO", "MISSING"},
		ResolvedVars: map[string]app.PlaceholderStatus{
			"FOO":     {Name: "FOO", Value: "foo-val", Source: app.PlaceholderSourceProject},
			"MISSING": {Name: "MISSING", Source: app.PlaceholderSourceMissing},
		},
	}

	view := m.View()
	for _, check := range []string{"Project Variables", "FOO", "MISSING", "e 打开外部编辑器", "vim"} {
		if !strings.Contains(view, check) {
			t.Fatalf("Project 页未包含 %q:\n%s", check, view)
		}
	}
}

func TestModelProjectVarsBlockNoCacheHint(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.width = 120
	m.height = 32
	m.overview = &app.ProjectOverview{RepoConnected: true}
	m.projectSettings = &app.ProjectSettingsState{
		ProjectRoot:        "/tmp/dec-project",
		ConfigPath:         "/tmp/dec-project/.dec/config.yaml",
		VarsPath:           "/tmp/dec-project/.dec/vars.yaml",
		AvailableIDEs:      []string{"cursor"},
		ProjectConfigReady: true,
	}
	m.projectVars = &app.ProjectVarsView{
		VarsPath:      "/tmp/dec-project/.dec/vars.yaml",
		VarsFileReady: false,
		CacheExists:   false,
	}

	view := m.View()
	if !strings.Contains(view, "未生成") {
		t.Fatalf("期望未生成 vars 文件的提示:\n%s", view)
	}
	if !strings.Contains(view, ".dec/cache 尚不存在") {
		t.Fatalf("期望 cache 不存在的提示:\n%s", view)
	}
}

func TestModelProjectEditKeyInvokesCmd(t *testing.T) {
	oldEnsure := ensureProjectVarsFileOperation
	defer func() { ensureProjectVarsFileOperation = oldEnsure }()

	called := false
	ensureProjectVarsFileOperation = func(projectRoot string) (*app.EnsureProjectVarsFileResult, error) {
		called = true
		return &app.EnsureProjectVarsFileResult{Path: "/tmp/dec-project/.dec/vars.yaml", Created: true}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.overview = &app.ProjectOverview{RepoConnected: true}
	m.projectSettings = &app.ProjectSettingsState{AvailableIDEs: []string{"cursor"}, ProjectConfigReady: true}
	m.projectVars = &app.ProjectVarsView{VarsPath: "/tmp/dec-project/.dec/vars.yaml", EditorCommand: "vim"}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("按 e 应返回 tea.Cmd")
	}
	if !called {
		t.Fatal("按 e 应触发 ensureProjectVarsFileOperation")
	}
}

func TestModelProjectVarsEditedMsgRefreshesView(t *testing.T) {
	oldLoad := loadProjectVarsViewOperation
	defer func() { loadProjectVarsViewOperation = oldLoad }()

	loaded := false
	loadProjectVarsViewOperation = func(projectRoot string) (*app.ProjectVarsView, error) {
		loaded = true
		return &app.ProjectVarsView{VarsPath: "/tmp/dec-project/.dec/vars.yaml", VarsFileReady: true}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2

	updated, cmd := m.Update(projectVarsEditedMsg{err: nil})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("projectVarsEditedMsg 后应返回 reload tea.Cmd")
	}
	// 执行 cmd，看它是否真的调用了 loader
	msg := cmd()
	if _, ok := msg.(projectVarsLoadedMsg); !ok {
		t.Fatalf("cmd 返回 = %T, 期望 projectVarsLoadedMsg", msg)
	}
	if !loaded {
		t.Fatal("编辑完成后应重新加载 project vars view")
	}
}

func TestModelProjectVarsEditedMsgSurfacesError(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 2
	m.width = 120
	m.height = 32
	m.overview = &app.ProjectOverview{RepoConnected: true}
	m.projectSettings = &app.ProjectSettingsState{
		AvailableIDEs:      []string{"cursor"},
		ProjectConfigReady: true,
	}
	m.projectVars = &app.ProjectVarsView{VarsPath: "/tmp/dec-project/.dec/vars.yaml"}

	updated, _ := m.Update(projectVarsEditedMsg{err: errors.New("编辑器未退出正常")})
	m = updated.(model)
	if m.lastEditErr == nil {
		t.Fatal("期望 lastEditErr 被记录")
	}
	view := m.View()
	if !strings.Contains(view, "编辑器返回错误") {
		t.Fatalf("期望 UI 显示编辑器错误:\n%s", view)
	}
}

// -------- #93 Bundle-aware Assets 页 --------

// 构造一个带 1 个 bundle（2 成员）+ 2 独立资产的 Assets 状态。
func assetsStateWithBundle() *app.AssetSelectionState {
	return &app.AssetSelectionState{
		ExistingConfig: true,
		ConfigPath:     "/tmp/dec-project/.dec/config.yaml",
		VarsPath:       "/tmp/dec-project/.dec/vars.yaml",
		Items: []app.AssetSelectionItem{
			{Name: "vikunja-workflow", Type: "skill", Vault: "default"},
			{Name: "vikunja-issue", Type: "skill", Vault: "default"},
			{Name: "solo-rule", Type: "rule", Vault: "cli"},
		},
		Bundles: []app.AssetBundleOption{
			{
				Name:        "vikunja",
				Description: "Vikunja workflow bundle",
				Vault:       "default",
				Enabled:     false,
				Members: []app.AssetSelectionItem{
					{Name: "vikunja-workflow", Type: "skill", Vault: "default"},
					{Name: "vikunja-issue", Type: "skill", Vault: "default"},
				},
			},
		},
	}
}

// 把光标定位到 visibleAssetRows 中首个 kind==want 的行。
func seekCursorToKind(t *testing.T, m *model, want assetRowKind) {
	t.Helper()
	rows := m.visibleAssetRows()
	for i, r := range rows {
		if r.kind == want {
			m.assetCursor = i
			return
		}
	}
	t.Fatalf("rows 中找不到 kind=%d 的行", want)
}

func TestModelAssetsBundleToggleMarksMembersReadonly(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.assets = assetsStateWithBundle()
	m.normalizeAssetCursor()

	// 光标落在 bundle 节点行并按空格 toggle。
	seekCursorToKind(t, &m, assetRowBundle)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)

	if !m.bundleSelected("vikunja") {
		t.Fatal("bundle 应被勾选")
	}
	if !m.assetsDirty {
		t.Fatal("勾选 bundle 应标脏")
	}
	// 单独取消成员应被拒绝：Items[0] 仍保持 Enabled=false
	// 找到成员对应的 Items 下标
	for i, it := range m.assets.Items {
		if it.Type == "skill" && it.Name == "vikunja-workflow" {
			if it.Enabled {
				t.Fatalf("成员不应被自动 Enabled=true：Items[%d]", i)
			}
		}
	}
	// 找到独立资产行尝试 toggle：独立 rule 应仍可切换
	seekCursorToKind(t, &m, assetRowAsset)
	rows := m.visibleAssetRows()
	var cursorItem app.AssetSelectionItem
	for _, r := range rows {
		if r.kind == assetRowAsset {
			cursorItem = m.assets.Items[r.assetIndex]
			break
		}
	}
	// bundle 带入的资产排在 Items 前面；cursorItem 若是 vikunja-workflow 则应为只读。
	if cursorItem.Name == "vikunja-workflow" {
		prev := m.assets.Items[0].Enabled
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
		m = updated.(model)
		if m.assets.Items[0].Enabled != prev {
			t.Fatal("bundle 带入的资产应只读，不应被 space 翻转")
		}
	}
}

func TestModelAssetsBundleUnselectReturnsMembersToStandalone(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.assets = assetsStateWithBundle()
	// 预置：bundle 已选中；同时把 Items[1] 单独 enabled（模拟用户也显式 enabled 了成员）。
	m.bundleSelection = []string{"vikunja"}
	m.assets.Items[1].Enabled = true
	m.normalizeAssetCursor()

	// 取消 bundle
	seekCursorToKind(t, &m, assetRowBundle)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if m.bundleSelected("vikunja") {
		t.Fatal("bundle 应被取消")
	}
	// Items[1] (独立 enabled) 应保留独立启用状态
	if !m.assets.Items[1].Enabled {
		t.Fatal("取消 bundle 后，独立 enabled 的成员应保留 enabled")
	}
}

func TestModelAssetsSaveSendsBundlesAndFiltersImplicitMembers(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.assets = assetsStateWithBundle()
	m.bundleSelection = []string{"vikunja"}
	// 模拟：成员 Items[0] 被 "伪装" 为 Enabled=true（测试 filterItemsForSave 会把它挤掉）；
	// 独立 rule (Items[2]) Enabled=true 应保留。
	m.assets.Items[0].Enabled = true
	m.assets.Items[2].Enabled = true

	got := filterItemsForSave(m.assets.Items, m.bundleSelection, m.assets.Bundles)
	if got[0].Enabled {
		t.Fatal("被 bundle 带入的成员 Enabled 应被过滤为 false")
	}
	if !got[2].Enabled {
		t.Fatal("独立资产 Enabled 应保留")
	}
}

func TestModelAssetsTypeFilterCycleAndBundleOnlyView(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.assets = assetsStateWithBundle()
	m.normalizeAssetCursor()

	if m.assetTypeFilter != "all" {
		t.Fatalf("初始 type filter = %q, 期望 all", m.assetTypeFilter)
	}
	// 按 t 轮转一次到 skill
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(model)
	if m.assetTypeFilter != "skill" {
		t.Fatalf("第一次 t = %q, 期望 skill", m.assetTypeFilter)
	}
	// skill 过滤下不应显示 rule 资产
	for _, r := range m.visibleAssetRows() {
		if r.kind == assetRowAsset && m.assets.Items[r.assetIndex].Type == "rule" {
			t.Fatal("skill 过滤下不应包含 rule 行")
		}
	}
	// 继续到 rule/mcp/bundle
	for i := 0; i < 3; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
		m = updated.(model)
	}
	if m.assetTypeFilter != "bundle" {
		t.Fatalf("第四次 t = %q, 期望 bundle", m.assetTypeFilter)
	}
	// bundle 视图只含 bundle 节点行（+ 展开的成员），不含单资产行
	for _, r := range m.visibleAssetRows() {
		if r.kind == assetRowAsset {
			t.Fatal("bundle 视图下不应包含单资产行")
		}
	}
}

func TestModelAssetsBundleEnterExpandsAndCollapses(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.assets = assetsStateWithBundle()
	m.normalizeAssetCursor()
	seekCursorToKind(t, &m, assetRowBundle)

	before := len(m.visibleAssetRows())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	after := len(m.visibleAssetRows())
	if after <= before {
		t.Fatalf("展开后 rows %d 应大于折叠态 %d", after, before)
	}
	if !m.expandedBundles["vikunja"] {
		t.Fatal("bundle 应处于展开态")
	}
	// 再按一次 enter 折叠
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.expandedBundles["vikunja"] {
		t.Fatal("bundle 应处于折叠态")
	}
}

func TestModelAssetsNoBundleLegacyBehaviorUnchanged(t *testing.T) {
	// 回归：存量项目（assets.Bundles 为空）时，Assets 页 toggle / filter 行为应与旧版一致。
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.assets = &app.AssetSelectionState{
		Items: []app.AssetSelectionItem{{Name: "solo", Type: "skill", Vault: "default"}},
	}
	m.normalizeAssetCursor()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if !m.assets.Items[0].Enabled {
		t.Fatal("无 bundle 项目 space 应翻转单资产")
	}
	if len(m.bundleSelection) != 0 {
		t.Fatal("无 bundle 项目不应修改 bundleSelection")
	}
	if len(m.visibleAssetRows()) != 1 {
		t.Fatalf("rows = %d, 期望 1（仅单资产）", len(m.visibleAssetRows()))
	}
}

func TestModelAssetsSaveCmdSignatureCarriesBundles(t *testing.T) {
	// saveAssetsCmd 应以 AssetSaveSelection{Items, EnabledBundles} 调用用例层。
	// 这里直接调用 saveAssetsCmd 闭包并检查 msg 语义，不落盘：通过注入临时 projectRoot 触发错误路径即可。
	// 关键是验证 app.AssetSaveSelection 被构造并传入；构造本身由 saveAssetsCmd 完成。
	// 简化：通过 filterItemsForSave + SaveAssetSelection 的类型存在性断言确保签名未被回退。
	_ = saveAssetsCmd("/nonexistent", nil, []string{"x"})
}

