package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shichao402/Dec/pkg/app"
	"github.com/shichao402/Dec/pkg/editor"
	"github.com/shichao402/Dec/pkg/update"
)

var (
	shellTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	shellMutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	shellCardStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("67")).Padding(1, 2)
	shellActiveNav   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	shellNavStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	shellStatusBar   = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	shellLogStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	shellWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	shellGoodStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Bold(true)
	shellSelectedRow = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Bold(true)
	shellEnabledRow  = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
)

type overviewLoadedMsg struct {
	overview       *app.ProjectOverview
	err            error
	vaultInference *app.VaultProjectInference
}

type vaultProjectAppliedMsg struct {
	result *app.VaultProjectAutoApplyResult
	err    error
}

type assetsLoadedMsg struct {
	state *app.AssetSelectionState
	err   error
}

type assetsSavedMsg struct {
	result *app.SaveBundleSelectionResult
	err    error
}

type settingsLoadedMsg struct {
	state *app.GlobalSettingsState
	err   error
}

type settingsSavedMsg struct {
	result *app.SaveGlobalSettingsResult
	err    error
}

type builtinAssetsEnsuredMsg struct {
	warnings []string
}

type projectSettingsLoadedMsg struct {
	state *app.ProjectSettingsState
	err   error
}

type projectSettingsSavedMsg struct {
	result *app.SaveProjectSettingsResult
	err    error
}

type projectConfigInitializedMsg struct {
	result *app.ConfigInitPreparation
	err    error
}

type projectVarsLoadedMsg struct {
	view *app.ProjectVarsView
	err  error
}

type projectVarsEditedMsg struct {
	err error
}

// focusContext 描述 TUI 的空间导航焦点层级。
// 左右键（h/l）在该层级间进入/退出；上下键（j/k）在同级内移动。
//   - focusSidebar：侧栏导航（Home/Assets/...），j/k 切换页，l 进入内容区
//   - focusContent：当前页内容列表，j/k 移动光标，h 退回侧栏，l 进入更深（如展开 bundle）
//   - focusBundleExpanded：bundle 已展开，j/k 在成员间移动，h 折叠并退回内容区
type focusContext string

const (
	focusSidebar        focusContext = "sidebar"
	focusContent        focusContext = "content"
	focusBundleExpanded focusContext = "bundleExpanded"
)

type runEventMsg struct {
	event app.OperationEvent
}

type runCompletedMsg struct {
	result     *app.PullProjectAssetsResult
	pushResult *app.PushProjectAssetsResult
	err        error
}

type removeEventMsg struct {
	event app.OperationEvent
}

type removeCompletedMsg struct {
	result *app.RemoveBundleResult
	err    error
}

type updateCheckedMsg struct {
	result *update.CheckResult
	err    error
}

type updateDoneMsg struct {
	targetVersion string
	err           error
}

var runPullOperation = func(ctx context.Context, projectRoot string, reporter app.Reporter) (*app.PullProjectAssetsResult, error) {
	return app.PullProjectAssets(ctx, projectRoot, "", reporter)
}

var runPushOperation = func(ctx context.Context, projectRoot string, reporter app.Reporter) (*app.PushProjectAssetsResult, error) {
	return app.PushProjectAssets(ctx, projectRoot, reporter)
}

var runRemoveOperation = func(input app.RemoveBundleInput, reporter app.Reporter) (*app.RemoveBundleResult, error) {
	return app.RemoveBundle(input, reporter)
}

var previewPushOperation = func(projectRoot string) (*app.PushProjectAssetsPreview, error) {
	return app.PreviewPushProjectAssets(projectRoot)
}

var loadGlobalSettingsOperation = func(reporter app.Reporter) (*app.GlobalSettingsState, error) {
	return app.LoadGlobalSettings(reporter)
}

var saveGlobalSettingsOperation = func(input app.SaveGlobalSettingsInput, reporter app.Reporter) (*app.SaveGlobalSettingsResult, error) {
	return app.SaveGlobalSettings(input, reporter)
}

var ensureBuiltinIDEAssetsOperation = func(ideNames []string, reporter app.Reporter) []string {
	return app.EnsureBuiltinIDEAssets(ideNames, reporter)
}

var loadProjectSettingsOperation = func(projectRoot string, reporter app.Reporter) (*app.ProjectSettingsState, error) {
	return app.LoadProjectSettings(projectRoot, reporter)
}

var saveProjectSettingsOperation = func(input app.SaveProjectSettingsInput, reporter app.Reporter) (*app.SaveProjectSettingsResult, error) {
	return app.SaveProjectSettings(input, reporter)
}

var prepareProjectConfigInitOperation = func(projectRoot string, reporter app.Reporter) (*app.ConfigInitPreparation, error) {
	return app.PrepareProjectConfigInit(projectRoot, reporter)
}

var inferVaultProjectOperation = func(projectRoot string, reporter app.Reporter) (*app.VaultProjectInference, error) {
	return app.InferVaultProject(projectRoot, reporter)
}

var applyVaultProjectOperation = func(projectRoot string, reporter app.Reporter) (*app.VaultProjectAutoApplyResult, error) {
	return app.ApplyVaultProject(projectRoot, reporter)
}

var loadProjectVarsViewOperation = func(projectRoot string) (*app.ProjectVarsView, error) {
	return app.LoadProjectVarsView(projectRoot)
}

var ensureProjectVarsFileOperation = func(projectRoot string) (*app.EnsureProjectVarsFileResult, error) {
	return app.EnsureProjectVarsFile(projectRoot)
}

var updateCheckOperation = func(currentVersion string) (*update.CheckResult, error) {
	return update.Check(currentVersion)
}

var updateDoUpdateOperation = func(currentVersion, latestVersion string) error {
	return update.DoUpdate(currentVersion, latestVersion)
}

var updateManualInstallCommand = func() string {
	return update.ManualInstallCommand()
}

var updateMirrorInstallCommand = func() string {
	return update.MirrorInstallCommand()
}

type model struct {
	projectRoot    string
	currentVersion string
	pages          []string
	pageIndex      int
	width          int
	height         int
	overview       *app.ProjectOverview
	overviewErr    error
	assets         *app.AssetSelectionState
	assetsErr      error
	settings       *app.GlobalSettingsState
	settingsErr    error
	logs           []string
	assetTree      TreeList
	// bundleSelection 是 TUI 内对 ProjectConfig.EnabledBundles 的镜像。
	// 进入 Assets 页后：从 assets.Bundles[i].Enabled==true 初始化；保存时随 Items 一起传给 SaveAssetSelection。
	bundleSelection  []string
	assetFilter      string
	assetFilterInput bool
	assetsDirty      bool
	savingAssets     bool
	// "bundle" 表示只显示 bundle 节点（以及它们展开的成员）；其他值只影响单资产行的过滤。
	settingsCursor              int
	settingsDirty               bool
	savingSettings              bool
	settingsRepoInput           string
	settingsRepoEditing         bool
	settingsSelectedIDEs        []string
	projectSettings             *app.ProjectSettingsState
	projectSettingsErr          error
	projectSettingsCursor       int
	projectSettingsDirty        bool
	savingProjectSettings       bool
	projectSettingsOverride     bool
	projectSettingsSelectedIDEs []string
	initializingProjectConfig   bool
	lastInitResult              *app.ConfigInitPreparation
	lastInitErr                 error
	projectVars                 *app.ProjectVarsView
	projectVarsErr              error
	lastEditErr                 error
	runningPull                 bool
	runProgress                 *app.Progress
	runEvents                   []string
	runPinLine                  string
	runShowHelp                 bool
	runResult                   *app.PullProjectAssetsResult
	pushResult                  *app.PushProjectAssetsResult
	runErr                      error
	runStream                   <-chan tea.Msg
	runCtx                      context.Context
	runCancel                   context.CancelFunc
	runMode                     string // "pull" | "push" | "remove" | "update"
	removeStage                 string // "", "select", "confirm", "running"
	removeCursor                int
	removeFilter                string
	removeFilterInput           bool
	removeTarget                *app.AssetBundleOption
	runningRemove               bool
	removeResult                *app.RemoveBundleResult
	removeErr                   error
	pushStage                   string // "", "summary", "confirm", "running"
	pushPreview                 *app.PushProjectAssetsPreview
	pushPreviewErr              error
	updateStage                 string // "", "checking", "result", "confirm", "running", "done"
	updateResult                *update.CheckResult
	updateErr                   error
	updateDoneVersion           string
	updatingBinary              bool
	deleteCandidates            []app.DeleteCandidate
	deleteTree                  TreeList
	deleteFilter                string
	deleteFilterInput           bool
	deleteStage                 string // "", "list", "summary", "confirm", "running"
	deleteCandidatesLoaded      bool
	loadingDeleteCandidates     bool
	deleteLoadErr               error
	deleteLoadCancel            context.CancelFunc
	deleteLoadGen               uint64
	deleteIncludeRemote         bool
	runningDelete               bool
	deleteResult                *app.DeleteProjectResult
	deleteErr                   error
	// configInitMode 为 true 时表示由 dec config init 拉起：聚焦 Assets/bundle 视图，保存后退出。
	configInitMode bool
	// vaultInference Home 页待确认的 vault project 推断（来自目录名匹配）。
	vaultInference *app.VaultProjectInference
	// vaultInferenceDismissed 用户本次会话内已拒绝推断，刷新前不再提示。
	vaultInferenceDismissed bool
	applyingVaultProject    bool
	// vaultAutoApplyNotice Home 页展示 vault 应用成功提示（仅本次会话内最近一次）。
	vaultAutoApplyNotice string
	// focus 是当前键盘交互上下文（侧栏 / 内容 / bundle 成员）。
	focus focusContext
	// addSecretStage 是 Project 页「登记新 secret」的阶段；空串表示流程未开启。
	addSecretStage       string
	addSecretPathInput   string
	addSecretFolderInput string
	addSecretFolders     []string
	addSecretFolderIdx   int
	addSecretResult      *app.AddSecretResult
	addSecretErr         error
}

func newModel(projectRoot, currentVersion string) model {
	return newModelWithOptions(projectRoot, currentVersion, RunOptions{})
}

func newModelWithOptions(projectRoot, currentVersion string, opts RunOptions) model {
	logs := []string{
		"TUI shell ready",
		"Loading project overview...",
		"Loading asset selection...",
		"Loading global settings...",
	}
	if opts.ConfigInitMode {
		logs = []string{
			"项目配置初始化",
			"选择 bundle 后按 s 保存并退出",
			"Loading asset selection...",
		}
	}
	m := model{
		projectRoot:     projectRoot,
		currentVersion:  currentVersion,
		pages:           []string{"Home", "Bundles", "Project", "Run", "Delete", "Settings"},
		configInitMode:  opts.ConfigInitMode,
		focus:           focusSidebar,
		logs:            logs,
	}
	if opts.ConfigInitMode {
		m.pageIndex = 1 // Bundles
		m.focus = focusContent
	}
	return m
}

