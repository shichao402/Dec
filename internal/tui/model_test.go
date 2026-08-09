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
		AvailableBundleCount: 2,
		EnabledBundleCount:   1,
		Bundles: []app.BundleOverview{
			{Name: "default", VaultName: "default", Enabled: true},
			{Name: "cli", VaultName: "cli"},
		},
		IDEs:   []string{"codex", "cursor"},
		Editor: "code --wait",
	}})
	m = updated.(model)

	view := m.View()
	checks := []string{
		"Dec Shell",
		"Home",
		"Bundles",
		"项目名:",
		"git@github.com:demo/dec.git",
		"Vault bundle: 2 个 | 已启用: 1 个",
		"IDE: codex, cursor",
		"编辑器: code --wait",
		"下一步",
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

	gen := m.shellRefresh.beginParts(1)
	msg := loadOverviewCmd("/Users/firo/workspace/Dec", gen)()
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
	if !m.vaultApplyLoad.busy() {
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
	if cmd == nil {
		t.Fatal("按 n 且未初始化 config 时应触发本地 project 生成")
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
	if !strings.Contains(got, "Bundles") {
		t.Fatalf("dismiss 后建议下一步应指向 Bundles 页: %q", got)
	}
}

func TestModelAssetsPageRendersSelectionState(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.width = 110
	m.height = 32
	m.assets = &app.AssetSelectionState{
		ExistingConfig: true,
		ConfigPath:     "/tmp/dec-project/.dec/config.yaml",
		VarsPath:       "/tmp/dec-project/.dec/vars.yaml",
		Bundles: []app.AssetBundleOption{
			{
				Name:    "default",
				Vault:   "default",
				Enabled: true,
				Members: []app.AssetSelectionItem{{Name: "project-workflow", Type: "skill", Vault: "default"}},
			},
			{
				Name:    "cli",
				Vault:   "cli",
				Members: []app.AssetSelectionItem{{Name: "cli-release-rules", Type: "rule", Vault: "cli"}},
			},
		},
	}
	m.bundleSelection = []string{"default"}
	m.normalizeAssetCursor()

	view := m.View()
	checks := []string{
		"Bundle 列表",
		"Details",
		"[x] ▸ default · 1 个成员",
		"[ ] ▸ cli · 1 个成员",
		"1/2 已启用",
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
		"上次结果",
		"Pull  请求 2 · 成功 1 · 失败 1",
		"IDE   cursor",
		"Commit abc123",
		"事件",
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
	m.assets = &app.AssetSelectionState{
		Bundles: []app.AssetBundleOption{
			{
				Name:    "default",
				Vault:   "default",
				Members: []app.AssetSelectionItem{{Name: "project-workflow", Type: "skill", Vault: "default"}},
			},
		},
	}
	m.normalizeAssetCursor()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if !m.bundleSelected("default") {
		t.Fatal("space 应切换当前 bundle 为已勾选")
	}
	if !m.assetsDirty {
		t.Fatal("切换 bundle 后应标记为 dirty")
	}
}

func TestModelFilterInputNarrowsAssets(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.assets = &app.AssetSelectionState{
		Bundles: []app.AssetBundleOption{
			{Name: "default", Vault: "default"},
			{Name: "cli", Vault: "cli"},
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

	rows := m.visibleAssetRows()
	if len(rows) != 1 {
		t.Fatalf("筛选后可见行数 = %d, 期望 1", len(rows))
	}
	if got := m.assets.Bundles[rows[0].bundleIndex].Name; got != "cli" {
		t.Fatalf("筛选命中 bundle = %q, 期望 %q", got, "cli")
	}
}

func TestModelAssetsPageDoesNotLeavePageWithoutVisibleAssets(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.assets = &app.AssetSelectionState{
		Bundles: []app.AssetBundleOption{{Name: "default", Vault: "default"}},
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
			if summary := m.currentSummary(); summary != "Pull running… Esc cancel" {
				t.Fatalf("currentSummary() = %q, 期望 %q", summary, "Pull running… Esc cancel")
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
			SecretsTargetCount: 1,
			DecHasChanges:      true,
			DecCandidateCount:  3,
		}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	m = updated.(model)
	if m.pushStage != "loading" {
		t.Fatalf("pushStage = %q, 期望 loading", m.pushStage)
	}
	if cmd == nil {
		t.Fatal("按 P 后应返回预览加载命令")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if m.pushStage != "summary" {
		t.Fatalf("预览完成后 pushStage = %q, 期望 summary", m.pushStage)
	}
	if m.pushPreview == nil {
		t.Fatal("预览完成后应有 pushPreview")
	}
	if m.runningPull {
		t.Fatal("按 P 后不应直接进入 push 执行")
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

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("P 后应返回预览 cmd")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if m.pushStage != "summary" {
		t.Fatalf("P 预览完成后 stage = %q, 期望 summary", m.pushStage)
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

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("P 后应返回预览 cmd")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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
		ConfigPath:             "/tmp/.dec/config.yaml",
		VarsPath:               "/tmp/.dec/local/vars.yaml",
		RepoConnected:          true,
		RepoURL:                "git@github.com:demo/dec.git",
		ConnectedRepoURL:       "git@github.com:demo/dec.git",
		AvailableIDEs:          []string{"codex", "cursor"},
		SelectedIDEs:           []string{"cursor"},
		EffectiveIDEs:          []string{"cursor"},
		AvailableSecretBundles: []string{"woa"},
		UserEnabledBundles:     []string{"woa"},
		SecretsConfigPath:      "/tmp/.dec/secrets/config.yaml",
	}
	m.settingsRepoInput = m.settings.RepoURL
	m.settingsSelectedIDEs = []string{"cursor"}
	m.settingsSelectedSecretBundles = []string{"woa"}
	m.normalizeSettingsCursor()

	view := m.View()
	checks := []string{
		"Global Settings",
		"Repo URL:",
		"当前远端:",
		"[x] cursor",
		"[ ] codex",
		"本机启用的 bundles",
		"[x] woa",
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

func TestModelSettingsHotkeysToggleUserSecretBundle(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 5
	m.focus = focusContent
	m.settings = &app.GlobalSettingsState{
		RepoURL:                "git@github.com:demo/dec.git",
		AvailableIDEs:          []string{"cursor"},
		SelectedIDEs:           []string{"cursor"},
		AvailableSecretBundles: []string{"woa"},
	}
	m.settingsRepoInput = m.settings.RepoURL
	m.settingsSelectedIDEs = []string{"cursor"}
	m.settingsCursor = 2 // repo(0) + IDE(1) + first secret bundle

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if !settingsContainsIDE(m.settingsSelectedSecretBundles, "woa") {
		t.Fatalf("space 应启用 user secret bundle, 实际: %#v", m.settingsSelectedSecretBundles)
	}
	if !m.settingsDirty {
		t.Fatal("切换 user secret bundle 后应 dirty")
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
		if input.UserEnabledBundles == nil {
			t.Fatal("UserEnabledBundles 不应为 nil")
		}
		if len(input.UserEnabledBundles) != 1 || input.UserEnabledBundles[0] != "woa" {
			t.Fatalf("UserEnabledBundles = %#v, 期望 [woa]", input.UserEnabledBundles)
		}
		return &app.SaveGlobalSettingsResult{IDEs: []string{"cursor"}, UserEnabledBundles: []string{"woa"}}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 5
	m.settings = &app.GlobalSettingsState{
		RepoURL:            "git@github.com:demo/dec.git",
		AvailableIDEs:      []string{"cursor"},
		SelectedIDEs:       []string{"cursor"},
		UserEnabledBundles: []string{"woa"},
	}
	m.settingsRepoInput = m.settings.RepoURL
	m.settingsSelectedIDEs = []string{"cursor"}
	m.settingsSelectedSecretBundles = []string{"woa"}

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
		if input.UserEnabledBundles == nil {
			t.Fatal("UserEnabledBundles 不应为 nil")
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
	const flow = "Settings 连仓库 → Home 确认/生成本地 project → Bundles 勾选 → Run pull"
	if got := suggestNextAction(&app.ProjectOverview{}, false, false); !strings.Contains(got, flow) || !strings.Contains(got, "Settings") {
		t.Fatalf("未连接仓库时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true}, false, false); !strings.Contains(got, flow) || !strings.Contains(got, "Home") {
		t.Fatalf("未初始化项目时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true}, true, false); !strings.Contains(got, "确认 vault project") {
		t.Fatalf("推断待确认时建议动作错误: %q", got)
	}
	if got := suggestNextAction(&app.ProjectOverview{RepoConnected: true, ProjectConfigReady: true}, false, false); !strings.Contains(got, "Bundles") {
		t.Fatalf("无已启用 bundle 时建议动作错误: %q", got)
	}
	ready := &app.ProjectOverview{
		RepoConnected:      true,
		ProjectConfigReady: true,
		EnabledBundleCount: 1,
		Bundles:            []app.BundleOverview{{Name: "default", VaultName: "default", Enabled: true}},
	}
	if got := suggestNextAction(ready, false, false); !strings.Contains(got, "Run") {
		t.Fatalf("enabled_bundles 非空时应建议 Run 页: %q", got)
	}
}

// Run 页 pull 计划：有启用 bundle 时列出将拉取的内容，没有时给出警告。
func TestRenderPullPlanLines(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.assets = assetsStateWithBundle()
	m.assets.Bundles[0].Enabled = true
	m.bundleSelection = []string{"vikunja"}

	lines := strings.Join(m.renderPullPlanLines(), "\n")
	if !strings.Contains(lines, "本次 Pull 计划") {
		t.Fatalf("缺少 pull 计划标题: %q", lines)
	}
	if !strings.Contains(lines, "vikunja") {
		t.Fatalf("pull 计划应列出已启用 bundle: %q", lines)
	}
	if strings.Contains(lines, "无启用 bundle") {
		t.Fatalf("有启用 bundle 时不应显示警告: %q", lines)
	}

	m.assets.Bundles[0].Enabled = false
	m.bundleSelection = nil
	lines = strings.Join(m.renderPullPlanLines(), "\n")
	if !strings.Contains(lines, "无启用 bundle") {
		t.Fatalf("无启用 bundle 时应显示警告: %q", lines)
	}
}

// Bundles 页勾选未保存时，Run 页应提示差异，避免用户以为 pull 会带上新勾选。
func TestRenderPullPlanWarnsUnsavedSelection(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.assets = assetsStateWithBundle()
	m.assets.Bundles[0].Enabled = true
	m.bundleSelection = []string{"cli"}
	m.assetsDirty = true

	lines := strings.Join(m.renderPullPlanLines(), "\n")
	if !strings.Contains(lines, "未保存的勾选") {
		t.Fatalf("应提示未保存勾选: %q", lines)
	}
	if !strings.Contains(lines, "+cli") || !strings.Contains(lines, "-vikunja") {
		t.Fatalf("未保存差异描述不完整: %q", lines)
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
			Kind: app.DeleteKindSecret, Label: "[secret] env/vikunja.env",
			SecretPath: "env/vikunja.env", LocalRoot: ".secrets/bundles/vikunja", SecretsBundle: "vikunja",
			TreeRoot: "secrets", TreeBranch: "vikunja", GroupOrder: 0, GroupTitle: "vikunja (bundle)",
		},
		{
			Kind: app.DeleteKindSSHKey, Label: "[ssh] deploy",
			SSHKeyName: "deploy", DecBundleName: "vikunja", SecretsBundle: "vikunja",
			LocalRoot: ".secrets/bundles/vikunja", TreeRoot: "secrets", TreeBranch: "vikunja", GroupOrder: 0, GroupTitle: "vikunja (bundle)",
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
		"▾ secrets (SyncTarget)",
		".secrets/bundles/vikunja",
		"vikunja.env",
		"SSH · machine",
		"[ssh] deploy",
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

func TestModelDeletePageTabSwitchDoesNotReloadCandidates(t *testing.T) {
	oldList := listDeleteCandidatesOperation
	defer func() { listDeleteCandidatesOperation = oldList }()

	calls := 0
	listDeleteCandidatesOperation = func(ctx context.Context, projectRoot string, includeRemote bool, reporter app.Reporter) ([]app.DeleteCandidate, error) {
		calls++
		return []app.DeleteCandidate{
			{Kind: app.DeleteKindBundle, BundleName: "cli", Label: "[bundle] cli"},
		}, nil
	}

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3 // Run
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("首次进入 Delete 应触发加载")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if !m.deleteCandidatesLoaded || calls != 1 {
		t.Fatalf("loaded=%v calls=%d", m.deleteCandidatesLoaded, calls)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // Delete -> Settings
	m = updated.(model)
	if cmd != nil {
		t.Fatalf("离开 Delete 不应触发加载, cmd = %T", cmd)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // Settings -> Home
	m = updated.(model)
	if cmd != nil {
		t.Fatalf("Home 不应触发 Delete 加载, cmd = %T", cmd)
	}

	// Home -> Bundles -> Project -> Run -> Delete
	for i := 0; i < 4; i++ {
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(model)
	}
	if cmd != nil {
		t.Fatalf("再次进入 Delete 且已加载时不应重复加载, cmd = %T", cmd)
	}
	if calls != 1 {
		t.Fatalf("ListDeleteCandidates 调用次数 = %d, 期望 1", calls)
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

	// 勾选两个 bundle 叶子（勿用根目录 space，那会级联全选/全消）。
	for _, wantLabel := range []string{"[bundle] cli", "[bundle] vikunja"} {
		found := false
		rows := m.deleteTree.VisibleRows()
		for i, row := range rows {
			if row.Node.SelectMode == TreeSelectLeaf && strings.Contains(row.Node.Label, wantLabel) {
				m.deleteTree.Cursor = i
				if !m.deleteTree.ToggleSelectAtCursor() {
					t.Fatalf("未能勾选 %s", wantLabel)
				}
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("树中找不到叶子 %s", wantLabel)
		}
	}
	if len(m.selectedDeleteItems()) != 2 {
		t.Fatalf("应选中 2 项, 实际 %d", len(m.selectedDeleteItems()))
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
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
	updateDoUpdateOperation = func(currentVersion, latestVersion string) error {
		called = true
		if currentVersion != "v1.0.0" {
			t.Fatalf("currentVersion = %q, 期望 %q", currentVersion, "v1.0.0")
		}
		if latestVersion != "v1.2.0" {
			t.Fatalf("latestVersion = %q, 期望 %q", latestVersion, "v1.2.0")
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
	updateDoUpdateOperation = func(currentVersion, latestVersion string) error {
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
	oldMirror := updateMirrorInstallCommand
	defer func() {
		updateManualInstallCommand = oldCmd
		updateMirrorInstallCommand = oldMirror
	}()
	updateManualInstallCommand = func() string { return "curl -fsSL example.com | bash" }
	updateMirrorInstallCommand = func() string { return "curl -fsSL mirror.example.com | bash" }

	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 3
	m.width = 120
	m.height = 32
	m.updateStage = "done"
	m.updateErr = errors.New("download failed")

	view := m.View()
	checks := []string{"更新失败", "curl -fsSL example.com | bash", "curl -fsSL mirror.example.com | bash"}
	for _, check := range checks {
		if !strings.Contains(view, check) {
			t.Fatalf("失败视图缺少 %q:\n%s", check, view)
		}
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
	oldEnsure := ensureLocalProjectConfigOperation
	defer func() { ensureLocalProjectConfigOperation = oldEnsure }()

	called := false
	ensureLocalProjectConfigOperation = func(projectRoot string, reporter app.Reporter) (*app.ConfigInitPreparation, error) {
		called = true
		if projectRoot != "/tmp/dec-project" {
			t.Fatalf("ProjectRoot = %q", projectRoot)
		}
		return &app.ConfigInitPreparation{
			ProjectRoot:    projectRoot,
			ExistingConfig: false,
			VarsCreated:    true,
			ProjectConfig:  &types.ProjectConfig{ProjectName: "dec-project"},
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
	if !m.localProjectLoad.busy() {
		t.Fatal("按 i 后应进入 localProjectLoad busy 状态")
	}
	if cmd == nil {
		t.Fatal("按 i 后应返回 tea.Cmd")
	}
	msg := cmd()
	if _, ok := msg.(localProjectEnsuredMsg); !ok {
		t.Fatalf("cmd 返回 = %T, 期望 localProjectEnsuredMsg", msg)
	}
	if !called {
		t.Fatal("应调用 ensureLocalProjectConfigOperation")
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
	if !m.projectInitLoad.busy() {
		t.Fatal("按 R 后应进入 projectInitLoad busy 状态")
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

func TestModelProjectPageInitWorksWithoutRepoConnected(t *testing.T) {
	oldEnsure := ensureLocalProjectConfigOperation
	defer func() { ensureLocalProjectConfigOperation = oldEnsure }()
	ensureLocalProjectConfigOperation = func(projectRoot string, reporter app.Reporter) (*app.ConfigInitPreparation, error) {
		return &app.ConfigInitPreparation{ProjectRoot: projectRoot, ProjectConfig: &types.ProjectConfig{}}, nil
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
	if !m.localProjectLoad.busy() {
		t.Fatal("未连仓库下按 i 仍应生成本地 project")
	}
	if cmd == nil {
		t.Fatal("按 i 应返回 ensureLocalProjectCmd")
	}
	view := m.View()
	if !strings.Contains(view, "按 i 在本页生成本地 project") {
		t.Fatalf("View 应提示在本页初始化:\n%s", view)
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
	gen := m.projectInitLoad.beginGen()
	updated, _ := m.Update(projectConfigInitializedMsg{
		result: &app.ConfigInitPreparation{
			ExistingConfig: false,
			AssetCount:     7,
			VarsCreated:    true,
			ProjectConfig:  &types.ProjectConfig{},
		},
		err:     nil,
		loadGen: gen,
	})
	m = updated.(model)
	if m.projectInitLoad.busy() {
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

// 构造一个带 2 个 bundle 的 Bundles 页状态：vikunja（2 成员）+ cli（1 成员）。
func assetsStateWithBundle() *app.AssetSelectionState {
	return &app.AssetSelectionState{
		ExistingConfig: true,
		ConfigPath:     "/tmp/dec-project/.dec/config.yaml",
		VarsPath:       "/tmp/dec-project/.dec/vars.yaml",
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
			{
				Name:    "cli",
				Vault:   "cli",
				Enabled: false,
				Members: []app.AssetSelectionItem{
					{Name: "solo-rule", Type: "rule", Vault: "cli"},
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

func TestModelAssetsBundleToggleMarksDirty(t *testing.T) {
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
	if m.bundleSelected("cli") {
		t.Fatal("不应牵连其他 bundle")
	}
}

func TestModelAssetsBundleUnselectRemovesOnlyThatBundle(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.assets = assetsStateWithBundle()
	m.bundleSelection = []string{"vikunja", "cli"}
	m.normalizeAssetCursor()

	seekCursorToKind(t, &m, assetRowBundle)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)

	if m.bundleSelected("vikunja") {
		t.Fatal("bundle 应被取消")
	}
	if !m.bundleSelected("cli") {
		t.Fatal("其他 bundle 的勾选不应被牵连")
	}
}

// 成员行只读：光标停在展开后的成员上按空格，不应改变任何 bundle 勾选。
func TestModelAssetsMemberRowIsReadonly(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.assets = assetsStateWithBundle()
	m.bundleSelection = []string{"vikunja"}
	m.normalizeAssetCursor()
	m.refreshAssetTree()
	m.assetTree.DefaultExpandAll()

	seekCursorToKind(t, &m, assetRowBundleMember)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)

	if !m.bundleSelected("vikunja") || len(m.bundleSelection) != 1 {
		t.Fatalf("成员行按空格不应改动勾选，got %v", m.bundleSelection)
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

func TestModelAssetsEmptyBundlesRendersHint(t *testing.T) {
	// 回归：仓库里没有 bundle 时，Bundles 页不应崩，也不应有可勾选行。
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent
	m.assets = &app.AssetSelectionState{ExistingConfig: true}
	m.normalizeAssetCursor()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if len(m.bundleSelection) != 0 {
		t.Fatalf("无 bundle 时不应修改 bundleSelection, got %v", m.bundleSelection)
	}
	if got := len(m.visibleAssetRows()); got != 0 {
		t.Fatalf("rows = %d, 期望 0", got)
	}
}

func TestModelAssetsSaveCmdCarriesBundlesOnly(t *testing.T) {
	// saveAssetsCmd 只接受 bundle 短名列表：签名回退（重新引入单资产参数）会在此处编译失败。
	_ = saveAssetsCmd("/nonexistent", []string{"x"})
}

func TestModelAssetsLoadedKeepsBundleSelection(t *testing.T) {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.pageIndex = 1
	m.focus = focusContent

	updated, _ := m.Update(assetsLoadedMsg{
		state: &app.AssetSelectionState{
			ExistingConfig: true,
			Bundles: []app.AssetBundleOption{
				{Name: "vikunja", Vault: "vikunja", Enabled: true, Members: []app.AssetSelectionItem{{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja"}}},
				{Name: "cli", Vault: "cli", Members: []app.AssetSelectionItem{{Name: "cli-release-rules", Type: "rule", Vault: "cli"}}},
			},
		},
	})
	m = updated.(model)
	if !m.bundleSelected("vikunja") {
		t.Fatal("已启用 bundle 应回填到 bundleSelection")
	}
	if m.bundleSelected("cli") {
		t.Fatal("未启用 bundle 不应被勾选")
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
		result: &app.SaveBundleSelectionResult{EnabledBundleCount: 1},
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
