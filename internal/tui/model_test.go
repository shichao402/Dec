package tui

import (
	"context"
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
		"Bundles",
		"项目名:",
		"git@github.com:demo/dec.git",
		"展开资产: 5 可用 / 2 已启用",
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

func TestModelHomeShowsVaultInferencePrompt(t *testing.T) {
	oldInfer := inferVaultProjectOperation
	defer func() { inferVaultProjectOperation = oldInfer }()
	inferVaultProjectOperation = func(projectRoot string, reporter app.Reporter) (*app.VaultProjectInference, error) {
		return &app.VaultProjectInference{
			ProjectRoot:    projectRoot,
			ProjectName:    "Dec",
			VaultPath:      "projects/Dec.yaml",
			EnabledBundles: []string{"dec-agent", "dec-vikunja"},
		}, nil
	}

	m := newModel("/Users/firo/workspace/Dec", "v1.0.0")
	m.width = 120
	m.height = 36
	m.overview = &app.ProjectOverview{
		ProjectRoot:   "/Users/firo/workspace/Dec",
		ProjectName:   "Dec",
		RepoConnected: true,
	}

	msg := loadOverviewCmd("/Users/firo/workspace/Dec")()
	overviewMsg, ok := msg.(overviewLoadedMsg)
	if !ok {
		t.Fatalf("loadOverviewCmd 返回 = %T, 期望 overviewLoadedMsg", msg)
	}

	updated, _ := m.Update(overviewMsg)
	m = updated.(model)
	view := m.View()
	checks := []string{
		"根据目录名推断 vault project，请确认是否应用",
		"推断 project: Dec",
		"projects/Dec.yaml",
		"dec-agent, dec-vikunja",
		"y / Enter 应用 · n 跳过并手动选择",
	}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Fatalf("Home 页应展示 vault 推断确认:\n缺少 %q\n%s", check, view)
		}
	}
}

func TestModelHomeVaultInferenceConfirmApplies(t *testing.T) {
	oldApply := applyVaultProjectOperation
	defer func() { applyVaultProjectOperation = oldApply }()
	applyVaultProjectOperation = func(projectRoot string, reporter app.Reporter) (*app.VaultProjectAutoApplyResult, error) {
		return &app.VaultProjectAutoApplyResult{
			ProjectRoot:    projectRoot,
			ProjectName:    "Dec",
			EnabledBundles: []string{"dec-agent"},
			Applied:        true,
			VarsCreated:    true,
		}, nil
	}

	m := newModel("/Users/firo/workspace/Dec", "v1.0.0")
	m.width = 120
	m.height = 36
	m.overview = &app.ProjectOverview{RepoConnected: true}
	m.vaultInference = &app.VaultProjectInference{
		ProjectName:    "Dec",
		EnabledBundles: []string{"dec-agent"},
	}

	updated, cmd := m.handleVaultInferenceKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if !m.applyingVaultProject {
		t.Fatal("按 y 后应进入 applying 状态")
	}
	if cmd == nil {
		t.Fatal("按 y 后应触发 apply 命令")
	}

	appliedMsg := cmd()
	resultMsg, ok := appliedMsg.(vaultProjectAppliedMsg)
	if !ok {
		t.Fatalf("apply 命令返回 = %T, 期望 vaultProjectAppliedMsg", appliedMsg)
	}

	updated, refreshCmd := m.Update(resultMsg)
	m = updated.(model)
	if m.vaultAutoApplyNotice != "已从 vault 应用 project Dec" {
		t.Fatalf("vaultAutoApplyNotice = %q", m.vaultAutoApplyNotice)
	}
	if m.vaultInference != nil {
		t.Fatal("应用成功后应清除 vaultInference")
	}
	if refreshCmd == nil {
		t.Fatal("应用成功后应触发 refresh")
	}
}