func (m model) Init() tea.Cmd {
	return m.refreshCmd()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case deleteLoadedMsg:
		if msg.loadGen != m.deleteLoadGen {
			return m, nil
		}
		m.deleteLoadCancel = nil
		m.loadingDeleteCandidates = false
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.deleteCandidatesLoaded = true
		m.deleteIncludeRemote = msg.includeRemote
		m.deleteCandidates = msg.candidates
		m.deleteLoadErr = msg.err
		m.deleteTree = TreeList{}
		m.rebuildDeleteTree()
		for _, line := range msg.logs {
			m.pushLog(line)
		}
		if msg.err != nil {
			m.pushLog("Delete 列表加载失败: " + msg.err.Error())
		} else {
			m.pushLog(fmt.Sprintf("Delete 列表已加载：%d 项", len(msg.candidates)))
		}
		return m, nil
	case deleteEventMsg:
		m.recordRunEvent(msg.event)
		if m.runStream != nil {
			return m, waitRunMsg(m.runStream)
		}
		return m, nil
	case deleteCompletedMsg:
		m.runningDelete = false
		m.runStream = nil
		if m.runCancel != nil {
			m.runCancel()
			m.runCancel = nil
		}
		m.runCtx = nil
		m.deleteStage = ""
		m.deleteResult = msg.result
		m.deleteErr = msg.err
		m.deleteCandidatesLoaded = false
		if msg.err != nil {
			m.pushLog("Delete failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			m.pushLog(fmt.Sprintf("Delete finished: dec %d · secrets %d · ssh %d · bundle %d",
				msg.result.DecDeleted, msg.result.SecretsDeleted, msg.result.SSHKeysDeleted, msg.result.BundlesDeleted))
		}
		return m, tea.Batch(m.refreshCmd(), m.startDeleteCandidatesLoad(m.deleteIncludeRemote, true))
	case overviewLoadedMsg:
		m.overview = msg.overview
		m.overviewErr = msg.err
		m.vaultInference = msg.vaultInference
		if msg.vaultInference != nil {
			m.vaultInferenceDismissed = false
		}
		if msg.err != nil {
			m.pushLog("Overview load failed: " + msg.err.Error())
			return m, nil
		}
		if msg.vaultInference != nil {
			m.pushLog(fmt.Sprintf("Vault project inferred from directory: %s (%d bundles)", msg.vaultInference.ProjectName, len(msg.vaultInference.EnabledBundles)))
		}
		m.pushLog(fmt.Sprintf("Overview loaded: %d enabled / %d available bundles", msg.overview.EnabledBundleCount, msg.overview.AvailableBundleCount))
		return m, nil
	case vaultProjectAppliedMsg:
		m.applyingVaultProject = false
		if msg.err != nil {
			m.pushLog("Vault project apply failed: " + msg.err.Error())
			return m, nil
		}
		m.vaultInference = nil
		m.vaultInferenceDismissed = false
		if msg.result != nil && msg.result.Applied {
			name := msg.result.ProjectName
			m.vaultAutoApplyNotice = fmt.Sprintf("已从 vault 应用 project %s", name)
			m.pushLog(m.vaultAutoApplyNotice)
			if msg.result.VarsCreated {
				m.pushLog("Project vars template created: .dec/vars.yaml")
			}
		}
		return m, m.refreshCmd()
	case assetsLoadedMsg:
		m.assets = msg.state
		m.assetsErr = msg.err
		m.savingAssets = false
		m.assetsDirty = false
		if msg.err != nil {
			m.pushLog("Asset selection load failed: " + msg.err.Error())
			return m, nil
		}
		// 用磁盘上的 bundle 启用态重置本地镜像。Save 时会根据此列表（可能已被用户编辑过）写回。
		m.bundleSelection = nil
		if msg.state != nil {
			for _, bo := range msg.state.Bundles {
				if bo.Enabled {
					m.bundleSelection = append(m.bundleSelection, bo.Name)
				}
			}
		}
		m.normalizeAssetCursor()
		if msg.state != nil {
			m.pushLog(fmt.Sprintf("Bundle selection loaded: %d bundles", len(msg.state.Bundles)))
		}
		return m, nil
	case assetsSavedMsg:
		m.savingAssets = false
		if msg.err != nil {
			m.pushLog("Bundle selection save failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			m.pushLog(fmt.Sprintf("Bundle selection saved: %d bundles", msg.result.EnabledBundleCount))
		}
		if m.configInitMode {
			m.pushLog("项目配置已保存，退出初始化")
			return m, tea.Quit
		}
		return m, m.refreshCmd()
	case settingsLoadedMsg:
		m.settings = msg.state
		m.settingsErr = msg.err
		m.savingSettings = false
		m.settingsRepoEditing = false
		m.settingsDirty = false
		if msg.err != nil {
			m.pushLog("Global settings load failed: " + msg.err.Error())
			return m, nil
		}
		if msg.state != nil {
			m.settingsRepoInput = msg.state.RepoURL
			m.settingsSelectedIDEs = cloneStrings(msg.state.SelectedIDEs)
			m.normalizeSettingsCursor()
			m.syncSettingsDirty()
			m.pushLog(fmt.Sprintf("Global settings loaded: %d IDEs selected", len(m.settingsSelectedIDEs)))
			if msg.state.RepoConnected && len(m.settingsSelectedIDEs) > 0 {
				return m, ensureBuiltinIDEAssetsCmd(cloneStrings(m.settingsSelectedIDEs))
			}
		}
		return m, nil
	case builtinAssetsEnsuredMsg:
		for _, warning := range msg.warnings {
			m.pushLog("Install warning: " + warning)
		}
		if len(msg.warnings) == 0 {
			m.pushLog("Builtin IDE assets synced (skills + dec MCP)")
		}
		return m, nil
	case settingsSavedMsg:
		m.savingSettings = false
		if msg.err != nil {
			m.pushLog("Global settings save failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			m.pushLog(fmt.Sprintf("Global settings saved: %d IDEs", len(msg.result.IDEs)))
			for _, warning := range msg.result.InstallWarnings {
				m.pushLog("Install warning: " + warning)
			}
		}
		return m, m.refreshCmd()
	case projectSettingsLoadedMsg:
		m.projectSettings = msg.state
		m.projectSettingsErr = msg.err
		m.savingProjectSettings = false
		m.projectSettingsDirty = false
		if msg.err != nil {
			m.pushLog("Project settings load failed: " + msg.err.Error())
			return m, nil
		}
		if msg.state != nil {
			m.projectSettingsOverride = msg.state.OverrideActive
			m.projectSettingsSelectedIDEs = cloneStrings(msg.state.SelectedIDEs)
			m.normalizeProjectSettingsCursor()
			m.syncProjectSettingsDirty()
			if msg.state.OverrideActive {
				m.pushLog(fmt.Sprintf("Project settings loaded: %d IDE overrides", len(m.projectSettingsSelectedIDEs)))
			} else {
				m.pushLog("Project settings loaded: inheriting global IDEs")
			}
		}
		return m, nil
	case projectSettingsSavedMsg:
		m.savingProjectSettings = false
		if msg.err != nil {
			m.pushLog("Project settings save failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			if msg.result.OverrideActive {
				m.pushLog(fmt.Sprintf("Project settings saved: %d IDE overrides", len(msg.result.SelectedIDEs)))
			} else {
				m.pushLog("Project settings saved: cleared override, inheriting global")
			}
		}
		return m, m.refreshCmd()
	case projectConfigInitializedMsg:
		m.initializingProjectConfig = false
		m.lastInitResult = msg.result
		m.lastInitErr = msg.err
		if msg.err != nil {
			m.pushLog("Project config init failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			if msg.result.ProjectConfig == nil {
				m.pushLog(fmt.Sprintf("Project config init: 仓库暂无资产 (AssetCount=%d)", msg.result.AssetCount))
			} else if msg.result.ExistingConfig {
				m.pushLog(fmt.Sprintf("Project config refreshed: %d assets available", msg.result.AssetCount))
			} else {
				m.pushLog(fmt.Sprintf("Project config initialized: %d assets available", msg.result.AssetCount))
			}
			if msg.result.VarsCreated {
				m.pushLog("Project vars template created: .dec/vars.yaml")
			}
		}
		return m, m.refreshCmd()
	case projectVarsLoadedMsg:
		m.projectVars = msg.view
		m.projectVarsErr = msg.err
		if msg.err != nil {
			m.pushLog("Project vars load failed: " + msg.err.Error())
			return m, nil
		}
		if msg.view != nil {
			missing := msg.view.MissingPlaceholders()
			m.pushLog(fmt.Sprintf("Project vars loaded: %d used / %d missing", len(msg.view.UsedPlaceholders), len(missing)))
		}
		return m, nil
	case projectVarsEditedMsg:
		m.lastEditErr = msg.err
		if msg.err != nil {
			m.pushLog("Editor exited with error: " + msg.err.Error())
		} else {
			m.pushLog("Editor session finished; reloading project vars")
		}
		return m, loadProjectVarsCmd(m.projectRoot)
	case addSecretDoneMsg:
		m.addSecretStage = ""
		m.addSecretResult = msg.result
		m.addSecretErr = msg.err
		for _, line := range msg.logs {
			m.pushLog(line)
		}
		if msg.err != nil {
			m.pushLog("登记 secret 失败: " + msg.err.Error())
		}
		return m, nil
	case runEventMsg:
		m.recordRunEvent(msg.event)
		if m.runStream != nil {
			return m, waitRunMsg(m.runStream)
		}
		return m, nil
	case runCompletedMsg:
		m.runningPull = false
		m.runStream = nil
		if m.runCancel != nil {
			m.runCancel()
			m.runCancel = nil
		}
		m.runCtx = nil
		if m.runMode == "push" {
			m.pushResult = msg.pushResult
			m.runResult = nil
			m.pushStage = ""
		} else {
			m.runResult = msg.result
			m.pushResult = nil
		}
		m.runErr = msg.err
		if msg.err != nil {
			if m.runMode == "push" {
				m.pushLog("Run push failed: " + msg.err.Error())
			} else {
				m.pushLog("Run pull failed: " + msg.err.Error())
			}
			return m, nil
		}
		if m.runMode == "push" && msg.pushResult != nil {
			m.pushLog(fmt.Sprintf("Run push finished: dec %d · secrets created %d / updated %d",
				msg.pushResult.DecPushedCount, msg.pushResult.SecretsCreatedCount, msg.pushResult.SecretsUpdatedCount))
		} else if msg.result != nil {
			secretsMsg := fmt.Sprintf("secrets %d files · %d ssh", msg.result.SecretsNoteCount, msg.result.SecretsSSHKeyCount)
			if msg.result.SecretsSkippedReason != "" && msg.result.SecretsNoteCount == 0 && msg.result.SecretsSSHKeyCount == 0 {
				secretsMsg = "secrets skipped: " + msg.result.SecretsSkippedReason
			}
			m.pushLog(fmt.Sprintf("Run pull finished: %d pulled / %d failed · %s",
				msg.result.PulledCount, msg.result.FailedCount, secretsMsg))
		}
		return m, m.refreshCmd()
	case removeEventMsg:
		m.recordRunEvent(msg.event)
		if m.runStream != nil {
			return m, waitRunMsg(m.runStream)
		}
		return m, nil
	case removeCompletedMsg:
		m.runningRemove = false
		m.runStream = nil
		m.removeResult = msg.result
		m.removeErr = msg.err
		m.removeStage = ""
		m.removeTarget = nil
		if msg.err != nil {
			m.pushLog("Run remove failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			m.pushLog(fmt.Sprintf("Run remove finished: bundle %s (%d members)", msg.result.BundleName, msg.result.MemberCount))
		}
		return m, m.refreshCmd()
	case updateCheckedMsg:
		m.updateResult = msg.result
		m.updateErr = msg.err
		if msg.err != nil {
			m.updateStage = "done"
			m.pushLog("Update check failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result == nil {
			m.updateStage = "done"
			m.pushLog("Update check returned empty result")
			return m, nil
		}
		if !msg.result.NeedUpdate {
			m.updateStage = "done"
			m.pushLog(fmt.Sprintf("Already up to date: %s", msg.result.CurrentVersion))
			return m, nil
		}
		m.updateStage = "confirm"
		m.pushLog(fmt.Sprintf("New version available: %s -> %s", msg.result.CurrentVersion, msg.result.LatestVersion))
		return m, nil
	case updateDoneMsg:
		m.updatingBinary = false
		m.updateErr = msg.err
		m.updateDoneVersion = msg.targetVersion
		m.updateStage = "done"
		if msg.err != nil {
			m.pushLog("Update failed: " + msg.err.Error())
			return m, nil
		}
		m.pushLog(fmt.Sprintf("Update succeeded: %s", msg.targetVersion))
		return m, nil
	case tea.KeyMsg:
		if routedModel, routedCmd, routed := m.routeDeletePageKey(msg); routed {
			return routedModel, routedCmd
		}
		if m.assetFilterInput && m.isBundlesPage() {
			return m.handleAssetFilterInput(msg)
		}
		if m.settingsRepoEditing && m.isSettingsPage() {
			return m.handleSettingsRepoInput(msg)
		}
		if m.removeFilterInput && m.isRunPage() {
			return m.handleRemoveFilterInput(msg)
		}
		if m.addSecretStage != "" && m.isProjectPage() {
			return m.handleAddSecretKey(msg)
		}
		if m.isRunPage() && m.pushStage != "" && !m.runningPull {
			return m.handlePushStageKey(msg)
		}
		if m.isRunPage() && m.removeStage != "" && !m.runningRemove {
			return m.handleRemoveStageKey(msg)
		}
		if m.isRunPage() && m.updateStage != "" && !m.updatingBinary {
			return m.handleUpdateStageKey(msg)
		}
		if m.isHomePage() && m.hasVaultInferencePrompt() {
			return m.handleVaultInferenceKey(msg)
		}
		if m.isHomePage() && m.applyingVaultProject {
			return m, nil
		}
		if m.isRunPage() && m.runningPull && msg.String() == "esc" {
			if m.runCancel != nil {
				m.runCancel()
				m.pushLog("Run pull cancel requested")
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.pushLog("Exit requested")
			return m, tea.Quit
		case "tab":
			fromPage := m.pages[m.pageIndex]
			m.pageIndex = (m.pageIndex + 1) % len(m.pages)
			m.focus = focusSidebar
			m.pushLog("Switched to " + m.pages[m.pageIndex])
			return m, m.onPageChanged(fromPage)
		case "shift+tab":
			fromPage := m.pages[m.pageIndex]
			m.pageIndex = (m.pageIndex - 1 + len(m.pages)) % len(m.pages)
			m.focus = focusSidebar
			m.pushLog("Switched to " + m.pages[m.pageIndex])
			return m, m.onPageChanged(fromPage)
		case "l", "right":
			return m.handleHorizontalNav(1)
		case "h", "left":
			return m.handleHorizontalNav(-1)
		case "j", "down":
			return m.handleVerticalNav(1)
		case "k", "up":
			return m.handleVerticalNav(-1)
		case "r":
			m.pushLog("Refreshing project overview, assets, and global settings")
			return m, m.refreshCmd()
		case "/":
			if m.isBundlesPage() && m.focus != focusSidebar {
				m.assetFilterInput = true
				m.pushLog("Asset filter input opened")
			}
			return m, nil
		case "c":
			if m.isBundlesPage() && m.focus != focusSidebar && strings.TrimSpace(m.assetFilter) != "" {
				m.assetFilter = ""
				m.normalizeAssetCursor()
				m.pushLog("Asset filter cleared")
				return m, nil
			}
			if m.isProjectPage() && !m.savingProjectSettings && m.projectSettings != nil && m.projectSettingsErr == nil {
				m.clearProjectOverride()
				return m, nil
			}
			return m, nil
		case " ", "enter":
			if m.isBundlesPage() && !m.savingAssets && m.focus != focusSidebar {
				if msg.String() == "enter" && m.assetsCursorOnBundle() {
					if m.assetTree.CursorExpanded() {
						m.collapseCurrentBundle()
					} else {
						m.expandCurrentBundle()
					}
					return m, nil
				}
				m.toggleCurrentAsset()
				return m, nil
			}
			if m.isSettingsPage() && !m.savingSettings && m.focus != focusSidebar {
				if m.settingsCursor == 0 {
					if msg.String() == "enter" {
						m.beginSettingsRepoEdit()
					}
				} else {
					m.toggleCurrentSettingsIDE()
				}
				return m, nil
			}
			if m.isProjectPage() && !m.savingProjectSettings && m.projectSettings != nil && m.projectSettingsErr == nil && m.focus != focusSidebar {
				if m.projectSettingsCursor == 0 {
					m.toggleProjectOverride()
				} else {
					m.toggleCurrentProjectIDE()
				}
			}
			return m, nil
		case "e":
			if m.isSettingsPage() && !m.savingSettings {
				m.beginSettingsRepoEdit()
				return m, nil
			}
			if m.isProjectPage() && !m.savingProjectSettings && !m.initializingProjectConfig {
				editorCmd := ""
				if m.projectVars != nil {
					editorCmd = m.projectVars.EditorCommand
				} else if m.overview != nil {
					editorCmd = m.overview.Editor
				}
				m.pushLog("Opening external editor for .dec/vars.yaml")
				return m, openProjectVarsEditorCmd(m.projectRoot, editorCmd)
			}
			return m, nil
		case "s":
			if m.isBundlesPage() && !m.savingAssets && m.assets != nil && m.assetsErr == nil {
				m.savingAssets = true
				m.pushLog("Saving bundle selection")
				return m, saveAssetsCmd(m.projectRoot, cloneStrings(m.bundleSelection))
			}
			if m.isSettingsPage() && !m.savingSettings && m.settings != nil && m.settingsErr == nil {
				m.savingSettings = true
				m.pushLog("Saving global settings")
				return m, saveSettingsCmd(strings.TrimSpace(m.settingsRepoInput), cloneStrings(m.settingsSelectedIDEs))
			}
			if m.isProjectPage() && !m.savingProjectSettings && m.projectSettings != nil && m.projectSettingsErr == nil {
				if m.projectSettingsOverride && len(normalizedStringList(m.projectSettingsSelectedIDEs)) == 0 {
					m.pushLog("覆盖模式下至少选择一个 IDE，或按 c 切回继承模式")
					return m, nil
				}
				m.savingProjectSettings = true
				clearOverride := !m.projectSettingsOverride
				if clearOverride {
					m.pushLog("Saving project settings: clear override")
				} else {
					m.pushLog("Saving project settings: override")
				}
				return m, saveProjectSettingsCmd(m.projectRoot, clearOverride, cloneStrings(m.projectSettingsSelectedIDEs))
			}
			if m.isRunPage() && !m.runningPull && !m.runningRemove && !m.updatingBinary && m.updateStage == "" {
				return m, m.startPullRun()
			}
			return m, nil
		case "i":
			if m.isProjectPage() && m.projectSettings != nil && m.projectSettingsErr == nil && !m.projectSettings.ProjectConfigReady {
				if m.initializingProjectConfig || m.savingProjectSettings {
					return m, nil
				}
				if m.overview == nil || !m.overview.RepoConnected {
					m.pushLog("初始化项目配置需要先连接仓库，请切到 Settings 页配置 Repo URL")
					return m, nil
				}
				m.initializingProjectConfig = true
				m.lastInitResult = nil
				m.lastInitErr = nil
				m.pushLog("Initializing project config (扫描仓库资产)...")
				return m, initProjectConfigCmd(m.projectRoot)
			}
			return m, nil
		case "A":
			if m.isProjectPage() && m.projectSettings != nil && m.projectSettingsErr == nil {
				if !m.projectSettings.ProjectConfigReady {
					m.pushLog("登记 secret 需要先初始化 .dec/config.yaml，按 i 初始化")
					return m, nil
				}
				m.beginAddSecret()
				return m, nil
			}
			return m, nil
		case "R":
			if m.isProjectPage() && m.projectSettings != nil && m.projectSettingsErr == nil && m.projectSettings.ProjectConfigReady {
				if m.initializingProjectConfig || m.savingProjectSettings {
					return m, nil
				}
				if m.overview == nil || !m.overview.RepoConnected {
					m.pushLog("刷新 available 需要先连接仓库，请切到 Settings 页配置 Repo URL")
					return m, nil
				}
				m.initializingProjectConfig = true
				m.lastInitResult = nil
				m.lastInitErr = nil
				m.pushLog("Refreshing project available assets (扫描仓库)...")
				return m, initProjectConfigCmd(m.projectRoot)
			}
			return m, nil
		case "p":
			if m.isRunPage() && !m.runningPull && !m.runningRemove && m.pushStage == "" && !m.updatingBinary && m.updateStage == "" {
				return m, m.startPullRun()
			}
			return m, nil
		case "P":
			if m.isRunPage() && !m.runningPull && !m.runningRemove && m.pushStage == "" && !m.updatingBinary && m.updateStage == "" {
				m.beginPushConfirmation()
			}
			return m, nil
		case "u":
			if m.isRunPage() && !m.runningPull && !m.runningRemove && m.pushStage == "" && !m.updatingBinary && m.removeStage == "" {
				return m, m.startUpdateCheck()
			}
			return m, nil
		case "?":
			if m.isRunPage() && m.removeStage == "" && m.pushStage == "" && !m.removeFilterInput {
				m.runShowHelp = !m.runShowHelp
			}
			return m, nil
		}
	}

	return m, nil
}

// handleHorizontalNav 实现左右键（h/l）的空间进入/退出模型。
func (m model) handleHorizontalNav(direction int) (tea.Model, tea.Cmd) {
	switch m.focus {
	case focusSidebar:
		if direction > 0 {
			m.focus = focusContent
			m.pushLog("进入内容区")
		}
		return m, nil
	case focusContent:
		if direction < 0 {
			if m.isBundlesPage() && m.assetsCursorOnBundle() && m.assetTree.CursorExpanded() {
				m.collapseCurrentBundle()
				return m, nil
			}
			if m.isDeletePage() && m.deleteTree.CollapseAtCursor() {
				m.pushLog("Delete 折叠目录")
				return m, nil
			}
			m.focus = focusSidebar
			m.pushLog("返回导航")
			return m, nil
		}
		if m.isDeletePage() && direction > 0 {
			if m.deleteTree.CursorOnExpandable() && !m.deleteTree.CursorExpanded() {
				m.deleteTree.ExpandAtCursor()
				m.pushLog("Delete 展开目录")
				return m, nil
			}
			return m, nil
		}
		if m.isBundlesPage() && m.focus == focusContent && direction > 0 {
			m.expandAssetAtCursor()
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

// handleVerticalNav 实现上下键（j/k）在侧栏或内容区的移动。
func (m model) handleVerticalNav(delta int) (tea.Model, tea.Cmd) {
	switch m.focus {
	case focusSidebar:
		fromPage := m.pages[m.pageIndex]
		if delta > 0 {
			m.pageIndex = (m.pageIndex + 1) % len(m.pages)
		} else {
			m.pageIndex = (m.pageIndex - 1 + len(m.pages)) % len(m.pages)
		}
		m.pushLog("Switched to " + m.pages[m.pageIndex])
		return m, m.onPageChanged(fromPage)
	case focusContent:
		if m.isBundlesPage() {
			if m.canNavigateAssets() {
				m.moveAssetCursor(delta)
			}
			return m, nil
		}
		if m.isSettingsPage() {
			if m.canNavigateSettings() {
				m.moveSettingsCursor(delta)
			}
			return m, nil
		}
		if m.isProjectPage() {
			if m.canNavigateProjectSettings() {
				m.moveProjectSettingsCursor(delta)
			}
			return m, nil
		}
		if m.isDeletePage() && m.focus == focusContent {
			m.deleteTree.MoveCursor(delta)
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

func (m model) View() string {
	width := m.width
	if width <= 0 {
		width = 100
	}
	height := m.height
	if height <= 0 {
		height = 30
	}

	// 宽度自适应：优先保证 main 区不被 sidebar 挤到横溢出。
	// - <80 列：sidebar 缩到 14，给主区最多可用空间
	// - 80-109 列：常规 18
	// - >=110 列：宽终端给 22，强化导航可读性
	sidebarWidth := 18
	switch {
	case width < 80:
		sidebarWidth = 14
	case width >= 110:
		sidebarWidth = 22
	}
	// 主区宽度扣除：侧边栏 + 两个卡片 border 各占 1 列（sidebar 右 + main 左）。
	// lipgloss.RoundedBorder 在横向分别贡献 1 列的边角，共 4 列（2 个卡片 × 2 边 / 2）。
	// 这里以保守常量 4 做扣除，保证 sidebar_card + main_card 水平合计 <= width。
	mainWidth := width - sidebarWidth - 4
	// 软下界：极窄终端下宁可窄，也不横溢出超过 width。
	if mainWidth < 20 {
		mainWidth = 20
	}

	statusBar := m.renderStatusBar(width)
	logsHeight := 7
	if height < 26 {
		logsHeight = 5
	}
	contentHeight := height - lipgloss.Height(statusBar) - logsHeight
	if contentHeight < 12 {
		contentHeight = 12
	}

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderSidebar(sidebarWidth, contentHeight+logsHeight),
		lipgloss.JoinVertical(
			lipgloss.Left,
			m.renderMain(mainWidth, contentHeight),
			m.renderLogs(mainWidth, logsHeight),
		),
	)

	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}

func (m model) refreshCmd() tea.Cmd {
	return tea.Batch(loadOverviewCmd(m.projectRoot), loadAssetsCmd(m.projectRoot), loadSettingsCmd(), loadProjectSettingsCmd(m.projectRoot), loadProjectVarsCmd(m.projectRoot))
}

func loadOverviewCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		inference, inferErr := inferVaultProjectOperation(projectRoot, nil)
		if inferErr != nil {
			return overviewLoadedMsg{err: inferErr, vaultInference: inference}
		}
		overview, err := app.LoadProjectOverview(projectRoot)
		return overviewLoadedMsg{overview: overview, err: err, vaultInference: inference}
	}
}

func applyVaultProjectCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		result, err := applyVaultProjectOperation(projectRoot, nil)
		return vaultProjectAppliedMsg{result: result, err: err}
	}
}

func loadAssetsCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		state, err := app.LoadAssetSelection(projectRoot, nil)
		return assetsLoadedMsg{state: state, err: err}
	}
}

func saveAssetsCmd(projectRoot string, bundles []string) tea.Cmd {
	return func() tea.Msg {
		result, err := app.SaveEnabledBundles(projectRoot, bundles, nil)
		return assetsSavedMsg{result: result, err: err}
	}
}

func loadSettingsCmd() tea.Cmd {
	return func() tea.Msg {
		state, err := loadGlobalSettingsOperation(nil)
		return settingsLoadedMsg{state: state, err: err}
	}
}

func saveSettingsCmd(repoURL string, ides []string) tea.Cmd {
	return func() tea.Msg {
		result, err := saveGlobalSettingsOperation(app.SaveGlobalSettingsInput{RepoURL: repoURL, IDEs: cloneStrings(ides)}, nil)
		return settingsSavedMsg{result: result, err: err}
	}
}

func ensureBuiltinIDEAssetsCmd(ideNames []string) tea.Cmd {
	return func() tea.Msg {
		return builtinAssetsEnsuredMsg{warnings: ensureBuiltinIDEAssetsOperation(cloneStrings(ideNames), nil)}
	}
}

func loadProjectSettingsCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		state, err := loadProjectSettingsOperation(projectRoot, nil)
		return projectSettingsLoadedMsg{state: state, err: err}
	}
}

func saveProjectSettingsCmd(projectRoot string, clearOverride bool, ides []string) tea.Cmd {
	return func() tea.Msg {
		result, err := saveProjectSettingsOperation(app.SaveProjectSettingsInput{
			ProjectRoot:   projectRoot,
			IDEs:          cloneStrings(ides),
			ClearOverride: clearOverride,
		}, nil)
		return projectSettingsSavedMsg{result: result, err: err}
	}
}

func initProjectConfigCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		result, err := prepareProjectConfigInitOperation(projectRoot, nil)
		return projectConfigInitializedMsg{result: result, err: err}
	}
}

func loadProjectVarsCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		view, err := loadProjectVarsViewOperation(projectRoot)
		return projectVarsLoadedMsg{view: view, err: err}
	}
}

// openProjectVarsEditorCmd 挂起 TUI 用 tea.ExecProcess 拉起外部编辑器编辑 .dec/vars.yaml。
// 文件不存在时先落模板（不覆盖已有内容）。编辑器退出后推送 projectVarsEditedMsg 触发刷新。
func openProjectVarsEditorCmd(projectRoot, editorCmd string) tea.Cmd {
	ensured, err := ensureProjectVarsFileOperation(projectRoot)
	if err != nil {
		return func() tea.Msg {
			return projectVarsEditedMsg{err: err}
		}
	}
	cmd, err := editor.BuildCommand(ensured.Path, editorCmd)
	if err != nil {
		return func() tea.Msg {
			return projectVarsEditedMsg{err: err}
		}
	}
	return tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		return projectVarsEditedMsg{err: runErr}
	})
}

func startPullRunCmd(ctx context.Context, projectRoot string, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			result, err := runPullOperation(ctx, projectRoot, app.ReporterFunc(func(event app.OperationEvent) {
				stream <- runEventMsg{event: event}
			}))
			stream <- runCompletedMsg{result: result, err: err}
			close(stream)
		}()
		return nil
	}
}

func startPushRunCmd(ctx context.Context, projectRoot string, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			result, err := runPushOperation(ctx, projectRoot, app.ReporterFunc(func(event app.OperationEvent) {
				stream <- runEventMsg{event: event}
			}))
			stream <- runCompletedMsg{pushResult: result, err: err}
			close(stream)
		}()
		return nil
	}
}

func waitRunMsg(stream <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-stream
		if !ok {
			return nil
		}
		return msg
	}
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}

func (m model) handleAssetFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.assetFilterInput = false
		m.pushLog("Asset filter input closed")
		return m, nil
	case tea.KeyEnter:
		m.assetFilterInput = false
		m.normalizeAssetCursor()
		m.pushLog("Asset filter applied: " + m.currentAssetFilterLabel())
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.assetFilter = trimLastRune(m.assetFilter)
		m.normalizeAssetCursor()
		return m, nil
	}

	if len(msg.Runes) > 0 && !msg.Alt {
		m.assetFilter += string(msg.Runes)
		m.normalizeAssetCursor()
	}
	return m, nil
}

func (m model) handleSettingsRepoInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.settingsRepoEditing = false
		m.syncSettingsDirty()
		m.pushLog("Repo URL input closed")
		return m, nil
	case tea.KeyEnter:
		m.settingsRepoEditing = false
		m.syncSettingsDirty()
		m.pushLog("Repo URL updated: " + fallbackValue(strings.TrimSpace(m.settingsRepoInput), "<empty>"))
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.settingsRepoInput = trimLastRune(m.settingsRepoInput)
		m.syncSettingsDirty()
		return m, nil
	}

	if len(msg.Runes) > 0 && !msg.Alt {
		m.settingsRepoInput += string(msg.Runes)
		m.syncSettingsDirty()
	}
	return m, nil
}

func (m *model) startPullRun() tea.Cmd {
	stream := make(chan tea.Msg, 64)
	ctx, cancel := context.WithCancel(context.Background())
	m.runningPull = true
	m.runMode = "pull"
	m.runProgress = nil
	m.runEvents = nil
	m.runPinLine = ""
	m.runResult = nil
	m.pushResult = nil
	m.runErr = nil
	m.runStream = stream
	m.runCtx = ctx
	m.runCancel = cancel
	m.pushLog("Run page started pull")
	return tea.Batch(startPullRunCmd(ctx, m.projectRoot, stream), waitRunMsg(stream))
}

func (m *model) beginPushConfirmation() {
	preview, err := previewPushOperation(m.projectRoot)
	if err != nil {
		m.pushPreviewErr = err
		m.pushPreview = nil
		m.pushStage = "summary"
		m.pushLog("Push 预览失败: " + err.Error())
		return
	}
	m.pushPreview = preview
	m.pushPreviewErr = nil
	m.pushStage = "summary"
	m.pushResult = nil
	m.runErr = nil
	m.pushLog(fmt.Sprintf("Push 确认页已打开：%d 个 enabled bundle", preview.EnabledBundleCount))
}

func (m model) handlePushStageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.pushStage {
	case "summary":
		return m.handlePushSummaryKey(msg)
	case "confirm":
		return m.handlePushConfirmKey(msg)
	}
	return m, nil
}

func (m model) handlePushSummaryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		if m.pushPreviewErr != nil {
			return m, nil
		}
		m.pushStage = "confirm"
		m.pushLog("Push 进入最终确认")
		return m, nil
	case "n", "esc":
		m.pushStage = ""
		m.pushPreview = nil
		m.pushPreviewErr = nil
		m.pushLog("Push 已取消")
		return m, nil
	}
	return m, nil
}

func (m model) handlePushConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		m.pushStage = "running"
		m.pushPreview = nil
		m.pushPreviewErr = nil
		return m, m.startPushRun()
	case "n", "esc":
		m.pushStage = "summary"
		m.pushLog("Push 最终确认已取消，返回摘要")
		return m, nil
	}
	return m, nil
}

func (m *model) startPushRun() tea.Cmd {
	stream := make(chan tea.Msg, 64)
	ctx, cancel := context.WithCancel(context.Background())
	m.runningPull = true
	m.runMode = "push"
	m.runProgress = nil
	m.runEvents = nil
	m.runPinLine = ""
	m.runResult = nil
	m.pushResult = nil
	m.runErr = nil
	m.runStream = stream
	m.runCtx = ctx
	m.runCancel = cancel
	m.pushLog("Run page started push")
	return tea.Batch(startPushRunCmd(ctx, m.projectRoot, stream), waitRunMsg(stream))
}

func (m *model) beginRemoveSelection() {
	if m.assets == nil || len(app.ListEnabledBundles(m.assets)) == 0 {
		m.pushLog("没有可删除的已启用 bundle（enabled_bundles 为空）")
		return
	}
	m.removeStage = "select"
	m.removeCursor = 0
	m.removeFilter = ""
	m.removeFilterInput = false
	m.removeTarget = nil
	m.removeResult = nil
	m.removeErr = nil
	m.pushLog("Remove 选择器已打开")
}

func (m model) enabledRemoveBundles() []app.AssetBundleOption {
	if m.assets == nil {
		return nil
	}
	bundles := app.ListEnabledBundles(m.assets)
	filter := strings.ToLower(strings.TrimSpace(m.removeFilter))
	if filter == "" {
		return bundles
	}
	filtered := make([]app.AssetBundleOption, 0, len(bundles))
	for _, bo := range bundles {
		haystack := strings.ToLower(strings.Join([]string{bo.Name, bo.Vault, bo.Description}, " "))
		if strings.Contains(haystack, filter) {
			filtered = append(filtered, bo)
		}
	}
	return filtered
}

func (m model) handleRemoveStageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.removeStage {
	case "select":
		return m.handleRemoveSelectKey(msg)
	case "confirm":
		return m.handleRemoveConfirmKey(msg)
	}
	return m, nil
}

func (m model) handleRemoveSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	bundles := m.enabledRemoveBundles()
	switch msg.String() {
	case "esc":
		m.removeStage = ""
		m.removeTarget = nil
		m.removeFilter = ""
		m.pushLog("Remove 选择已取消")
		return m, nil
	case "/":
		m.removeFilterInput = true
		m.pushLog("Remove 筛选输入已打开")
		return m, nil
	case "c":
		if strings.TrimSpace(m.removeFilter) != "" {
			m.removeFilter = ""
			if m.removeCursor >= len(m.enabledRemoveBundles()) {
				m.removeCursor = 0
			}
			m.pushLog("Remove 筛选已清空")
		}
		return m, nil
	case "j", "down":
		if len(bundles) == 0 {
			return m, nil
		}
		m.removeCursor++
		if m.removeCursor >= len(bundles) {
			m.removeCursor = len(bundles) - 1
		}
		return m, nil
	case "k", "up":
		if len(bundles) == 0 {
			return m, nil
		}
		m.removeCursor--
		if m.removeCursor < 0 {
			m.removeCursor = 0
		}
		return m, nil
	case "enter", " ":
		if len(bundles) == 0 {
			return m, nil
		}
		if m.removeCursor < 0 || m.removeCursor >= len(bundles) {
			m.removeCursor = 0
		}
		target := bundles[m.removeCursor]
		m.removeTarget = &target
		m.removeStage = "confirm"
		m.pushLog(fmt.Sprintf("Remove 目标选中: bundle %s (%d 成员)", target.Name, len(target.Members)))
		return m, nil
	}
	return m, nil
}

func (m model) handleRemoveConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if m.removeTarget == nil {
			m.removeStage = ""
			return m, nil
		}
		m.removeStage = "running"
		return m, m.startRemoveRun()
	case "n", "esc":
		m.removeStage = "select"
		m.removeTarget = nil
		m.pushLog("Remove 取消，返回选择器")
		return m, nil
	}
	return m, nil
}