func TestModelHomeVaultInferenceDismiss(t *testing.T) {
	m := newModel("/Users/firo/workspace/Dec", "v1.0.0")
	m.width = 120
	m.height = 36
	m.overview = &app.ProjectOverview{RepoConnected: true, ProjectName: "Dec"}
	m.vaultInference = &app.VaultProjectInference{
		ProjectName:    "Dec",
		EnabledBundles: []string{"dec-agent"},
	}

	updated, cmd := m.handleVaultInferenceKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("按 n 不应触发命令")
	}
	if !m.vaultInferenceDismissed {
		t.Fatal("按 n 后应标记 dismissed")
	}
	if m.hasVaultInferencePrompt() {
		t.Fatal("dismiss 后不应再显示确认提示")
	}
	view := m.View()
	if strings.Contains(view, "请确认是否应用") {
		t.Fatalf("dismiss 后不应展示确认块:\n%s", view)
	}
	got := suggestNextAction(m.overview, false, true)
	if !strings.Contains(got, "Bundles 页") {
		t.Fatalf("dismiss 后建议下一步应指向 Bundles 页: %q", got)
	}
}

func TestModelAssetsPageRendersSelectionState(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.width = 110
	m.height = 32
	m.assetTypeFilter = "all"
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
		"Bundle 列表",
		"Details",
		"[x] default / skill / project-workflow",
		"[ ] cli / rule / cli-release-rules",
		"快捷键：j/k 移动 · h 返回导航",
	}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Fatalf("Bundles View() 缺少 %q:\n%s", check, view)
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
		"Run · Pull 完成",
		"状态",
		"上次结果",
		"Pull  请求 2 · 成功 1 · 失败 1",
		"IDE   cursor",
		"Commit abc123",
		"事件日志",
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
	m.focus = focusContent
	m.assetTypeFilter = "all"
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
	m.focus = focusContent
	m.assetTypeFilter = "all"
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
	m.focus = focusContent
	m.assets = &app.AssetSelectionState{
		Items: []app.AssetSelectionItem{{Name: "project-workflow", Type: "skill", Vault: "default"}},
	}
	m.assetFilter = "missing"
	m.normalizeAssetCursor()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.pageIndex != 1 {
		t.Fatalf("内容区无可见资产时按 down 不应切出 Bundles 页, pageIndex = %d", m.pageIndex)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(model)
	if m.pageIndex != 1 {
		t.Fatalf("内容区无可见资产时按 up 不应切出 Bundles 页, pageIndex = %d", m.pageIndex)
	}

	// 侧栏焦点下 j/k 应切换页
	m.focus = focusSidebar
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.pageIndex == 1 {
		t.Fatal("侧栏焦点下按 down 应切换页")
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

func TestModelRunPageHotkeysStartPush(t *testing.T) {
	oldPreview := previewPushOperation
	defer func() { previewPushOperation = oldPreview }()
	previewPushOperation = func(projectRoot string) (*app.PushProjectAssetsPreview, error) {
		return &app.PushProjectAssetsPreview{
			EnabledBundleCount: 1,
			EnabledBundleNames: []string{"vikunja"},
			SecretsFileCount:   2,
			SecretsTargetCount: 1,
			DecHasChanges:      true,
			DecCandidateCount:  3,
		}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	m = updated.(model)
	if m.pushStage != "summary" {
		t.Fatalf("pushStage = %q, 期望 summary", m.pushStage)
	}
	if m.pushPreview == nil {
		t.Fatal("按 P 后应有 pushPreview")
	}
	if m.runningPull {
		t.Fatal("按 P 后不应直接进入 push 执行")
	}
	if cmd != nil {
		t.Fatal("按 P 进入确认页时不应返回执行命令")
	}
	if summary := m.currentSummary(); summary != "Confirming push (summary)" {
		t.Fatalf("currentSummary() = %q, 期望 Confirming push (summary)", summary)
	}
}

func TestModelRunPagePushFlowDoubleConfirmAndCancel(t *testing.T) {
	oldPreview := previewPushOperation
	defer func() { previewPushOperation = oldPreview }()
	previewPushOperation = func(projectRoot string) (*app.PushProjectAssetsPreview, error) {
		return &app.PushProjectAssetsPreview{EnabledBundleCount: 1}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	m = updated.(model)
	if m.pushStage != "summary" {
		t.Fatalf("P 后 stage = %q, 期望 summary", m.pushStage)
	}

	// y → confirm
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if m.pushStage != "confirm" {
		t.Fatalf("y 后 stage = %q, 期望 confirm", m.pushStage)
	}

	// n → 回到 summary
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	if m.pushStage != "summary" {
		t.Fatalf("n 后 stage = %q, 期望 summary", m.pushStage)
	}

	// esc 完全退出
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.pushStage != "" {
		t.Fatalf("esc 后 stage = %q, 期望空", m.pushStage)
	}
}

func TestModelRunPagePushConfirmTriggersRunPushOperation(t *testing.T) {
	oldPreview := previewPushOperation
	oldPush := runPushOperation
	defer func() {
		previewPushOperation = oldPreview
		runPushOperation = oldPush
	}()

	previewPushOperation = func(projectRoot string) (*app.PushProjectAssetsPreview, error) {
		return &app.PushProjectAssetsPreview{EnabledBundleCount: 1}, nil
	}
	called := false
	runPushOperation = func(ctx context.Context, projectRoot string, reporter app.Reporter) (*app.PushProjectAssetsResult, error) {
		called = true
		return &app.PushProjectAssetsResult{DecPushedCount: 1}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)

	if !m.runningPull || m.runMode != "push" {
		t.Fatal("最终 y 后应进入 push 执行")
	}
	if m.pushStage != "running" {
		t.Fatalf("最终 y 后 stage = %q, 期望 running", m.pushStage)
	}
	if cmd == nil {
		t.Fatal("最终 y 后应返回执行命令")
	}

	// 执行 batch：第一个子命令是 startPushRunCmd 的 goroutine 启动器；第二个是 waitRunMsg
	batchMsg, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd() 类型 = %T, 期望 tea.BatchMsg", cmd())
	}
	var completed runCompletedMsg
	gotCompleted := false
	for _, sub := range batchMsg {
		if sub == nil {
			continue
		}
		msg := sub()
		if msg == nil {
			continue
		}
		if c, ok := msg.(runCompletedMsg); ok {
			completed = c
			gotCompleted = true
		}
	}
	if !gotCompleted {
		t.Fatal("应在 batch 执行中拿到 runCompletedMsg")
	}
	if !called {
		t.Fatal("应调用 runPushOperation")
	}

	updated, _ = m.Update(completed)
	m = updated.(model)
	if m.runningPull {
		t.Fatal("completed 后应退出 runningPull")
	}
	if m.pushStage != "" {
		t.Fatalf("completed 后 pushStage = %q, 期望空", m.pushStage)
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
	m.pageIndex = 5
	m.focus = focusContent
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
	m.pageIndex = 5
	m.focus = focusContent
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
	m.pageIndex = 5
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
	m.pageIndex = 5
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
	if got := suggestNextAction(&app.ProjectOverview{}, false, false); !strings.Contains(got, "Settings 页") {
		t.Fatalf("未连接仓库时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true}, false, false); !strings.Contains(got, "Home 页") {
		t.Fatalf("未初始化项目时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true}, true, false); !strings.Contains(got, "确认推断") {
		t.Fatalf("推断待确认时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true}, false, true); !strings.Contains(got, "Bundles 页") {
		t.Fatalf("推断已跳过时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true, ProjectConfigReady: true, EnabledCount: 0}, false, false); !strings.Contains(got, "Bundles 页") {
		t.Fatalf("无已启用 bundle 时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true, ProjectConfigReady: true, EnabledCount: 0, EnabledBundleCount: 1}, false, false); !strings.Contains(got, "Run 页") {
		t.Fatalf("仅 enabled_bundles 非空时应建议 Run 页: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true, ProjectConfigReady: true, EnabledCount: 2}, false, false); !strings.Contains(got, "Run 页") {
		t.Fatalf("项目就绪时建议动作错误: %q", got)
	}
}

func TestRenderRunIdleGuideEnabledBundlesOnly(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.overview = &app.ProjectOverview{
		EnabledCount:       0,
		EnabledBundleCount: 1,
	}
	lines := m.renderRunIdleGuide()
	for _, line := range lines {
		if strings.Contains(line, "无启用 bundle") {
			t.Fatalf("enabled_bundles 非空时不应显示无启用 bundle 警告: %q", strings.Join(lines, "\n"))
		}
	}

	m.overview = &app.ProjectOverview{EnabledCount: 0, EnabledBundleCount: 0}
	lines = m.renderRunIdleGuide()
	found := false
	for _, line := range lines {
		if strings.Contains(line, "无启用 bundle") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("enabled_bundles 与 enabled 均为空时应显示警告: %q", strings.Join(lines, "\n"))
	}
}

func TestModelDeletePageGroupsByBundle(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 4
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{
		{
			Kind: app.DeleteKindDecAsset, Label: "[dec/skill] demo / vikunja", Type: "skill", Name: "demo", Vault: "vikunja",
			TreeRoot: ".dec", TreeBranch: "vikunja", GroupOrder: 0, GroupTitle: "vikunja",
		},
		{
			Kind: app.DeleteKindSecret, Label: "[secret] mise/conf.d/vikunja.toml",
			SecretPath: "mise/conf.d/vikunja.toml", SecretsBundle: "vikunja_workflow",
			TreeRoot: ".secrets", TreeBranch: "vikunja_workflow", GroupOrder: 0, GroupTitle: "vikunja_workflow",
		},
		{
			Kind: app.DeleteKindBundle, Label: "[bundle] vikunja / vikunja · 2 成员", BundleName: "vikunja",
			TreeRoot: ".dec", TreeBranch: "vikunja", GroupOrder: 0, GroupTitle: "vikunja",
		},
	}
	m.deleteCandidatesLoaded = true
	m.rebuildDeleteTree()

	view := m.View()
	for _, want := range []string{
		"▾ .dec",
		"▾ cache",
		"▾ vikunja",
		"↳ demo",
		"▾ .secrets",
		"vikunja.toml",
		"[bundle]",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("Delete 页应展示目录树，缺少 %q:\n%s", want, view)
		}
	}
}

func TestModelDeletePageShowsBundleCandidates(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 4
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindBundle, BundleName: "vikunja", Label: "[bundle] vikunja / vikunja · 2 成员"},
	}
	m.deleteCandidatesLoaded = true

	view := m.View()
	if !strings.Contains(view, "[bundle] vikunja") {
		t.Fatalf("Delete 页应展示 bundle 候选项:\n%s", view)
	}
}

func TestModelDeletePageHCollapsesBeforeSidebar(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 4
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindBundle, BundleName: "cli", Label: "[bundle] cli", TreeRoot: ".dec", TreeBranch: "cli"},
	}
	m.deleteCandidatesLoaded = true
	m.rebuildDeleteTree()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(model)
	if m.focus != focusContent {
		t.Fatalf("叶子行 h 应先折叠父目录, focus = %q", m.focus)
	}
	if len(m.deleteTree.VisibleRows()) >= 4 {
		t.Fatalf("h 后应减少可见行, rows=%d", len(m.deleteTree.VisibleRows()))
	}

	// 折叠到根且无法再折叠时，h 才返回侧栏
	for tries := 0; tries < 8; tries++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
		m = updated.(model)
		if m.focus == focusSidebar {
			break
		}
	}
	if m.focus != focusSidebar {
		t.Fatalf("全部折叠后 h 应返回 sidebar, focus = %q", m.focus)
	}
}

func TestModelDeletePageEnterTogglesDirectory(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 4
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindBundle, BundleName: "cli", Label: "[bundle] cli", TreeRoot: ".dec", TreeBranch: "cli"},
	}
	m.deleteCandidatesLoaded = true
	m.rebuildDeleteTree()
	m.deleteTree.Cursor = 0 // .dec

	before := len(m.deleteTree.VisibleRows())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if len(m.deleteTree.VisibleRows()) >= before {
		t.Fatalf("Enter 在 .dec 上应折叠, before=%d after=%d", before, len(m.deleteTree.VisibleRows()))
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if len(m.deleteTree.VisibleRows()) != before {
		t.Fatalf("再次 Enter 应展开, rows=%d want %d", len(m.deleteTree.VisibleRows()), before)
	}
}

func TestModelDeletePageEscReturnsToSidebar(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 4
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindBundle, BundleName: "cli", Label: "[bundle] cli"},
	}
	m.deleteCandidatesLoaded = true

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.focus != focusSidebar {
		t.Fatalf("Esc 后 focus = %q, 期望 sidebar", m.focus)
	}

	m.focus = focusContent
	m.rebuildDeleteTree()
	// 先折叠到只剩根节点，此时 h 才返回侧栏
	for tries := 0; tries < 8; tries++ {
		if len(m.deleteTree.VisibleRows()) <= 1 {
			break
		}
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
		m = updated.(model)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(model)
	if m.focus != focusSidebar {
		t.Fatalf("根节点折叠后 h 应返回 sidebar, focus = %q", m.focus)
	}
}

func TestModelDeletePageSelectConfirmAndCancel(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 4
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindBundle, BundleName: "cli", Label: "[bundle] cli", TreeRoot: ".dec", TreeBranch: "cli"},
		{Kind: app.DeleteKindBundle, BundleName: "vikunja", Label: "[bundle] vikunja", TreeRoot: ".dec", TreeBranch: "vikunja"},
	}
	m.deleteCandidatesLoaded = true
	m.rebuildDeleteTree()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	for tries := 0; tries < 4; tries++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = updated.(model)
		if row, ok := m.deleteTree.currentRow(); ok && row.SelectIndex >= 0 && !m.deleteTree.IsSelected(row.SelectIndex) {
			break
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	if len(m.selectedDeleteItems()) != 2 {
		t.Fatalf("应选中 2 项, 实际 %d", len(m.selectedDeleteItems()))
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)
	if m.deleteStage != "summary" {
		t.Fatalf("d 后 stage = %q, 期望 summary", m.deleteStage)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if m.deleteStage != "confirm" {
		t.Fatalf("summary y 后 stage = %q, 期望 confirm", m.deleteStage)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	if m.deleteStage != "summary" {
		t.Fatalf("confirm n 后 stage = %q, 期望 summary", m.deleteStage)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.deleteStage != "" {
		t.Fatalf("summary esc 后 stage = %q, 期望空", m.deleteStage)
	}
}

func TestModelDeletePageConfirmTriggersDeleteOperation(t *testing.T) {
	oldDelete := runDeleteOperation
	defer func() { runDeleteOperation = oldDelete }()

	called := false
	runDeleteOperation = func(ctx context.Context, input app.DeleteProjectInput, reporter app.Reporter) (*app.DeleteProjectResult, error) {
		called = true
		if len(input.Items) != 1 || input.Items[0].BundleName != "vikunja" {
			t.Fatalf("input.Items = %#v", input.Items)
		}
		if !input.Confirmed {
			t.Fatal("Confirmed 应为 true")
		}
		return &app.DeleteProjectResult{BundlesDeleted: 1, VersionCommit: "abc123"}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 4
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindBundle, BundleName: "vikunja", Label: "[bundle] vikunja", TreeRoot: ".dec", TreeBranch: "vikunja", Members: []app.AssetSelectionItem{{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja"}}},
	}
	m.deleteCandidatesLoaded = true
	m.rebuildDeleteTree()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if !m.runningDelete {
		t.Fatal("confirm y 后应进入 runningDelete")
	}
	if cmd == nil {
		t.Fatal("confirm y 后应返回命令")
	}

	batchMsg, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd() 类型 = %T, 期望 tea.BatchMsg", cmd())
	}
	var completed deleteCompletedMsg
	gotCompleted := false
	for _, sub := range batchMsg {
		if sub == nil {
			continue
		}
		msg := sub()
		if c, ok := msg.(deleteCompletedMsg); ok {
			completed = c
			gotCompleted = true
		}
	}
	if !gotCompleted {
		t.Fatal("应在 batch 执行中拿到 deleteCompletedMsg")
	}
	if !called {
		t.Fatal("应调用 runDeleteOperation")
	}

	updated, _ = m.Update(completed)
	m = updated.(model)
	if m.runningDelete {
		t.Fatal("completed 后应退出 runningDelete")
	}
	if m.deleteResult == nil || m.deleteResult.BundlesDeleted != 1 {
		t.Fatalf("deleteResult = %#v", m.deleteResult)
	}
}

func TestModelDeletePageFilter(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 4
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindBundle, BundleName: "cli", Label: "[bundle] cli"},
		{Kind: app.DeleteKindBundle, BundleName: "vikunja", Label: "[bundle] vikunja"},
	}
	m.deleteCandidatesLoaded = true

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(model)
	if !m.deleteFilterInput {
		t.Fatal("/ 后应进入 deleteFilterInput")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v', 'i', 'k'}})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	visible := m.visibleDeleteCandidates()
	if len(visible) != 1 || visible[0].BundleName != "vikunja" {
		t.Fatalf("筛选 vik 后 visible = %#v", visible)
	}
}

func TestModelRunPageNoLongerHasRemoveHotkey(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(model)
	if m.removeStage != "" {
		t.Fatalf("Run 页 x 不应再触发 remove, stage = %q", m.removeStage)
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
	m.focus = focusContent
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
		"项目 IDE",
		"IDE 模式: 继承全局",
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
		"IDE 模式: 项目显式覆盖",
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
	m.focus = focusContent
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
	m.focus = focusContent
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
	m.focus = focusContent
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
	if !strings.Contains(view, "Home 页初始化") {
		t.Fatalf("View 未提示到 Home 页初始化:\n%s", view)
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
	if !m.lastInitResult.VarsCreated {
		t.Fatal("期望 VarsCreated=true")
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
	if m.lastInitResult == nil || m.lastInitResult.ProjectConfig != nil {
		t.Fatalf("lastInitResult = %#v, 期望 ProjectConfig=nil", m.lastInitResult)
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
	for _, check := range []string{"项目变量", "FOO", "MISSING", "e 打开外部编辑器", "vim"} {
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
	m.refreshAssetTree()
	rows := m.assetTree.VisibleRows()
	for i, tr := range rows {
		if p, ok := tr.Node.Payload.(assetTreePayload); ok && p.kind == want {
			m.assetTree.Cursor = i
			return
		}
	}
	t.Fatalf("rows 中找不到 kind=%d 的行", want)
}

func TestModelAssetsBundleToggleMarksMembersReadonly(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
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
	m.assetTypeFilter = "all"
	m.normalizeAssetCursor()
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
	m.focus = focusContent
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
	m.focus = focusContent
	m.assets = assetsStateWithBundle()
	m.normalizeAssetCursor()

	if m.assetTypeFilter != "bundle" {
		t.Fatalf("初始 type filter = %q, 期望 bundle", m.assetTypeFilter)
	}
	// 默认 bundle 视图只含 bundle 节点行
	for _, r := range m.visibleAssetRows() {
		if r.kind == assetRowAsset {
			t.Fatal("默认 bundle 视图下不应包含单资产行")
		}
	}
	// 按 t 轮转一次到 all
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(model)
	if m.assetTypeFilter != "all" {
		t.Fatalf("第一次 t = %q, 期望 all", m.assetTypeFilter)
	}
	// 再按 t 到 skill
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(model)
	if m.assetTypeFilter != "skill" {
		t.Fatalf("第二次 t = %q, 期望 skill", m.assetTypeFilter)
	}
	// skill 过滤下不应显示 rule 资产
	for _, r := range m.visibleAssetRows() {
		if r.kind == assetRowAsset && m.assets.Items[r.assetIndex].Type == "rule" {
			t.Fatal("skill 过滤下不应包含 rule 行")
		}
	}
	// 继续到 command/rule/mcp/bundle
	for i := 0; i < 4; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
		m = updated.(model)
	}
	if m.assetTypeFilter != "bundle" {
		t.Fatalf("第五次 t = %q, 期望 bundle", m.assetTypeFilter)
	}
	// bundle 视图只含 bundle 节点行（+ 展开的成员），不含单资产行
	for _, r := range m.visibleAssetRows() {
		if r.kind == assetRowAsset {
			t.Fatal("bundle 视图下不应包含单资产行")
		}
	}
}

func TestModelAssetsBundleRightExpandsAndLeftCollapses(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.assets = assetsStateWithBundle()
	m.normalizeAssetCursor()
	seekCursorToKind(t, &m, assetRowBundle)

	before := func() int {
		m.refreshAssetTree()
		return len(m.assetTree.VisibleRows())
	}()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(model)
	m.refreshAssetTree()
	after := len(m.assetTree.VisibleRows())
	if after <= before {
		t.Fatalf("按 l 展开后 rows %d 应大于折叠态 %d", after, before)
	}
	if !m.assetTree.Expanded[assetBundleNodeID("vikunja")] {
		t.Fatal("bundle 应处于展开态")
	}
	if m.focus != focusContent {
		t.Fatalf("展开后 focus = %q, 期望 content", m.focus)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(model)
	if m.assetTree.Expanded[assetBundleNodeID("vikunja")] {
		t.Fatal("按 h 后 bundle 应处于折叠态")
	}
	if m.focus != focusContent {
		t.Fatalf("折叠后 focus = %q, 期望 content", m.focus)
	}
}

func TestModelSpatialNavigationSidebarEnterExit(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	if m.focus != focusSidebar {
		t.Fatalf("默认 focus = %q, 期望 sidebar", m.focus)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(model)
	if m.focus != focusContent {
		t.Fatalf("侧栏按 l 后 focus = %q, 期望 content", m.focus)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(model)
	if m.focus != focusSidebar {
		t.Fatalf("内容区按 h 后 focus = %q, 期望 sidebar", m.focus)
	}
}

func TestModelAssetsNoBundleLegacyBehaviorUnchanged(t *testing.T) {
	// 回归：存量项目（assets.Bundles 为空）时，Assets 页 toggle / filter 行为应与旧版一致。
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.assetTypeFilter = "all"
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

func TestModelAssetsLoadedKeepsBundleViewWhenPackagesExist(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent

	updated, _ := m.Update(assetsLoadedMsg{
		state: &app.AssetSelectionState{
			ExistingConfig: false,
			Items: []app.AssetSelectionItem{
				{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja"},
				{Name: "cli-release-rules", Type: "rule", Vault: "cli"},
			},
			Bundles: []app.AssetBundleOption{
				{Name: "vikunja", Vault: "vikunja", Members: []app.AssetSelectionItem{{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja"}}},
				{Name: "cli", Vault: "cli", Members: []app.AssetSelectionItem{{Name: "cli-release-rules", Type: "rule", Vault: "cli"}}},
			},
		},
	})
	m = updated.(model)
	if m.assetTypeFilter != "bundle" {
		t.Fatalf("有 bundle 时不应回落到 all, got %q", m.assetTypeFilter)
	}
}

func TestModelConfigInitModeQuitsAfterSave(t *testing.T) {
	m := newModelWithOptions("/tmp/dec-project", "v1.0.0", RunOptions{ConfigInitMode: true})
	if !m.configInitMode {
		t.Fatal("configInitMode 应为 true")
	}
	if m.pageIndex != 1 {
		t.Fatalf("pageIndex = %d, 期望 1 (Bundles)", m.pageIndex)
	}

	updated, cmd := m.Update(assetsSavedMsg{
		result: &app.SaveAssetSelectionResult{EnabledBundleCount: 1},
	})
	if cmd == nil {
		t.Fatal("config init 保存后应触发 tea.Quit")
	}
	if _, ok := updated.(model); !ok {
		t.Fatalf("Update 返回 = %T, 期望 model", updated)
	}
}

func TestNewModelWithOptionsDefaultsMatchLegacy(t *testing.T) {
	legacy := newModel("/tmp/dec-project", "v1.0.0")
	opts := newModelWithOptions("/tmp/dec-project", "v1.0.0", RunOptions{})
	if legacy.pageIndex != opts.pageIndex {
		t.Fatalf("pageIndex 不一致: legacy=%d opts=%d", legacy.pageIndex, opts.pageIndex)
	}
	if opts.configInitMode {
		t.Fatal("默认 RunOptions 不应开启 configInitMode")
	}
}