func (m model) handleRemoveFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.removeFilterInput = false
		m.pushLog("Remove 筛选输入关闭")
		return m, nil
	case tea.KeyEnter:
		m.removeFilterInput = false
		m.removeCursor = 0
		m.pushLog("Remove 筛选应用: " + m.currentRemoveFilterLabel())
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.removeFilter = trimLastRune(m.removeFilter)
		return m, nil
	}

	if len(msg.Runes) > 0 && !msg.Alt {
		m.removeFilter += string(msg.Runes)
	}
	return m, nil
}

func (m *model) startRemoveRun() tea.Cmd {
	if m.removeTarget == nil {
		return nil
	}
	stream := make(chan tea.Msg, 64)
	m.runningRemove = true
	m.runMode = "remove"
	m.runProgress = nil
	m.runEvents = nil
	m.runPinLine = ""
	m.runResult = nil
	m.runErr = nil
	m.removeResult = nil
	m.removeErr = nil
	m.runStream = stream
	input := app.RemoveBundleInput{
		ProjectRoot: m.projectRoot,
		BundleName:  m.removeTarget.Name,
		Members:     append([]app.AssetSelectionItem(nil), m.removeTarget.Members...),
		Confirmed:   true,
	}
	m.pushLog(fmt.Sprintf("Run page started remove: bundle %s", input.BundleName))
	return tea.Batch(startRemoveRunCmd(input, stream), waitRunMsg(stream))
}

func startRemoveRunCmd(input app.RemoveBundleInput, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			result, err := runRemoveOperation(input, app.ReporterFunc(func(event app.OperationEvent) {
				stream <- removeEventMsg{event: event}
			}))
			stream <- removeCompletedMsg{result: result, err: err}
			close(stream)
		}()
		return nil
	}
}

func (m *model) startUpdateCheck() tea.Cmd {
	m.runMode = "update"
	m.updateStage = "checking"
	m.updateResult = nil
	m.updateErr = nil
	m.updateDoneVersion = ""
	m.pushLog("Run page started update check")
	currentVersion := m.currentVersion
	return func() tea.Msg {
		result, err := updateCheckOperation(currentVersion)
		return updateCheckedMsg{result: result, err: err}
	}
}

func (m *model) startUpdateApply() tea.Cmd {
	m.updateStage = "running"
	m.updatingBinary = true
	m.pushLog("Run page started update apply")
	currentVersion := m.currentVersion
	target := ""
	if m.updateResult != nil {
		target = m.updateResult.LatestVersion
	}
	return func() tea.Msg {
		err := updateDoUpdateOperation(currentVersion, target)
		return updateDoneMsg{targetVersion: target, err: err}
	}
}

func (m model) handleVaultInferenceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		if m.vaultInference == nil {
			return m, nil
		}
		m.applyingVaultProject = true
		m.pushLog(fmt.Sprintf("Applying inferred vault project %s...", m.vaultInference.ProjectName))
		return m, applyVaultProjectCmd(m.projectRoot)
	case "n", "esc":
		m.vaultInferenceDismissed = true
		m.pushLog("已跳过 vault project 推断，可切到 Bundles 页手动选择")
		return m, nil
	}
	return m, nil
}

func (m model) handleUpdateStageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.updateStage {
	case "confirm":
		switch msg.String() {
		case "y":
			return m, m.startUpdateApply()
		case "n", "esc":
			m.updateStage = ""
			m.updateResult = nil
			m.updateErr = nil
			m.updateDoneVersion = ""
			m.pushLog("Update 取消")
			return m, nil
		}
	case "done":
		switch msg.String() {
		case "esc", "enter", " ", "q":
			m.updateStage = ""
			m.updateResult = nil
			m.updateErr = nil
			m.updateDoneVersion = ""
			m.pushLog("Update 面板关闭")
			return m, nil
		}
	case "checking", "running":
		// 忙碌状态忽略所有输入，避免并发操作
		return m, nil
	}
	return m, nil
}

func (m model) currentRemoveFilterLabel() string {
	filter := strings.TrimSpace(m.removeFilter)
	if filter == "" {
		return "<none>"
	}
	return filter
}

func (m *model) recordRunEvent(event app.OperationEvent) {
	if event.Progress != nil {
		progress := *event.Progress
		m.runProgress = &progress
	}
	m.updateRunPinLine(event)
	for _, line := range splitRunMessage(event.Message) {
		m.runEvents = append(m.runEvents, line)
		if len(m.runEvents) > 12 {
			m.runEvents = append([]string(nil), m.runEvents[len(m.runEvents)-12:]...)
		}
		m.pushLog(line)
	}
}

func splitRunMessage(message string) []string {
	normalized := strings.ReplaceAll(strings.TrimSpace(message), "\r\n", "\n")
	if normalized == "" {
		return nil
	}
	parts := strings.Split(normalized, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func (m *model) pushLog(line string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	m.logs = append(m.logs, trimmed)
	if len(m.logs) > 8 {
		m.logs = append([]string(nil), m.logs[len(m.logs)-8:]...)
	}
}

func (m model) renderSidebar(width, height int) string {
	items := make([]string, 0, len(m.pages)+2)
	items = append(items, shellTitleStyle.Render("Dec Shell"))
	items = append(items, shellMutedStyle.Render("j/k 切换页 · l 进入 · h 返回"))
	for idx, page := range m.pages {
		style := shellNavStyle
		if idx == m.pageIndex {
			style = shellActiveNav
			if m.focus == focusSidebar {
				style = shellSelectedRow
			}
		}
		items = append(items, style.Render(page))
	}

	content := strings.Join(items, "\n")
	return shellCardStyle.Width(width).Height(height).Render(content)
}

func (m model) renderMain(width, height int) string {
	heroLines := []string{
		shellTitleStyle.Render(m.pages[m.pageIndex]),
		shellMutedStyle.Render(m.projectRoot),
		shellMutedStyle.Render(m.currentSummary()),
	}
	hero := shellCardStyle.Width(width).Render(strings.Join(heroLines, "\n"))
	bodyHeight := height - lipgloss.Height(hero)
	if bodyHeight < 8 {
		bodyHeight = 8
	}
	body := shellCardStyle.Width(width).Height(bodyHeight).Render(m.renderPageBody(width - 6))
	return lipgloss.JoinVertical(lipgloss.Left, hero, body)
}

func (m model) renderPageBody(width int) string {
	switch m.pages[m.pageIndex] {
	case "Home":
		return m.renderHomePage(width)
	case "Bundles":
		return m.renderBundlesPage(width)
	case "Project":
		return m.renderProjectPage(width)
	case "Run":
		return m.renderRunPage(width)
	case "Delete":
		return m.renderDeletePage(width)
	default:
		return m.renderSettingsPage(width)
	}
}

func (m model) renderHomePage(width int) string {
	if m.overviewErr != nil {
		return shellWarnStyle.Render("Failed to load overview") + "\n\n" + m.overviewErr.Error()
	}
	if m.overview == nil {
		return shellMutedStyle.Render("Loading project overview...")
	}

	lines := []string{}
	if notice := strings.TrimSpace(m.vaultAutoApplyNotice); notice != "" {
		lines = append(lines, shellGoodStyle.Render(notice))
	}
	if m.applyingVaultProject {
		lines = append(lines, shellMutedStyle.Render("正在从 vault 应用 project..."))
	} else if m.hasVaultInferencePrompt() {
		inf := m.vaultInference
		lines = append(lines,
			shellWarnStyle.Render("根据目录名推断 vault project，请确认是否应用"),
			fmt.Sprintf("推断 project: %s", inf.ProjectName),
			fmt.Sprintf("Vault 路径: %s", inf.VaultPath),
			fmt.Sprintf("Bundles (%d): %s", len(inf.EnabledBundles), formatInferenceBundleNames(inf.EnabledBundles)),
			shellMutedStyle.Render("y / Enter 应用 · n 跳过并手动选择"),
		)
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines,
		fmt.Sprintf("项目名: %s", formatProjectNameDisplay(m.overview)),
		fmt.Sprintf("Vault project: projects/%s.yaml", m.overview.ProjectName),
		fmt.Sprintf("仓库: %s", formatReady(m.overview.RepoConnected, "已连接", "未连接")),
		fmt.Sprintf("远端仓库: %s", fallbackValue(m.overview.RepoRemoteURL, "未连接")),
		fmt.Sprintf("项目配置: %s", formatReady(m.overview.ProjectConfigReady, "已初始化", "未初始化")),
		fmt.Sprintf("变量文件: %s", formatReady(m.overview.VarsFileReady, "已存在", "未生成")),
		fmt.Sprintf("Vault bundle: %d 个 | 已启用: %d 个", countOverviewAvailableBundles(m.overview), countOverviewEnabledBundles(m.overview)),
		fmt.Sprintf("默认 IDE: %s", strings.Join(m.overview.IDEs, ", ")),
		fmt.Sprintf("编辑器: %s", fallbackValue(m.overview.Editor, "未配置")),
		fmt.Sprintf("建议下一步: %s", suggestNextAction(m.overview, m.hasVaultInferencePrompt(), m.vaultInferenceDismissed)),
		formatWarnings(m.overview.IDEWarnings),
	)
	return wrapLines(width, lines)
}

func (m model) renderBundlesPage(width int) string {
	if m.assetsErr != nil {
		return shellWarnStyle.Render("无法加载 bundle 选择") + "\n\n" + m.assetsErr.Error()
	}
	if m.assets == nil {
		return shellMutedStyle.Render("Loading bundle selection...")
	}

	summary := []string{}
	if m.configInitMode {
		summary = append(summary, shellTitleStyle.Render("项目配置初始化 — 勾选要启用的 bundle"))
		summary = append(summary, shellMutedStyle.Render("勾选 vault bundles/ 下的 bundle；保存后写入 enabled_bundles。"))
	}
	summary = append(summary,
		shellMutedStyle.Render("扫描 vault bundles/，勾选 bundle 写入 enabled_bundles；成员资产随 bundle 一并启用。"),
		fmt.Sprintf("筛选: %s", m.currentAssetFilterLabel()),
		fmt.Sprintf("Bundle: %d/%d 已启用 | 成员资产: %d 个", len(m.bundleSelection), len(m.assets.Bundles), m.countSelectedBundleMembers()),
	)
	if m.assetsDirty {
		summary = append(summary, shellWarnStyle.Render("当前有未保存修改，按 s 保存。"))
	} else {
		summary = append(summary, shellMutedStyle.Render("当前 bundle 选择与磁盘一致。"))
	}
	if m.assetFilterInput {
		summary = append(summary, shellMutedStyle.Render("筛选输入中：输入关键字后按 Enter 应用，Esc 退出。"))
	} else {
		switch m.focus {
		case focusSidebar:
			summary = append(summary, shellMutedStyle.Render("按 l 进入内容区 · j/k 在侧栏切换页"))
		default:
			summary = append(summary, shellMutedStyle.Render("快捷键：j/k 移动 · h 返回导航 · l 展开 bundle · space 切换 · s 保存 · / 筛选"))
		}
	}
	if !m.assets.ExistingConfig {
		summary = append(summary, shellMutedStyle.Render("首次保存会创建 .dec/config.yaml 与 .dec/vars.yaml。"))
	}

	rows := m.assetTreeVisibleCount()
	if len(m.assets.Bundles) == 0 {
		return strings.Join(append(summary, "", "仓库 bundles/ 下还没有可选 bundle。"), "\n")
	}
	if rows == 0 {
		return strings.Join(append(summary, "", "当前筛选没有结果。"), "\n")
	}

	list := m.renderAssetList()
	detail := m.renderAssetDetails()
	if width < 88 {
		return strings.Join(append(summary, "", list, "", detail), "\n")
	}

	leftWidth := width / 2
	rightWidth := width - leftWidth - 2
	left := lipgloss.NewStyle().Width(leftWidth).Render(list)
	right := lipgloss.NewStyle().Width(rightWidth).Render(detail)
	return strings.Join(summary, "\n") + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m model) renderProjectPage(width int) string {
	if m.projectSettingsErr != nil {
		return shellWarnStyle.Render("Failed to load project settings") + "\n\n" + m.projectSettingsErr.Error()
	}
	if m.projectSettings == nil {
		if m.overviewErr != nil {
			return shellWarnStyle.Render("Failed to load project details") + "\n\n" + m.overviewErr.Error()
		}
		return shellMutedStyle.Render("Loading project settings...")
	}

	modeLabel := "继承全局"
	if m.projectSettingsOverride {
		modeLabel = "项目显式覆盖"
	}

	summary := []string{
		shellMutedStyle.Render("项目配置与变量：IDE 覆盖、.dec/vars.yaml；bundle 启用在 Bundles 页。"),
	}
	if m.overview != nil {
		summary = append(summary,
			fmt.Sprintf("项目名: %s", formatProjectNameDisplay(m.overview)),
			fmt.Sprintf("Vault project: projects/%s.yaml", m.overview.ProjectName),
			fmt.Sprintf("已启用 bundle (%d): %s", countOverviewEnabledBundles(m.overview), formatOverviewEnabledBundleNames(m.overview)),
		)
	}
	summary = append(summary,
		fmt.Sprintf("配置文件: %s", m.projectSettings.ConfigPath),
		fmt.Sprintf("变量文件: %s", m.projectSettings.VarsPath),
		fmt.Sprintf("IDE 模式: %s", modeLabel),
		fmt.Sprintf("项目 IDE: %s", fallbackValue(strings.Join(normalizedStringList(m.projectSettingsSelectedIDEs), ", "), "<none>")),
		fmt.Sprintf("全局默认: %s", fallbackValue(strings.Join(normalizedStringList(m.projectSettings.GlobalIDEs), ", "), "<未配置>")),
		fmt.Sprintf("生效 IDE: %s", fallbackValue(strings.Join(projectEffectivePreview(m.projectSettings, m.projectSettingsOverride, m.projectSettingsSelectedIDEs), ", "), "<none>")),
		formatWarnings(m.projectSettings.IDEWarnings),
	)
	if !m.projectSettings.ProjectConfigReady {
		summary = append(summary, shellMutedStyle.Render("尚未初始化 .dec/config.yaml，请先在 Home 页初始化 project。"))
	}
	if m.projectSettingsDirty {
		summary = append(summary, shellWarnStyle.Render("当前有未保存修改，按 s 保存。"))
	} else {
		summary = append(summary, shellMutedStyle.Render("当前项目设置与磁盘一致。"))
	}

	summary = append(summary, shellMutedStyle.Render("快捷键：j/k 移动 · space 切换模式/IDE · s 保存 · c 清除覆盖 · e 编辑变量 · A 登记 secret"))
	if m.savingProjectSettings {
		summary = append(summary, shellWarnStyle.Render("正在保存项目设置..."))
	}
	if m.lastEditErr != nil {
		summary = append(summary, shellWarnStyle.Render("编辑器返回错误: "+m.lastEditErr.Error()))
	}

	if outcome := m.renderAddSecretOutcome(); outcome != "" {
		summary = append(summary, outcome)
	}

	list := m.renderProjectSettingsList()
	detail := m.renderProjectSettingsDetails()
	varsBlock := m.renderProjectVarsBlock()
	trailing := make([]string, 0, 2)
	if m.addSecretStage != "" {
		trailing = append(trailing, m.renderAddSecretBlock())
	}
	if varsBlock != "" {
		trailing = append(trailing, varsBlock)
	}

	if width < 88 {
		parts := append(summary, "", list, "", detail)
		for _, block := range trailing {
			parts = append(parts, "", block)
		}
		return strings.Join(parts, "\n")
	}

	leftWidth := width / 2
	rightWidth := width - leftWidth - 2
	left := lipgloss.NewStyle().Width(leftWidth).Render(list)
	right := lipgloss.NewStyle().Width(rightWidth).Render(detail)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	result := strings.Join(summary, "\n") + "\n\n" + body
	for _, block := range trailing {
		result += "\n\n" + block
	}
	return result
}

func (m model) renderProjectSettingsList() string {
	lines := []string{shellTitleStyle.Render("项目 IDE")}
	override := m.projectSettingsOverride
	checked := " "
	if override {
		checked = "x"
	}
	overrideLine := fmt.Sprintf("%s [%s] 覆盖全局 IDE", settingsCursorMarker(m.projectSettingsCursor == 0 && m.focus != focusSidebar), checked)
	switch {
	case m.projectSettingsCursor == 0 && m.focus != focusSidebar:
		lines = append(lines, shellSelectedRow.Render(overrideLine))
	case override:
		lines = append(lines, shellEnabledRow.Render(overrideLine))
	default:
		lines = append(lines, shellLogStyle.Render(overrideLine))
	}
	if m.projectSettings == nil {
		return strings.Join(lines, "\n")
	}
	for idx, ideName := range m.projectSettings.AvailableIDEs {
		selected := override && settingsContainsIDE(m.projectSettingsSelectedIDEs, ideName)
		mark := " "
		if selected {
			mark = "x"
		}
		line := fmt.Sprintf("%s [%s] %s", settingsCursorMarker(m.projectSettingsCursor == idx+1 && m.focus != focusSidebar), mark, ideName)
		switch {
		case m.projectSettingsCursor == idx+1 && m.focus != focusSidebar:
			lines = append(lines, shellSelectedRow.Render(line))
		case selected:
			lines = append(lines, shellEnabledRow.Render(line))
		default:
			lines = append(lines, shellLogStyle.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) renderProjectSettingsDetails() string {
	lines := []string{shellTitleStyle.Render("Details")}
	if m.projectSettings == nil {
		return strings.Join(lines, "\n")
	}
	if m.projectSettingsCursor == 0 {
		lines = append(lines,
			"模式切换：决定是否用项目级 IDE 覆盖全局默认。",
			fmt.Sprintf("覆盖开关: %s", formatReady(m.projectSettingsOverride, "已开启", "未开启（继承全局）")),
			fmt.Sprintf("全局默认: %s", fallbackValue(strings.Join(normalizedStringList(m.projectSettings.GlobalIDEs), ", "), "<未配置>")),
			fmt.Sprintf("当前生效: %s", fallbackValue(strings.Join(projectEffectivePreview(m.projectSettings, m.projectSettingsOverride, m.projectSettingsSelectedIDEs), ", "), "<none>")),
			shellMutedStyle.Render("按 space 切换；c 可一键清除覆盖回落全局。"),
		)
	} else {
		ideName := m.currentProjectSettingsIDEName()
		state := "未选中"
		if m.projectSettingsOverride && settingsContainsIDE(m.projectSettingsSelectedIDEs, ideName) {
			state = "已选中"
		}
		lines = append(lines,
			fmt.Sprintf("IDE: %s", ideName),
			fmt.Sprintf("当前状态: %s", state),
		)
		if !m.projectSettingsOverride {
			lines = append(lines, shellMutedStyle.Render("当前处于继承模式。按 space 切到第一行开启覆盖后再选择 IDE。"))
		} else {
			lines = append(lines, shellMutedStyle.Render("按 space 在此 IDE 上切换。保存后将写入 .dec/config.yaml。"))
		}
	}
	return strings.Join(lines, "\n")
}

// projectEffectivePreview 返回本地编辑态下应当生效的 IDE 列表。
// 覆盖模式下使用本地选择；继承模式下展示 state.EffectiveIDEs（已由 ResolveEffectiveIDEs 解析过）。
func projectEffectivePreview(state *app.ProjectSettingsState, override bool, selected []string) []string {
	if override {
		return normalizedStringList(selected)
	}
	if state == nil {
		return nil
	}
	if len(state.GlobalIDEs) > 0 {
		return normalizedStringList(state.GlobalIDEs)
	}
	return normalizedStringList(state.EffectiveIDEs)
}

// renderProjectVarsBlock 渲染 Project 页下方的 "项目变量" 区块。
// 纯只读；写入通过 `e` 键挂起 TUI 用外部编辑器完成。
func (m model) renderProjectVarsBlock() string {
	lines := []string{shellTitleStyle.Render("项目变量")}

	if m.projectVarsErr != nil {
		lines = append(lines, shellWarnStyle.Render("加载变量失败: "+m.projectVarsErr.Error()))
		return strings.Join(lines, "\n")
	}

	if m.projectVars == nil {
		lines = append(lines, shellMutedStyle.Render("Loading project vars..."))
		return strings.Join(lines, "\n")
	}

	view := m.projectVars
	fileLine := fmt.Sprintf("变量文件: %s", view.VarsPath)
	if view.VarsFileReady {
		fileLine += shellMutedStyle.Render(" (已存在)")
	} else {
		fileLine += shellWarnStyle.Render(" (未生成，按 e 会自动创建模板并打开编辑器)")
	}
	lines = append(lines, fileLine)
	lines = append(lines, fmt.Sprintf("编辑器: %s", fallbackValue(view.EditorCommand, "<未配置，将回退到 vim/vi/notepad>")))
	lines = append(lines, shellMutedStyle.Render("快捷键：e 打开外部编辑器编辑 .dec/vars.yaml"))

	for _, w := range view.Warnings {
		lines = append(lines, shellWarnStyle.Render(w))
	}

	if !view.CacheExists {
		lines = append(lines, shellMutedStyle.Render(".dec/cache 尚不存在：请先到 Run 页执行 pull，再查看占位符。"))
	}
	if len(view.UsedPlaceholders) == 0 {
		if view.CacheExists {
			lines = append(lines, shellMutedStyle.Render("当前资产中未检测到 {{VAR_NAME}} 占位符。"))
		}
		return strings.Join(lines, "\n")
	}

	lines = append(lines, fmt.Sprintf("占位符总数: %d | 缺失: %d", len(view.UsedPlaceholders), len(view.MissingPlaceholders())))
	for _, name := range view.UsedPlaceholders {
		status := view.ResolvedVars[name]
		var row string
		switch status.Source {
		case app.PlaceholderSourceProject:
			row = fmt.Sprintf("  %s = %s  (project)", name, truncateVarValue(status.Value))
			row = shellEnabledRow.Render(row)
		case app.PlaceholderSourceGlobal:
			row = fmt.Sprintf("  %s = %s  (global)", name, truncateVarValue(status.Value))
			row = shellMutedStyle.Render(row)
		default:
			row = shellWarnStyle.Render(fmt.Sprintf("  %s = <缺失>  (missing)", name))
		}
		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}

// truncateVarValue 把过长的变量值截断显示，避免一行撑破区块。
func truncateVarValue(v string) string {
	const max = 60
	if len(v) <= max {
		return v
	}
	return v[:max] + "…"
}

func (m model) renderRunPage(width int) string {
	if m.pushStage == "summary" || m.pushStage == "confirm" {
		return m.renderRunPushPage(width)
	}
	if m.removeStage == "select" || m.removeStage == "confirm" {
		return m.renderRunRemovePage(width)
	}

	sections := []string{
		m.renderRunHeader(),
		m.renderRunActionBar(),
	}
	sections = append(sections, m.renderRunStateBlock(width)...)

	if m.runShowHelp {
		sections = append(sections, m.renderRunHelpPanel()...)
	}

	if m.updateStage != "" {
		sections = append(sections, "")
		sections = append(sections, m.renderUpdatePanel()...)
	}

	if len(m.runEvents) > 0 {
		sections = append(sections, "", shellTitleStyle.Render("事件日志"))
		for _, line := range m.runEvents {
			sections = append(sections, formatRunLogLine(line))
		}
	} else if !m.runningPull && !m.runningRemove && m.updateStage == "" {
		sections = append(sections, "", shellMutedStyle.Render("执行后事件将显示于此。"))
	}

	return wrapLines(width, sections)
}

func (m model) renderRunHeader() string {
	mode := "就绪"
	switch {
	case m.runningPull && m.runMode == "push":
		mode = "Push 执行中"
	case m.runningPull:
		mode = "Pull 执行中"
	case m.runningRemove:
		mode = "Remove 执行中"
	case m.updatingBinary:
		mode = "Update 执行中"
	case m.runErr != nil && m.runMode == "push":
		mode = "Push 失败"
	case m.runErr != nil:
		mode = "Pull 失败"
	case m.pushResult != nil:
		mode = "Push 完成"
	case m.runResult != nil:
		mode = "Pull 完成"
	case m.removeErr != nil:
		mode = "Remove 失败"
	case m.removeResult != nil:
		mode = "Remove 完成"
	}
	return shellTitleStyle.Render("Run · "+mode) + "\n" + fmt.Sprintf("状态 %s", m.runStatusLabel())
}

func (m model) renderRunActionBar() string {
	switch {
	case m.runningPull && m.runMode == "push":
		return shellMutedStyle.Render("操作  Esc 取消 push  ·  ? 帮助")
	case m.runningPull:
		return shellMutedStyle.Render("操作  Esc 取消 pull  ·  ? 帮助")
	case m.runningRemove, m.updatingBinary:
		return shellMutedStyle.Render("操作  ? 帮助")
	default:
		return shellMutedStyle.Render("操作  p Pull  ·  P Push  ·  u Update  ·  ? 帮助 · 删除请切到 Delete 页")
	}
}

func (m model) renderRunStateBlock(width int) []string {
	if m.runningPull || m.runningRemove {
		return m.renderRunActiveBlock(width)
	}
	if m.runResult == nil && m.pushResult == nil && m.runErr == nil && m.removeResult == nil && m.removeErr == nil {
		return m.renderRunIdleGuide()
	}
	lines := m.renderRunLastResult()
	if plan := m.renderPullPlanLines(); len(plan) > 0 {
		lines = append(lines, "")
		lines = append(lines, plan...)
	}
	return lines
}

// renderPullPlanLines 说明「按下 p 会拉取什么」。
//
// 计划一律按磁盘上的 enabled_bundles 计算（也就是 m.assets 里的 Enabled 标记），
// 因为 pull 读的是磁盘配置；Bundles 页里刚勾还没保存的改动只作为警告提示，
// 避免用户以为勾选立刻生效——这正是"勾了 bundle 却没拉到"的来源。
func (m model) renderPullPlanLines() []string {
	if m.assets == nil {
		return nil
	}
	enabled := app.ListEnabledBundles(m.assets)
	names := make([]string, 0, len(enabled))
	for _, bo := range enabled {
		names = append(names, bo.Name)
	}

	lines := []string{shellTitleStyle.Render("本次 Pull 计划")}
	if len(names) == 0 {
		lines = append(lines, shellWarnStyle.Render("⚠ 当前无启用 bundle，请先到 Bundles 页勾选并按 s 保存"))
	} else {
		lines = append(lines, fmt.Sprintf("将拉取 %d 个资产 · bundle: %s",
			len(app.ListEffectiveEnabledAssets(m.assets)), strings.Join(names, ", ")))
	}
	if pending := m.pendingBundleChanges(); pending != "" {
		lines = append(lines, shellWarnStyle.Render("⚠ Bundles 页有未保存的勾选（"+pending+"），按 s 保存后才会生效"))
	}
	return lines
}

// pendingBundleChanges 描述 TUI 内 bundleSelection 与磁盘 enabled_bundles 的差异。
// 无差异时返回空串。
func (m model) pendingBundleChanges() string {
	if m.assets == nil || !m.assetsDirty {
		return ""
	}
	onDisk := make(map[string]struct{})
	for _, bo := range m.assets.Bundles {
		if bo.Enabled {
			onDisk[bo.Name] = struct{}{}
		}
	}
	selected := make(map[string]struct{}, len(m.bundleSelection))
	for _, name := range m.bundleSelection {
		selected[name] = struct{}{}
	}

	var added, removed []string
	for _, bo := range m.assets.Bundles {
		_, wasEnabled := onDisk[bo.Name]
		_, nowEnabled := selected[bo.Name]
		switch {
		case nowEnabled && !wasEnabled:
			added = append(added, "+"+bo.Name)
		case wasEnabled && !nowEnabled:
			removed = append(removed, "-"+bo.Name)
		}
	}
	changes := append(added, removed...)
	if len(changes) == 0 {
		return ""
	}
	return strings.Join(changes, " ")
}

func (m model) renderRunActiveBlock(width int) []string {
	lines := make([]string, 0, 3)
	if m.runProgress != nil {
		phase := runPhaseLabel(m.runProgress.Phase)
		bar := renderRunProgressBar(m.runProgress.Current, m.runProgress.Total, progressBarWidth(width))
		lines = append(lines, fmt.Sprintf("阶段  %s  %s  %d/%d", phase, bar, m.runProgress.Current, m.runProgress.Total))
	} else {
		lines = append(lines, shellMutedStyle.Render("阶段  准备中…"))
	}
	if pin := strings.TrimSpace(m.runPinLine); pin != "" {
		lines = append(lines, shellWarnStyle.Render("提示  "+pin))
	}
	return lines
}

func (m model) renderRunIdleGuide() []string {
	lines := []string{
		shellTitleStyle.Render("建议"),
		shellMutedStyle.Render("· p 拉取 Dec bundle + secrets + IDE 安装"),
		shellMutedStyle.Render("· P 推送到远端（Dec cache → Git vault + secrets → Bitwarden，需两次确认）"),
		shellMutedStyle.Render("· x 删除已启用 bundle（整包，不可逆）"),
	}
	lines = append(lines, m.renderPullPlanLines()...)
	lines = append(lines, shellMutedStyle.Render("上次  尚无操作记录"))
	return lines
}

func (m model) renderRunLastResult() []string {
	lines := []string{shellTitleStyle.Render("上次结果")}
	if m.runResult != nil {
		lines = append(lines, fmt.Sprintf("Pull  请求 %d · 成功 %d · 失败 %d",
			m.runResult.RequestedCount, m.runResult.PulledCount, m.runResult.FailedCount))
		secretsLine := fmt.Sprintf("Secrets  落地 %d 个文件 · %d 个 SSH Key", m.runResult.SecretsNoteCount, m.runResult.SecretsSSHKeyCount)
		if m.runResult.SecretsSkippedReason != "" && m.runResult.SecretsNoteCount == 0 && m.runResult.SecretsSSHKeyCount == 0 {
			secretsLine = "Secrets  " + m.runResult.SecretsSkippedReason
		}
		lines = append(lines, secretsLine)
		lines = append(lines, fmt.Sprintf("IDE   %s", fallbackValue(strings.Join(m.runResult.EffectiveIDEs, ", "), "<none>")))
		if strings.TrimSpace(m.runResult.VersionCommit) != "" {
			lines = append(lines, fmt.Sprintf("Commit %s", m.runResult.VersionCommit))
		}
	}
	if m.pushResult != nil {
		if m.pushResult.DecPushedCount > 0 || m.pushResult.DecSkippedReason != "" {
			decLine := fmt.Sprintf("Dec   推送 %d 项", m.pushResult.DecPushedCount)
			if m.pushResult.DecSkippedReason != "" && m.pushResult.DecPushedCount == 0 {
				decLine = "Dec   " + m.pushResult.DecSkippedReason
			}
			lines = append(lines, decLine)
		}
		secretsLine := fmt.Sprintf("Secrets  新建 %d · 更新 %d", m.pushResult.SecretsCreatedCount, m.pushResult.SecretsUpdatedCount)
		if m.pushResult.SecretsSkippedReason != "" && m.pushResult.SecretsCreatedCount+m.pushResult.SecretsUpdatedCount == 0 {
			secretsLine = "Secrets  " + m.pushResult.SecretsSkippedReason
		}
		lines = append(lines, secretsLine)
		if strings.TrimSpace(m.pushResult.VersionCommit) != "" {
			lines = append(lines, fmt.Sprintf("Commit %s", m.pushResult.VersionCommit))
		}
	}
	if m.runErr != nil {
		label := "Pull 错误"
		if m.runMode == "push" {
			label = "Push 错误"
		}
		lines = append(lines, shellWarnStyle.Render(label+": "+m.runErr.Error()))
	}
	if m.removeResult != nil {
		lines = append(lines, fmt.Sprintf("Remove  bundle %s · %d 成员", m.removeResult.BundleName, m.removeResult.MemberCount))
		if strings.TrimSpace(m.removeResult.VersionCommit) != "" {
			lines = append(lines, fmt.Sprintf("Remove Commit %s", m.removeResult.VersionCommit))
		}
	}
	if m.removeErr != nil {
		lines = append(lines, shellWarnStyle.Render("Remove 错误: "+m.removeErr.Error()))
	}
	return lines
}

func (m model) renderRunHelpPanel() []string {
	return []string{
		"",
		shellTitleStyle.Render("快捷键"),
		shellMutedStyle.Render("p / s  执行 pull"),
		shellMutedStyle.Render("P      推送到远端（两次确认）"),
		shellMutedStyle.Render("删除请切到 Delete 页（侧栏 Run 之后）"),
		shellMutedStyle.Render("u      检查并自更新 dec"),
		shellMutedStyle.Render("r      刷新项目概览"),
		shellMutedStyle.Render("Esc    取消进行中的 pull / push"),
		shellMutedStyle.Render("?      开关此帮助"),
	}
}

func (m model) renderRunPushPage(width int) string {
	lines := []string{
		shellTitleStyle.Render("Run · Push"),
		fmt.Sprintf("状态 %s", m.runStatusLabel()),
	}
	switch m.pushStage {
	case "summary":
		lines = append(lines, shellMutedStyle.Render("操作  y/Enter 继续 · n/Esc 取消"))
		lines = append(lines, "")
		lines = append(lines, m.renderPushSummary()...)
	case "confirm":
		lines = append(lines, shellMutedStyle.Render("操作  y 确认推送 · n/Esc 返回摘要"))
		lines = append(lines, "")
		lines = append(lines, m.renderPushConfirm()...)
	}
	return wrapLines(width, lines)
}

func (m model) renderPushSummary() []string {
	lines := []string{shellTitleStyle.Render("Push 摘要")}
	if m.pushPreviewErr != nil {
		lines = append(lines, shellWarnStyle.Render("预览失败: "+m.pushPreviewErr.Error()))
		lines = append(lines, shellMutedStyle.Render("按 n/esc 取消"))
		return lines
	}
	if m.pushPreview == nil {
		lines = append(lines, shellWarnStyle.Render("无预览数据，按 esc 取消。"))
		return lines
	}
	p := m.pushPreview
	lines = append(lines, fmt.Sprintf("Enabled bundles: %d", p.EnabledBundleCount))
	if len(p.EnabledBundleNames) > 0 {
		lines = append(lines, fmt.Sprintf("  %s", strings.Join(p.EnabledBundleNames, ", ")))
	}
	if p.ProjectSecretsName != "" {
		lines = append(lines, fmt.Sprintf("Project secrets: %s", p.ProjectSecretsName))
	}
	secretsLine := fmt.Sprintf("Secrets  %d 个 Bitwarden folder（待推文件按远端 Note 列表确定）", p.SecretsTargetCount)
	if !p.BitwardenConfigured {
		secretsLine += "（Bitwarden 未配置，将跳过）"
	}
	lines = append(lines, secretsLine)
	if p.DecHasChanges {
		lines = append(lines, fmt.Sprintf("Dec cache  有变更（约 %d 项待推送）", p.DecCandidateCount))
	} else if p.DecSkippedReason != "" {
		lines = append(lines, fmt.Sprintf("Dec cache  %s", p.DecSkippedReason))
	} else {
		lines = append(lines, "Dec cache  无本地变更")
	}
	lines = append(lines, shellMutedStyle.Render("按 y/Enter 进入最终确认 · n/esc 取消"))
	return lines
}

func (m model) renderPushConfirm() []string {
	lines := []string{shellTitleStyle.Render("Push 最终确认")}
	lines = append(lines,
		shellWarnStyle.Render("⚠️  将推送到远端 Dec Git vault 与 Bitwarden，操作不可逆。"),
		shellMutedStyle.Render("Dec cache 变更将 commit 并 push 到 vault 仓库。"),
		shellMutedStyle.Render("各 Bitwarden folder 已有的 Secure Note 将按本地对应文件更新（不删远端）。"),
		shellMutedStyle.Render("执行中若 Bitwarden 未解锁，将自动尝试解锁（可设 DEC_BW_PASSWORD 免浏览器）。"),
		shellMutedStyle.Render("按 y 确认执行 · n/esc 返回摘要"),
	)
	return lines
}

func (m model) renderRunRemovePage(width int) string {
	lines := []string{
		shellTitleStyle.Render("Run · Remove"),
		fmt.Sprintf("状态 %s", m.runStatusLabel()),
		shellMutedStyle.Render("操作  j/k 移动 · Enter 选中 · / 筛选 · Esc 返回"),
		shellWarnStyle.Render("⚠ 删除整包不可逆；Bitwarden secrets 需自行处理"),
	}
	switch m.removeStage {
	case "select":
		lines = append(lines, "")
		lines = append(lines, m.renderRemoveSelect()...)
	case "confirm":
		lines = append(lines, "")
		lines = append(lines, m.renderRemoveConfirm()...)
	}
	return wrapLines(width, lines)
}

func (m *model) updateRunPinLine(event app.OperationEvent) {
	msg := strings.TrimSpace(event.Message)
	if msg == "" {
		return
	}
	switch {
	case strings.Contains(msg, "解锁页:"):
		m.runPinLine = msg
	case strings.Contains(msg, "Bitwarden 未解锁"):
		m.runPinLine = msg
	case strings.Contains(msg, "Bitwarden 已解锁"):
		m.runPinLine = msg
	case event.Scope == "push.secrets" && strings.Contains(msg, "解锁页:"):
		m.runPinLine = msg
	case event.Level == app.EventWarn || event.Level == app.EventError:
		m.runPinLine = msg
	case event.Progress != nil && event.Progress.Phase == "done":
		m.runPinLine = msg
	}
}

func runPhaseLabel(phase string) string {
	switch phase {
	case "pull":
		return "Dec cache"
	case "dec":
		return "Dec 推送"
	case "secrets":
		return "Secrets"
	case "install":
		return "IDE 安装"
	case "done":
		return "完成"
	case "validate":
		return "路径校验"
	default:
		if phase == "" {
			return "进行中"
		}
		return phase
	}
}

func progressBarWidth(pageWidth int) int {
	w := pageWidth / 3
	if w < 10 {
		w = 10
	}
	if w > 24 {
		w = 24
	}
	return w
}

func renderRunProgressBar(current, total, width int) string {
	if total <= 0 {
		return "[----]"
	}
	if width < 8 {
		width = 8
	}
	innerWidth := width - 2
	filled := current * innerWidth / total
	if filled > innerWidth {
		filled = innerWidth
	}
	if current > 0 && filled == 0 {
		filled = 1
	}
	inner := strings.Repeat("=", filled) + strings.Repeat("-", innerWidth-filled)
	return "[" + inner + "]"
}

func formatRunLogLine(line string) string {
	if isRunImportantLine(line) {
		return shellWarnStyle.Render("▸ " + line)
	}
	return shellLogStyle.Render("· " + line)
}

func isRunImportantLine(line string) bool {
	return strings.Contains(line, "[auth]") ||
		strings.Contains(line, "解锁页:") ||
		strings.Contains(line, "Bitwarden 未解锁") ||
		strings.Contains(line, "Bitwarden 已解锁") ||
		strings.Contains(line, "Bitwarden 未配置") ||
		strings.Contains(line, "跳过 secrets") ||
		strings.Contains(line, "无法自动打开") ||
		strings.Contains(line, "失败") ||
		strings.Contains(line, "⚠")
}

func (m model) renderUpdatePanel() []string {
	lines := []string{shellTitleStyle.Render("Update")}
	switch m.updateStage {
	case "checking":
		lines = append(lines, shellMutedStyle.Render(fmt.Sprintf("检查更新中... 当前版本: %s", fallbackValue(m.currentVersion, "未知"))))
	case "confirm":
		if m.updateResult == nil {
			lines = append(lines, shellWarnStyle.Render("检查结果缺失，按 n/esc 返回"))
			return lines
		}
		lines = append(lines,
			fmt.Sprintf("当前版本: %s", m.updateResult.CurrentVersion),
			fmt.Sprintf("远端版本: %s", m.updateResult.LatestVersion),
			shellWarnStyle.Render("⚠️  自更新会替换当前 dec 二进制，属不可逆操作。"),
			shellMutedStyle.Render("按 y 确认下载并覆盖 · n/esc 取消"),
		)
	case "running":
		target := ""
		if m.updateResult != nil {
			target = m.updateResult.LatestVersion
		}
		lines = append(lines, shellMutedStyle.Render(fmt.Sprintf("正在下载并替换二进制到 %s ...", fallbackValue(target, "最新版本"))))
	case "done":
		if m.updateErr != nil {
			lines = append(lines,
				shellWarnStyle.Render("更新失败: "+m.updateErr.Error()),
				shellMutedStyle.Render("可改用手动覆盖安装："),
				"  "+updateManualInstallCommand(),
				shellMutedStyle.Render("GitHub 直连不稳定时改走 CDN 镜像："),
				"  "+updateMirrorInstallCommand(),
				shellMutedStyle.Render("按 esc/enter 关闭面板"),
			)
			return lines
		}
		if m.updateResult != nil && !m.updateResult.NeedUpdate {
			lines = append(lines,
				shellGoodStyle.Render(fmt.Sprintf("已是最新版本: %s", m.updateResult.CurrentVersion)),
				shellMutedStyle.Render("按 esc/enter 关闭面板"),
			)
			return lines
		}
		target := m.updateDoneVersion
		if target == "" && m.updateResult != nil {
			target = m.updateResult.LatestVersion
		}
		lines = append(lines,
			shellGoodStyle.Render(fmt.Sprintf("更新成功！已更新到 %s", fallbackValue(target, "最新版本"))),
			shellMutedStyle.Render("按 esc/enter 关闭面板。新版本将在下次启动 dec 时生效。"),
		)
	}
	return lines
}

func (m model) renderRemoveSelect() []string {
	bundles := m.enabledRemoveBundles()
	lines := []string{shellTitleStyle.Render("Remove 选择器")}
	lines = append(lines, fmt.Sprintf("筛选: %s · 共 %d 个 bundle", m.currentRemoveFilterLabel(), len(bundles)))
	if m.removeFilterInput {
		lines = append(lines, shellMutedStyle.Render("筛选输入中：输入关键字后按 Enter 应用，Esc 退出。"))
	} else {
		lines = append(lines, shellMutedStyle.Render("快捷键：j/k 移动 · enter/space 选中 · / 筛选 · c 清空 · esc 取消"))
	}
	if len(bundles) == 0 {
		if m.assets != nil && len(app.ListEnabledBundles(m.assets)) == 0 {
			lines = append(lines, shellWarnStyle.Render("当前没有已启用的 bundle。"))
			lines = append(lines, shellMutedStyle.Render("请先在 Bundles 页勾选 bundle 并保存，再执行 pull。"))
		} else {
			lines = append(lines, shellWarnStyle.Render("没有匹配筛选条件的 bundle。"))
			lines = append(lines, shellMutedStyle.Render("按 c 清空筛选，或 esc 退出。"))
		}
		return lines
	}

	for i, bo := range bundles {
		marker := " "
		if i == m.removeCursor {
			marker = ">"
		}
		vault := bo.Vault
		if vault == "" {
			vault = bo.Name
		}
		line := fmt.Sprintf("  %s %s / %s · %d 成员", marker, bo.Name, vault, len(bo.Members))
		if i == m.removeCursor {
			lines = append(lines, shellSelectedRow.Render(line))
		} else {
			lines = append(lines, shellLogStyle.Render(line))
		}
	}
	return lines
}

func (m model) renderRemoveConfirm() []string {
	lines := []string{shellTitleStyle.Render("Remove 确认")}
	if m.removeTarget == nil {
		lines = append(lines, shellWarnStyle.Render("未选择 bundle，按 esc 返回。"))
		return lines
	}
	vault := m.removeTarget.Vault
	if vault == "" {
		vault = m.removeTarget.Name
	}
	lines = append(lines,
		fmt.Sprintf("Bundle: %s", m.removeTarget.Name),
		fmt.Sprintf("Vault: %s", vault),
		fmt.Sprintf("成员数: %d", len(m.removeTarget.Members)),
		shellWarnStyle.Render("⚠️  删除操作不可逆：将从远端仓库删除整包、清理 IDE 与本地 cache，并从 enabled_bundles 移除。"),
		shellMutedStyle.Render("Bitwarden secrets bundle 不会自动删除，敏感文件需自行处理。"),
		shellMutedStyle.Render("按 y 确认执行 · n/esc 取消返回选择器"),
	)
	return lines
}

func (m model) renderSettingsPage(width int) string {
	if m.settingsErr != nil {
		return shellWarnStyle.Render("无法加载全局设置") + "\n\n" + m.settingsErr.Error()
	}
	if m.settings == nil {
		return shellMutedStyle.Render("Loading global settings...")
	}

	summary := []string{
		fmt.Sprintf("Repo URL: %s", fallbackValue(strings.TrimSpace(m.settingsRepoInput), "<none>")),
		fmt.Sprintf("当前远端: %s", fallbackValue(m.settings.ConnectedRepoURL, "未连接")),
		fmt.Sprintf("已选 IDE: %s", fallbackValue(strings.Join(normalizedStringList(m.settingsSelectedIDEs), ", "), "<none>")),
		fmt.Sprintf("生效 IDE: %s", fallbackValue(strings.Join(settingsEffectivePreview(m.settings, m.settingsSelectedIDEs), ", "), "<none>")),
		fmt.Sprintf("全局配置: %s", m.settings.ConfigPath),
		fmt.Sprintf("本机 Vars: %s", m.settings.VarsPath),
		formatWarnings(m.settings.IDEWarnings),
	}
	if m.settingsDirty {
		summary = append(summary, shellWarnStyle.Render("当前有未保存修改，按 s 保存。"))
	} else {
		summary = append(summary, shellMutedStyle.Render("当前全局设置与磁盘一致。"))
	}
	if m.settingsRepoEditing {
		summary = append(summary, shellMutedStyle.Render("Repo URL 输入中：输入后按 Enter 应用，Esc 退出。"))
	} else {
		summary = append(summary, shellMutedStyle.Render("快捷键：j/k 移动 · e 编辑 repo · space 切换 IDE · s 保存"))
	}
	if !m.settings.VarsFileReady {
		summary = append(summary, shellMutedStyle.Render("首次保存会创建 ~/.dec/local/vars.yaml 模板。"))
	}
	if m.savingSettings {
		summary = append(summary, shellWarnStyle.Render("正在保存全局设置..."))
	}

	list := m.renderSettingsList()
	detail := m.renderSettingsDetails()
	if width < 88 {
		return strings.Join(append(summary, "", list, "", detail), "\n")
	}

	leftWidth := width / 2
	rightWidth := width - leftWidth - 2
	left := lipgloss.NewStyle().Width(leftWidth).Render(list)
	right := lipgloss.NewStyle().Width(rightWidth).Render(detail)
	return strings.Join(summary, "\n") + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m model) renderSettingsList() string {
	lines := []string{shellTitleStyle.Render("Global Settings")}
	repoLine := fmt.Sprintf("%s Repo URL: %s", settingsCursorMarker(m.settingsCursor == 0 && m.focus != focusSidebar), fallbackValue(strings.TrimSpace(m.settingsRepoInput), "<none>"))
	if m.settingsCursor == 0 && m.focus != focusSidebar {
		lines = append(lines, shellSelectedRow.Render(repoLine))
	} else {
		lines = append(lines, shellLogStyle.Render(repoLine))
	}
	for idx, ideName := range m.settings.AvailableIDEs {
		selected := settingsContainsIDE(m.settingsSelectedIDEs, ideName)
		checked := " "
		if selected {
			checked = "x"
		}
		line := fmt.Sprintf("%s [%s] %s", settingsCursorMarker(m.settingsCursor == idx+1 && m.focus != focusSidebar), checked, ideName)
		switch {
		case m.settingsCursor == idx+1 && m.focus != focusSidebar:
			lines = append(lines, shellSelectedRow.Render(line))
		case selected:
			lines = append(lines, shellEnabledRow.Render(line))
		default:
			lines = append(lines, shellLogStyle.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) renderSettingsDetails() string {
	lines := []string{shellTitleStyle.Render("Details")}
	if m.settingsCursor == 0 {
		lines = append(lines,
			fmt.Sprintf("当前远端: %s", fallbackValue(m.settings.ConnectedRepoURL, "未连接")),
			fmt.Sprintf("Bare Repo: %s", fallbackValue(m.settings.ConnectedBarePath, "未连接")),
			fmt.Sprintf("配置文件: %s", m.settings.ConfigPath),
			fmt.Sprintf("本机 Vars: %s", m.settings.VarsPath),
			"保存时会先确保仓库连接，再写回 ~/.dec/config.yaml。",
		)
	} else {
		ideName := m.currentSettingsIDEName()
		lines = append(lines,
			fmt.Sprintf("IDE: %s", ideName),
			"启动 dec 或保存 Settings 时，会在用户级目录同步内置 dec skill 与 dec MCP。",
			"dec MCP 写入 ~/.cursor/mcp.json 等；Cursor 打开各项目时用 ${workspaceFolder} 作为项目根。",
			fmt.Sprintf("当前状态: %s", formatReady(settingsContainsIDE(m.settingsSelectedIDEs, ideName), "已选中", "未选中")),
		)
	}
	if m.settingsRepoEditing {
		lines = append(lines, "", shellWarnStyle.Render("Repo URL 输入模式已开启。"))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderAssetList() string {
	lines := []string{shellTitleStyle.Render("Bundle 列表")}
	mm := m
	mm.refreshAssetTree()
	treeRows := mm.assetTree.VisibleRows()
	if len(treeRows) == 0 {
		return strings.Join(lines, "\n")
	}
	for i, tr := range treeRows {
		marker := " "
		if m.focus != focusSidebar && i == mm.assetTree.Cursor {
			marker = ">"
		}
		bundleEnabled := false
		if p, ok := tr.Node.Payload.(assetTreePayload); ok {
			bundleEnabled = p.bundleEnabled
		}
		line := renderAssetTreeLine(tr, &mm.assetTree, marker, bundleEnabled)
		if m.focus != focusSidebar && i == mm.assetTree.Cursor {
			lines = append(lines, shellSelectedRow.Render(line))
			continue
		}
		if p, ok := tr.Node.Payload.(assetTreePayload); ok {
			if p.kind == assetRowBundle && p.bundleEnabled {
				lines = append(lines, shellEnabledRow.Render(line))
			} else {
				lines = append(lines, shellLogStyle.Render(line))
			}
			continue
		}
		lines = append(lines, shellLogStyle.Render(line))
	}
	return strings.Join(lines, "\n")
}

// renderAssetRowLine 生成单行文本。
func (m model) renderAssetRowLine(row assetRow, marker string) string {
	bo := m.assets.Bundles[row.bundleIndex]
	if row.kind == assetRowBundleMember {
		mb := bo.Members[row.memberIndex]
		return fmt.Sprintf("%s   ↳ %s / %s / %s", marker, mb.Type, mb.Vault, mb.Name)
	}
	checked := " "
	if row.bundleEnabled {
		checked = "x"
	}
	arrow := "▸"
	if m.assetTree.Expanded[assetBundleNodeID(bo.Name)] {
		arrow = "▾"
	}
	label := bo.Name
	if bo.Name != bo.Vault {
		label = fmt.Sprintf("%s (%s)", bo.Name, bo.Vault)
	}
	return fmt.Sprintf("%s [%s] %s %s · %d 个成员", marker, checked, arrow, label, len(bo.Members))
}

func (m model) renderAssetDetails() string {
	lines := []string{shellTitleStyle.Render("Details")}
	if m.assets != nil {
		if p, ok := m.assetPayloadAtCursor(); ok {
			switch p.kind {
			case assetRowBundle:
				bo := m.assets.Bundles[p.bundleIndex]
				status := "未选中"
				if m.bundleSelected(bo.Name) {
					status = "已选中（勾选后其成员会随 pull 一起下发）"
				}
				lines = append(lines,
					fmt.Sprintf("Bundle: %s", bo.Name),
					fmt.Sprintf("Vault: %s", bo.Vault),
					fmt.Sprintf("状态: %s", status),
					fmt.Sprintf("成员: %d 个", len(bo.Members)),
				)
				if m.assetTree.Expanded[assetBundleNodeID(bo.Name)] {
					lines = append(lines, "", shellTitleStyle.Render("成员列表"))
					for _, mb := range bo.Members {
						lines = append(lines, fmt.Sprintf("  · %s / %s / %s", mb.Type, mb.Vault, mb.Name))
					}
				} else if m.focus == focusContent {
					lines = append(lines, shellMutedStyle.Render("按 l 或 Enter 展开查看成员"))
				}
				if bo.Description != "" {
					lines = append(lines, "", bo.Description)
				}
				return strings.Join(lines, "\n")
			case assetRowBundleMember:
				bo := m.assets.Bundles[p.bundleIndex]
				mb := bo.Members[p.memberIndex]
				lines = append(lines,
					fmt.Sprintf("Bundle: %s", bo.Name),
					fmt.Sprintf("Type: %s", mb.Type),
					fmt.Sprintf("Vault: %s", mb.Vault),
					fmt.Sprintf("Name: %s", mb.Name),
					shellMutedStyle.Render("成员由 bundle 带入，只读。"),
				)
				return strings.Join(lines, "\n")
			}
		} else if row, ok := m.assetTreeRowAtCursor(); ok {
			lines = append(lines,
				fmt.Sprintf("目录: %s", row.Node.Label),
				shellMutedStyle.Render("按 l 展开 · h 折叠"),
			)
		}
	}
	if len(lines) == 1 {
		if item, ok := m.currentAssetItem(); ok {
			lines = append(lines,
				fmt.Sprintf("Vault: %s", item.Vault),
				fmt.Sprintf("Type: %s", item.Type),
				fmt.Sprintf("Name: %s", item.Name),
			)
		} else {
			lines = append(lines, "当前没有匹配的 bundle。")
		}
	}

	if m.assets != nil {
		lines = append(lines,
			"",
			fmt.Sprintf("Config: %s", m.assets.ConfigPath),
			fmt.Sprintf("Vars: %s", m.assets.VarsPath),
		)
		if !m.assets.VarsFileReady {
			lines = append(lines, "Vars 模板会在首次保存时创建。")
		}
	}
	if m.savingAssets {
		lines = append(lines, "", shellWarnStyle.Render("正在保存 bundle 选择..."))
	}
	return strings.Join(lines, "\n")
}

func (m model) currentSettingsIDEName() string {
	if m.settings == nil || m.settingsCursor <= 0 {
		return ""
	}
	idx := m.settingsCursor - 1
	if idx < 0 || idx >= len(m.settings.AvailableIDEs) {
		return ""
	}
	return m.settings.AvailableIDEs[idx]
}

func (m model) currentAssetItem() (app.AssetSelectionItem, bool) {
	if m.assets == nil {
		return app.AssetSelectionItem{}, false
	}
	p, ok := m.assetPayloadAtCursor()
	if !ok {
		return app.AssetSelectionItem{}, false
	}
	if p.kind == assetRowBundleMember {
		bo := m.assets.Bundles[p.bundleIndex]
		return bo.Members[p.memberIndex], true
	}
	// bundle 节点本身没有对应的 AssetSelectionItem。返回 false 让 Detail 面板走 bundle 分支。
	return app.AssetSelectionItem{}, false
}

// ------ Bundle-aware row model for Assets page ------

type assetRowKind int

const (
	assetRowBundle assetRowKind = iota
	assetRowBundleMember
)

// assetRow 描述 Bundles 页一行可见条目；光标索引以 assetTree.VisibleRows() 为准。
type assetRow struct {
	kind          assetRowKind
	bundleIndex   int // kind == assetRowBundle / assetRowBundleMember 时有效
	memberIndex   int // kind == assetRowBundleMember 时有效
	bundleEnabled bool
}

// bundleSelected 返回当前 TUI 侧 bundleSelection 是否包含该 bundle。
func (m model) bundleSelected(name string) bool {
	for _, n := range m.bundleSelection {
		if n == name {
			return true
		}
	}
	return false
}

func (m model) canNavigateSettings() bool {
	return m.settings != nil && m.settingsRowCount() > 0
}

func (m model) settingsRowCount() int {
	if m.settings == nil {
		return 0
	}
	return 1 + len(m.settings.AvailableIDEs)
}

func (m model) canNavigateAssets() bool {
	return m.assets != nil && m.assetTreeVisibleCount() > 0
}

func (m *model) normalizeAssetCursor() {
	m.refreshAssetTree()
	m.assetTree.normalizeCursor()
}

func (m *model) moveAssetCursor(delta int) {
	m.refreshAssetTree()
	m.assetTree.MoveCursor(delta)
}

func (m *model) normalizeSettingsCursor() {
	if m.settingsRowCount() == 0 {
		m.settingsCursor = 0
		return
	}
	if m.settingsCursor < 0 || m.settingsCursor >= m.settingsRowCount() {
		m.settingsCursor = 0
	}
}

func (m *model) moveSettingsCursor(delta int) {
	if !m.canNavigateSettings() {
		return
	}
	m.normalizeSettingsCursor()
	m.settingsCursor += delta
	if m.settingsCursor < 0 {
		m.settingsCursor = 0
	}
	if m.settingsCursor >= m.settingsRowCount() {
		m.settingsCursor = m.settingsRowCount() - 1
	}
}

func (m *model) beginSettingsRepoEdit() {
	if m.settings == nil {
		return
	}
	m.settingsCursor = 0
	m.settingsRepoEditing = true
	m.pushLog("Repo URL input opened")
}

func (m *model) toggleCurrentSettingsIDE() {
	ideName := m.currentSettingsIDEName()
	if strings.TrimSpace(ideName) == "" {
		return
	}
	if settingsContainsIDE(m.settingsSelectedIDEs, ideName) {
		m.settingsSelectedIDEs = settingsRemoveIDE(m.settingsSelectedIDEs, ideName)
		m.pushLog("IDE disabled: " + ideName)
	} else {
		m.settingsSelectedIDEs = append(m.settingsSelectedIDEs, ideName)
		m.pushLog("IDE enabled: " + ideName)
	}
	m.syncSettingsDirty()
}

func (m *model) syncSettingsDirty() {
	if m.settings == nil {
		m.settingsDirty = false
		return
	}
	currentRepo := strings.TrimSpace(m.settingsRepoInput)
	loadedRepo := strings.TrimSpace(m.settings.RepoURL)
	m.settingsDirty = currentRepo != loadedRepo || !equalNormalizedStrings(m.settingsSelectedIDEs, m.settings.SelectedIDEs)
}

func settingsContainsIDE(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func settingsRemoveIDE(values []string, target string) []string {
	target = strings.TrimSpace(target)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			continue
		}
		result = append(result, value)
	}
	return result
}

func equalNormalizedStrings(left, right []string) bool {
	leftNorm := normalizedStringList(left)
	rightNorm := normalizedStringList(right)
	if len(leftNorm) != len(rightNorm) {
		return false
	}
	for idx := range leftNorm {
		if leftNorm[idx] != rightNorm[idx] {
			return false
		}
	}
	return true
}

func normalizedStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func settingsEffectivePreview(state *app.GlobalSettingsState, selected []string) []string {
	if len(selected) > 0 {
		return normalizedStringList(selected)
	}
	if state == nil {
		return nil
	}
	if len(state.EffectiveIDEs) > 0 {
		return normalizedStringList(state.EffectiveIDEs)
	}
	return normalizedStringList(state.SelectedIDEs)
}

func settingsCursorMarker(active bool) string {
	if active {
		return ">"
	}
	return " "
}

// ------- Project Settings helpers -------

func (m model) canNavigateProjectSettings() bool {
	return m.projectSettings != nil && m.projectSettingsRowCount() > 0
}

func (m model) projectSettingsRowCount() int {
	if m.projectSettings == nil {
		return 0
	}
	return 1 + len(m.projectSettings.AvailableIDEs)
}

func (m *model) normalizeProjectSettingsCursor() {
	if m.projectSettingsRowCount() == 0 {
		m.projectSettingsCursor = 0
		return
	}
	if m.projectSettingsCursor < 0 || m.projectSettingsCursor >= m.projectSettingsRowCount() {
		m.projectSettingsCursor = 0
	}
}

func (m *model) moveProjectSettingsCursor(delta int) {
	if !m.canNavigateProjectSettings() {
		return
	}
	m.normalizeProjectSettingsCursor()
	m.projectSettingsCursor += delta
	if m.projectSettingsCursor < 0 {
		m.projectSettingsCursor = 0
	}
	if m.projectSettingsCursor >= m.projectSettingsRowCount() {
		m.projectSettingsCursor = m.projectSettingsRowCount() - 1
	}
}

func (m model) currentProjectSettingsIDEName() string {
	if m.projectSettings == nil || m.projectSettingsCursor <= 0 {
		return ""
	}
	idx := m.projectSettingsCursor - 1
	if idx < 0 || idx >= len(m.projectSettings.AvailableIDEs) {
		return ""
	}
	return m.projectSettings.AvailableIDEs[idx]
}

// toggleProjectOverride 切换 "覆盖/继承" 模式。首次开启覆盖时，预填当前生效 IDE。
func (m *model) toggleProjectOverride() {
	if m.projectSettings == nil {
		return
	}
	m.projectSettingsOverride = !m.projectSettingsOverride
	if m.projectSettingsOverride {
		if len(m.projectSettingsSelectedIDEs) == 0 {
			m.projectSettingsSelectedIDEs = cloneStrings(m.projectSettings.EffectiveIDEs)
		}
		m.pushLog("Project override enabled")
	} else {
		m.pushLog("Project override disabled (will inherit global on save)")
	}
	m.syncProjectSettingsDirty()
}

// toggleCurrentProjectIDE 在覆盖模式下切换光标所在的 IDE。继承模式下仅记录日志。
func (m *model) toggleCurrentProjectIDE() {
	ideName := m.currentProjectSettingsIDEName()
	if strings.TrimSpace(ideName) == "" {
		return
	}
	if !m.projectSettingsOverride {
		m.pushLog("当前处于继承模式，按 space 在第一行切换到覆盖模式后再选择 IDE")
		return
	}
	if settingsContainsIDE(m.projectSettingsSelectedIDEs, ideName) {
		m.projectSettingsSelectedIDEs = settingsRemoveIDE(m.projectSettingsSelectedIDEs, ideName)
		m.pushLog("Project IDE disabled: " + ideName)
	} else {
		m.projectSettingsSelectedIDEs = append(m.projectSettingsSelectedIDEs, ideName)
		m.pushLog("Project IDE enabled: " + ideName)
	}
	m.syncProjectSettingsDirty()
}

// clearProjectOverride 立即切回继承态，并清空本地选择。
func (m *model) clearProjectOverride() {
	if m.projectSettings == nil {
		return
	}
	m.projectSettingsOverride = false
	m.projectSettingsSelectedIDEs = nil
	m.pushLog("Project override cleared (will inherit global on save)")
	m.syncProjectSettingsDirty()
}

func (m *model) syncProjectSettingsDirty() {
	if m.projectSettings == nil {
		m.projectSettingsDirty = false
		return
	}
	if m.projectSettingsOverride != m.projectSettings.OverrideActive {
		m.projectSettingsDirty = true
		return
	}
	if !m.projectSettingsOverride {
		// 同属继承态；本地选择无意义。
		m.projectSettingsDirty = false
		return
	}
	m.projectSettingsDirty = !equalNormalizedStrings(m.projectSettingsSelectedIDEs, m.projectSettings.SelectedIDEs)
}

func (m *model) toggleCurrentAsset() {
	if m.assets == nil {
		return
	}
	p, ok := m.assetPayloadAtCursor()
	if !ok {
		return
	}
	switch p.kind {
	case assetRowBundle:
		bo := m.assets.Bundles[p.bundleIndex]
		if m.bundleSelected(bo.Name) {
			next := make([]string, 0, len(m.bundleSelection))
			for _, n := range m.bundleSelection {
				if n != bo.Name {
					next = append(next, n)
				}
			}
			m.bundleSelection = next
			m.pushLog("Bundle disabled: " + bo.Name)
		} else {
			m.bundleSelection = append(m.bundleSelection, bo.Name)
			m.pushLog("Bundle enabled: " + bo.Name)
		}
		m.assetsDirty = true
	case assetRowBundleMember:
		bo := m.assets.Bundles[p.bundleIndex]
		mb := bo.Members[p.memberIndex]
		m.pushLog(fmt.Sprintf("Member 由 bundle %q 带入，无法单独切换：%s / %s / %s", bo.Name, mb.Vault, mb.Type, mb.Name))
	}
}

// countSelectedBundleMembers 统计当前勾选 bundle 展开后的成员数（跨 bundle 去重）。
func (m model) countSelectedBundleMembers() int {
	if m.assets == nil {
		return 0
	}
	seen := make(map[string]struct{})
	for _, bo := range m.assets.Bundles {
		if !m.bundleSelected(bo.Name) {
			continue
		}
		for _, mb := range bo.Members {
			seen[mb.Type+"\x00"+mb.Vault+"\x00"+mb.Name] = struct{}{}
		}
	}
	return len(seen)
}

// assetsCursorOnBundle 判断当前光标是否落在 bundle 节点行（不是成员行）。
func (m model) assetsCursorOnBundle() bool {
	p, ok := m.assetPayloadAtCursor()
	return ok && p.kind == assetRowBundle
}

func (m *model) expandAssetAtCursor() {
	if m.assets == nil {
		return
	}
	m.refreshAssetTree()
	row, ok := m.assetTree.currentRow()
	if !ok || !treeNodeExpandable(row.Node) {
		return
	}
	m.assetTree.Expanded[row.Node.ID] = true
	if p, ok := row.Node.Payload.(assetTreePayload); ok && p.kind == assetRowBundle {
		bo := m.assets.Bundles[p.bundleIndex]
		nodeID := assetBundleNodeID(bo.Name)
		seen := make(map[string]struct{})
		for _, mb := range bo.Members {
			sub := assetTypeSubDir(mb.Type)
			if _, dup := seen[sub]; dup {
				continue
			}
			seen[sub] = struct{}{}
			m.assetTree.Expanded[nodeID+"/"+sub] = true
		}
		m.pushLog("Bundle 展开: " + bo.Name)
	} else {
		m.pushLog("展开: " + row.Node.Label)
	}
	m.refreshAssetTree()
}

// expandCurrentBundle 展开 bundle 及其类型子目录。
func (m *model) expandCurrentBundle() {
	if !m.assetsCursorOnBundle() {
		return
	}
	m.expandAssetAtCursor()
}

// collapseCurrentBundle 折叠当前 bundle 节点（光标可在 bundle 或成员行）。
func (m *model) collapseCurrentBundle() {
	if m.assets == nil {
		return
	}
	m.refreshAssetTree()
	if m.assetTree.CollapseAtCursor() {
		name := ""
		if p, ok := m.assetPayloadAtCursor(); ok && p.bundleIndex < len(m.assets.Bundles) {
			name = m.assets.Bundles[p.bundleIndex].Name
		}
		m.pushLog("Bundle 折叠: " + name)
		m.refreshAssetTree()
	}
}

func (m model) currentAssetFilterLabel() string {
	filter := strings.TrimSpace(m.assetFilter)
	if filter == "" {
		return "<none>"
	}
	return filter
}

func (m model) isHomePage() bool {
	return m.pages[m.pageIndex] == "Home"
}

func (m model) hasVaultInferencePrompt() bool {
	return m.vaultInference != nil && !m.vaultInferenceDismissed && !m.applyingVaultProject
}

func (m model) isBundlesPage() bool {
	return m.pages[m.pageIndex] == "Bundles"
}

func (m model) isSettingsPage() bool {
	return m.pages[m.pageIndex] == "Settings"
}

func (m model) isProjectPage() bool {
	return m.pages[m.pageIndex] == "Project"
}

func (m model) isRunPage() bool {
	return m.pages[m.pageIndex] == "Run"
}

func (m model) renderLogs(width, height int) string {
	start := 0
	if len(m.logs) > height-2 {
		start = len(m.logs) - (height - 2)
	}
	visible := m.logs[start:]
	lines := make([]string, 0, len(visible)+1)
	lines = append(lines, shellTitleStyle.Render("Logs"))
	for _, line := range visible {
		lines = append(lines, shellLogStyle.Render("- "+line))
	}
	return shellCardStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m model) renderStatusBar(width int) string {
	left := "q quit | j/k nav | l/h in-out | r refresh"
	if m.isHomePage() && m.hasVaultInferencePrompt() {
		left = "y/Enter 应用 · n 跳过 | q quit | r refresh"
	} else if m.isHomePage() && m.applyingVaultProject {
		left = "正在应用 vault project..."
	}
	right := fmt.Sprintf("page %s", m.pages[m.pageIndex])
	if m.isBundlesPage() && m.assets != nil {
		right = fmt.Sprintf("%s | %d/%d bundles", right, len(m.bundleSelection), len(m.assets.Bundles))
		if m.assetsDirty {
			right += " | modified"
		}
		if m.assetFilterInput {
			right += " | filter"
		}
	} else if m.isSettingsPage() && m.settings != nil {
		right = fmt.Sprintf("%s | %d IDEs", right, len(normalizedStringList(m.settingsSelectedIDEs)))
		if m.settingsDirty {
			right += " | modified"
		}
		if m.settingsRepoEditing {
			right += " | repo-input"
		}
		if m.savingSettings {
			right += " | saving"
		}
	} else if m.isProjectPage() && m.projectSettings != nil {
		modeTag := "inherit"
		if m.projectSettingsOverride {
			modeTag = "override"
		}
		right = fmt.Sprintf("%s | %s", right, modeTag)
		if m.projectSettingsOverride {
			right = fmt.Sprintf("%s | %d IDEs", right, len(normalizedStringList(m.projectSettingsSelectedIDEs)))
		}
		if m.projectSettingsDirty {
			right += " | modified"
		}
		if m.savingProjectSettings {
			right += " | saving"
		}
	} else if m.isRunPage() {
		right = fmt.Sprintf("%s | %s", right, m.runStatusLabel())
		if m.runProgress != nil && (m.runningPull || m.runningRemove) {
			right += fmt.Sprintf(" | %s %d/%d", runPhaseLabel(m.runProgress.Phase), m.runProgress.Current, m.runProgress.Total)
		}
		if m.removeStage != "" {
			right += " | remove-" + m.removeStage
		}
		if m.updateStage != "" {
			right += " | update-" + m.updateStage
		}
	} else if m.overview != nil {
		right = fmt.Sprintf("%s | %d bundles", right, countOverviewEnabledBundles(m.overview))
	}
	// shellStatusBar 的 Padding(0, 1) 会在左右各占 1 列，实际可写内容宽度 = width - 2。
	// 必须按内容区预算，否则在窄终端下会被 lipgloss 的 Width() 悄悄换行，右侧页面状态被截断。
	innerWidth := width - 2
	if innerWidth < 1 {
		innerWidth = 1
	}
	// 预算保护：右侧状态承载页面信息更关键。
	// 当 left + right 已超内容区宽度（含中文宽字符），丢弃左侧快捷键提示，避免页面状态被截断。
	rightWidth := lipgloss.Width(right)
	leftWidth := lipgloss.Width(left)
	if leftWidth+rightWidth+1 > innerWidth {
		left = ""
		leftWidth = 0
	}
	available := innerWidth - leftWidth - rightWidth
	if available < 1 {
		available = 1
	}
	return shellStatusBar.Width(width).Render(left + strings.Repeat(" ", available) + right)
}

func (m model) currentSummary() string {
	if m.overviewErr != nil {
		return "Overview unavailable"
	}
	if m.isBundlesPage() {
		if m.assetsErr != nil {
			return "Bundle selection unavailable"
		}
		if m.assets == nil {
			return "Loading bundle selection"
		}
		if m.assetsDirty {
			return fmt.Sprintf("Unsaved bundles: %d/%d enabled", len(m.bundleSelection), len(m.assets.Bundles))
		}
		return fmt.Sprintf("Bundles ready, %d/%d enabled", len(m.bundleSelection), len(m.assets.Bundles))
	}
	if m.isSettingsPage() {
		if m.settingsErr != nil {
			return "Global settings unavailable"
		}
		if m.settings == nil {
			return "Loading global settings"
		}
		if m.savingSettings {
			return "Saving global settings"
		}
		if m.settingsRepoEditing {
			return "Editing repo URL"
		}
		if m.settingsDirty {
			return fmt.Sprintf("Unsaved settings: %d IDEs", len(normalizedStringList(m.settingsSelectedIDEs)))
		}
		return fmt.Sprintf("Settings ready, %d IDEs", len(normalizedStringList(m.settingsSelectedIDEs)))
	}
	if m.isRunPage() {
		if m.runningPull {
			if m.runMode == "push" {
				return "Push running"
			}
			return "Pull running"
		}
		if m.runningRemove {
			return "Remove running"
		}
		if m.updatingBinary {
			return "Update running"
		}
		if m.removeStage == "select" {
			return "Selecting bundle to remove"
		}
		if m.removeStage == "confirm" {
			return "Confirming remove"
		}
		if m.pushStage == "summary" {
			return "Confirming push (summary)"
		}
		if m.pushStage == "confirm" {
			return "Confirming push (final)"
		}
		if m.updateStage == "checking" {
			return "Checking for updates"
		}
		if m.updateStage == "confirm" {
			return "Confirming update"
		}
		if m.updateStage == "done" {
			if m.updateErr != nil {
				return "Last update failed"
			}
			if m.updateResult != nil && !m.updateResult.NeedUpdate {
				return "Already up to date"
			}
			return "Last update succeeded"
		}
		if m.runErr != nil {
			if m.runMode == "push" {
				return "Last push failed"
			}
			return "Last pull failed"
		}
		if m.removeErr != nil {
			return "Last remove failed"
		}
		if m.runResult != nil {
			return fmt.Sprintf("Last pull: %d ok / %d failed", m.runResult.PulledCount, m.runResult.FailedCount)
		}
		if m.pushResult != nil {
			return fmt.Sprintf("Last push: dec %d · secrets +%d/~%d",
				m.pushResult.DecPushedCount, m.pushResult.SecretsCreatedCount, m.pushResult.SecretsUpdatedCount)
		}
		if m.removeResult != nil {
			return fmt.Sprintf("Last remove: bundle %s", m.removeResult.BundleName)
		}
		return "Run page ready"
	}
	if m.isProjectPage() {
		if m.projectSettingsErr != nil {
			return "Project settings unavailable"
		}
		if m.projectSettings == nil {
			return "Loading project settings"
		}
		if m.savingProjectSettings {
			return "Saving project settings"
		}
		if m.projectSettingsDirty {
			return "Unsaved project IDE settings"
		}
		mode := "inherit"
		if m.projectSettingsOverride {
			mode = "override"
		}
		return fmt.Sprintf("Project ready, IDE %s", mode)
	}
	if m.overview == nil {
		return "Loading project state"
	}
	if m.applyingVaultProject {
		return "Applying inferred vault project"
	}
	if m.hasVaultInferencePrompt() {
		return fmt.Sprintf("Vault project %s inferred, awaiting confirmation", m.vaultInference.ProjectName)
	}
	if !m.overview.RepoConnected {
		return "Repository not connected yet"
	}
	if !m.overview.ProjectConfigReady {
		return "Project config missing"
	}
	return fmt.Sprintf("Repo ready, %d bundles enabled", countOverviewEnabledBundles(m.overview))
}

func formatProjectNameDisplay(overview *app.ProjectOverview) string {
	if overview == nil {
		return "<unknown>"
	}
	name := overview.ProjectName
	if !overview.ProjectNameFromConfig {
		return fmt.Sprintf("%s (目录推断，未写入 config)", name)
	}
	return name
}

// countOverviewAvailableBundles 优先按扫描到的 bundle 列表计数；仓库未连接或扫描失败时
// Bundles 为空，退回 config 层记录的计数。
func countOverviewAvailableBundles(overview *app.ProjectOverview) int {
	if overview == nil {
		return 0
	}
	if len(overview.Bundles) > 0 {
		return len(overview.Bundles)
	}
	return overview.AvailableBundleCount
}

func countOverviewEnabledBundles(overview *app.ProjectOverview) int {
	if overview == nil {
		return 0
	}
	if len(overview.Bundles) > 0 {
		n := 0
		for _, b := range overview.Bundles {
			if b.Enabled {
				n++
			}
		}
		return n
	}
	return overview.EnabledBundleCount
}

func formatOverviewEnabledBundleNames(overview *app.ProjectOverview) string {
	if overview == nil {
		return "<无>"
	}
	var names []string
	for _, b := range overview.Bundles {
		if b.Enabled {
			names = append(names, b.Name)
		}
	}
	if len(names) == 0 {
		if overview.EnabledBundleCount > 0 {
			return fmt.Sprintf("<%d 个，详情见 Bundles 页>", overview.EnabledBundleCount)
		}
		return "<无>"
	}
	return strings.Join(names, ", ")
}

func suggestNextAction(overview *app.ProjectOverview, vaultInferencePending, vaultInferenceDismissed bool) string {
	if overview == nil {
		return "等待项目概览加载完成"
	}
	if !overview.RepoConnected {
		return "先切到 Settings 页连接资产仓库"
	}
	if vaultInferencePending {
		return "在 Home 页确认推断的 vault project（y 应用 / n 跳过），或切到 Bundles 页手动选择"
	}
	if !overview.ProjectConfigReady {
		if vaultInferenceDismissed {
			return "切到 Bundles 页选择 bundle 并保存"
		}
		return "先在 Home 页初始化 project，或切到 Bundles 页选择 bundle 并保存"
	}
	if countOverviewEnabledBundles(overview) == 0 {
		return "当前还没有启用 bundle，请切到 Bundles 页勾选并保存"
	}
	return "可以切到 Run 页执行 pull"
}

func formatInferenceBundleNames(bundles []string) string {
	if len(bundles) == 0 {
		return "<无>"
	}
	return strings.Join(bundles, ", ")
}

func (m model) runStatusLabel() string {
	switch {
	case m.runningPull:
		return shellGoodStyle.Render("执行中")
	case m.runningRemove:
		return shellGoodStyle.Render("删除中")
	case m.updatingBinary:
		return shellGoodStyle.Render("更新中")
	case m.runErr != nil:
		return shellWarnStyle.Render("失败")
	case m.removeErr != nil:
		return shellWarnStyle.Render("Remove 失败")
	case m.updateErr != nil:
		return shellWarnStyle.Render("Update 失败")
	case m.removeStage == "select":
		return shellWarnStyle.Render("Remove · 选择 bundle")
	case m.removeStage == "confirm":
		return shellWarnStyle.Render("Remove · 确认删除")
	case m.pushStage == "summary":
		return shellWarnStyle.Render("Push · 摘要确认")
	case m.pushStage == "confirm":
		return shellWarnStyle.Render("Push · 最终确认")
	case m.updateStage == "checking":
		return shellMutedStyle.Render("Update 检查中")
	case m.updateStage == "confirm":
		return shellWarnStyle.Render("Update 确认中")
	case m.updateStage == "done":
		if m.updateErr != nil {
			return shellWarnStyle.Render("Update 失败")
		}
		return shellGoodStyle.Render("Update 已完成")
	case m.pushResult != nil:
		return shellGoodStyle.Render("Push 已完成")
	case m.runResult != nil:
		return shellGoodStyle.Render("已完成")
	case m.removeResult != nil:
		return shellGoodStyle.Render("Remove 已完成")
	default:
		return shellMutedStyle.Render("空闲")
	}
}

func formatWarnings(warnings []string) string {
	if len(warnings) == 0 {
		return "IDE 警告: 无"
	}
	return "IDE 警告: " + strings.Join(warnings, " | ")
}

func formatReady(ok bool, readyText, pendingText string) string {
	if ok {
		return shellGoodStyle.Render(readyText)
	}
	return shellWarnStyle.Render(pendingText)
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func wrapLines(width int, lines []string) string {
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, lipgloss.NewStyle().Width(width).Render(trimmed))
	}
	return strings.Join(filtered, "\n")
}
