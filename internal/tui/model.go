package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shichao402/Dec/pkg/app"
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
	overview *app.ProjectOverview
	err      error
}

type assetsLoadedMsg struct {
	state *app.AssetSelectionState
	err   error
}

type assetsSavedMsg struct {
	result *app.SaveAssetSelectionResult
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

type runEventMsg struct {
	event app.OperationEvent
}

type runCompletedMsg struct {
	result *app.PullProjectAssetsResult
	err    error
}

type removeEventMsg struct {
	event app.OperationEvent
}

type removeCompletedMsg struct {
	result *app.RemoveAssetResult
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

var runPullOperation = func(projectRoot string, reporter app.Reporter) (*app.PullProjectAssetsResult, error) {
	return app.PullProjectAssets(projectRoot, "", reporter)
}

var runRemoveOperation = func(input app.RemoveAssetInput, reporter app.Reporter) (*app.RemoveAssetResult, error) {
	return app.RemoveAsset(input, reporter)
}

var loadGlobalSettingsOperation = func(reporter app.Reporter) (*app.GlobalSettingsState, error) {
	return app.LoadGlobalSettings(reporter)
}

var saveGlobalSettingsOperation = func(input app.SaveGlobalSettingsInput, reporter app.Reporter) (*app.SaveGlobalSettingsResult, error) {
	return app.SaveGlobalSettings(input, reporter)
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

var updateCheckOperation = func(currentVersion string) (*update.CheckResult, error) {
	return update.Check(currentVersion)
}

var updateDoUpdateOperation = func(currentVersion string) error {
	return update.DoUpdate(currentVersion)
}

var updateManualInstallCommand = func() string {
	return update.ManualInstallCommand()
}

type model struct {
	projectRoot          string
	currentVersion       string
	pages                []string
	pageIndex            int
	width                int
	height               int
	overview             *app.ProjectOverview
	overviewErr          error
	assets               *app.AssetSelectionState
	assetsErr            error
	settings             *app.GlobalSettingsState
	settingsErr          error
	logs                 []string
	assetCursor          int
	assetFilter          string
	assetFilterInput     bool
	assetsDirty          bool
	savingAssets         bool
	settingsCursor       int
	settingsDirty        bool
	savingSettings       bool
	settingsRepoInput    string
	settingsRepoEditing  bool
	settingsSelectedIDEs []string
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
	runningPull          bool
	runProgress          *app.Progress
	runEvents            []string
	runResult            *app.PullProjectAssetsResult
	runErr               error
	runStream            <-chan tea.Msg
	runMode              string // "pull" | "remove" | "update"
	removeStage          string // "", "select", "confirm", "running"
	removeCursor         int
	removeFilter         string
	removeFilterInput    bool
	removeTarget         *app.AssetSelectionItem
	runningRemove        bool
	removeResult         *app.RemoveAssetResult
	removeErr            error
	updateStage          string // "", "checking", "result", "confirm", "running", "done"
	updateResult         *update.CheckResult
	updateErr            error
	updateDoneVersion    string
	updatingBinary       bool
}

func newModel(projectRoot, currentVersion string) model {
	return model{
		projectRoot:    projectRoot,
		currentVersion: currentVersion,
		pages:          []string{"Home", "Assets", "Project", "Run", "Settings"},
		logs: []string{
			"TUI shell ready",
			"Loading project overview...",
			"Loading asset selection...",
			"Loading global settings...",
		},
	}
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
	case overviewLoadedMsg:
		m.overview = msg.overview
		m.overviewErr = msg.err
		if msg.err != nil {
			m.pushLog("Overview load failed: " + msg.err.Error())
			return m, nil
		}
		m.pushLog(fmt.Sprintf("Overview loaded: %d enabled / %d available assets", msg.overview.EnabledCount, msg.overview.AvailableCount))
		return m, nil
	case assetsLoadedMsg:
		m.assets = msg.state
		m.assetsErr = msg.err
		m.savingAssets = false
		m.assetsDirty = false
		if msg.err != nil {
			m.pushLog("Asset selection load failed: " + msg.err.Error())
			return m, nil
		}
		m.normalizeAssetCursor()
		if msg.state != nil {
			m.pushLog(fmt.Sprintf("Asset selection loaded: %d items", len(msg.state.Items)))
		}
		return m, nil
	case assetsSavedMsg:
		m.savingAssets = false
		if msg.err != nil {
			m.pushLog("Asset selection save failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			m.pushLog(fmt.Sprintf("Asset selection saved: %d enabled / %d available", msg.result.EnabledCount, msg.result.AvailableCount))
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
	case runEventMsg:
		m.recordRunEvent(msg.event)
		if m.runStream != nil {
			return m, waitRunMsg(m.runStream)
		}
		return m, nil
	case runCompletedMsg:
		m.runningPull = false
		m.runStream = nil
		m.runResult = msg.result
		m.runErr = msg.err
		if msg.err != nil {
			m.pushLog("Run pull failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			m.pushLog(fmt.Sprintf("Run pull finished: %d pulled / %d failed", msg.result.PulledCount, msg.result.FailedCount))
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
			m.pushLog(fmt.Sprintf("Run remove finished: [%s] %s (vault: %s)", msg.result.Type, msg.result.Name, msg.result.Vault))
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
		if m.assetFilterInput && m.isAssetsPage() {
			return m.handleAssetFilterInput(msg)
		}
		if m.settingsRepoEditing && m.isSettingsPage() {
			return m.handleSettingsRepoInput(msg)
		}
		if m.removeFilterInput && m.isRunPage() {
			return m.handleRemoveFilterInput(msg)
		}
		if m.isRunPage() && m.removeStage != "" && !m.runningRemove {
			return m.handleRemoveStageKey(msg)
		}
		if m.isRunPage() && m.updateStage != "" && !m.updatingBinary {
			return m.handleUpdateStageKey(msg)
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.pushLog("Exit requested")
			return m, tea.Quit
		case "tab", "l", "right":
			m.pageIndex = (m.pageIndex + 1) % len(m.pages)
			m.pushLog("Switched to " + m.pages[m.pageIndex])
			return m, nil
		case "shift+tab", "h", "left":
			m.pageIndex = (m.pageIndex - 1 + len(m.pages)) % len(m.pages)
			m.pushLog("Switched to " + m.pages[m.pageIndex])
			return m, nil
		case "j", "down":
			if m.isAssetsPage() {
				if m.canNavigateAssets() {
					m.moveAssetCursor(1)
				}
				return m, nil
			}
			if m.isSettingsPage() {
				if m.canNavigateSettings() {
					m.moveSettingsCursor(1)
				}
				return m, nil
			}
			if m.isProjectPage() {
				if m.canNavigateProjectSettings() {
					m.moveProjectSettingsCursor(1)
				}
				return m, nil
			}
			m.pageIndex = (m.pageIndex + 1) % len(m.pages)
			m.pushLog("Switched to " + m.pages[m.pageIndex])
			return m, nil
		case "k", "up":
			if m.isAssetsPage() {
				if m.canNavigateAssets() {
					m.moveAssetCursor(-1)
				}
				return m, nil
			}
			if m.isSettingsPage() {
				if m.canNavigateSettings() {
					m.moveSettingsCursor(-1)
				}
				return m, nil
			}
			if m.isProjectPage() {
				if m.canNavigateProjectSettings() {
					m.moveProjectSettingsCursor(-1)
				}
				return m, nil
			}
			m.pageIndex = (m.pageIndex - 1 + len(m.pages)) % len(m.pages)
			m.pushLog("Switched to " + m.pages[m.pageIndex])
			return m, nil
		case "r":
			m.pushLog("Refreshing project overview, assets, and global settings")
			return m, m.refreshCmd()
		case "/":
			if m.isAssetsPage() {
				m.assetFilterInput = true
				m.pushLog("Asset filter input opened")
			}
			return m, nil
		case "c":
			if m.isAssetsPage() && strings.TrimSpace(m.assetFilter) != "" {
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
			if m.isAssetsPage() && !m.savingAssets {
				m.toggleCurrentAsset()
				return m, nil
			}
			if m.isSettingsPage() && !m.savingSettings {
				if m.settingsCursor == 0 {
					if msg.String() == "enter" {
						m.beginSettingsRepoEdit()
					}
				} else {
					m.toggleCurrentSettingsIDE()
				}
				return m, nil
			}
			if m.isProjectPage() && !m.savingProjectSettings && m.projectSettings != nil && m.projectSettingsErr == nil {
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
			}
			return m, nil
		case "s":
			if m.isAssetsPage() && !m.savingAssets && m.assets != nil && m.assetsErr == nil {
				m.savingAssets = true
				m.pushLog("Saving asset selection")
				return m, saveAssetsCmd(m.projectRoot, cloneAssetSelectionItems(m.assets.Items))
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
			if m.isRunPage() && !m.runningPull && !m.runningRemove && !m.updatingBinary && m.updateStage == "" {
				return m, m.startPullRun()
			}
			return m, nil
		case "x":
			if m.isRunPage() && !m.runningPull && !m.runningRemove && !m.updatingBinary && m.updateStage == "" {
				m.beginRemoveSelection()
			}
			return m, nil
		case "u":
			if m.isRunPage() && !m.runningPull && !m.runningRemove && !m.updatingBinary && m.removeStage == "" {
				return m, m.startUpdateCheck()
			}
			return m, nil
		}
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
	return tea.Batch(loadOverviewCmd(m.projectRoot), loadAssetsCmd(m.projectRoot), loadSettingsCmd(), loadProjectSettingsCmd(m.projectRoot))
}

func loadOverviewCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		overview, err := app.LoadProjectOverview(projectRoot)
		return overviewLoadedMsg{overview: overview, err: err}
	}
}

func loadAssetsCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		state, err := app.LoadAssetSelection(projectRoot, nil)
		return assetsLoadedMsg{state: state, err: err}
	}
}

func saveAssetsCmd(projectRoot string, items []app.AssetSelectionItem) tea.Cmd {
	return func() tea.Msg {
		result, err := app.SaveAssetSelection(projectRoot, items, nil)
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

func startPullRunCmd(projectRoot string, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			result, err := runPullOperation(projectRoot, app.ReporterFunc(func(event app.OperationEvent) {
				stream <- runEventMsg{event: event}
			}))
			stream <- runCompletedMsg{result: result, err: err}
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

func cloneAssetSelectionItems(items []app.AssetSelectionItem) []app.AssetSelectionItem {
	return append([]app.AssetSelectionItem(nil), items...)
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
	m.runningPull = true
	m.runMode = "pull"
	m.runProgress = nil
	m.runEvents = nil
	m.runResult = nil
	m.runErr = nil
	m.runStream = stream
	m.pushLog("Run page started pull")
	return tea.Batch(startPullRunCmd(m.projectRoot, stream), waitRunMsg(stream))
}

func (m *model) beginRemoveSelection() {
	if m.assets == nil || len(m.enabledRemoveCandidates()) == 0 {
		m.pushLog("没有可删除的已启用资产")
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

func (m model) enabledRemoveCandidates() []app.AssetSelectionItem {
	if m.assets == nil {
		return nil
	}
	items := make([]app.AssetSelectionItem, 0, len(m.assets.Items))
	filter := strings.ToLower(strings.TrimSpace(m.removeFilter))
	for _, item := range m.assets.Items {
		if !item.Enabled {
			continue
		}
		if filter != "" {
			haystack := strings.ToLower(strings.Join([]string{item.Vault, item.Type, item.Name}, " "))
			if !strings.Contains(haystack, filter) {
				continue
			}
		}
		items = append(items, item)
	}
	return items
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
	candidates := m.enabledRemoveCandidates()
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
			if m.removeCursor >= len(m.enabledRemoveCandidates()) {
				m.removeCursor = 0
			}
			m.pushLog("Remove 筛选已清空")
		}
		return m, nil
	case "j", "down":
		if len(candidates) == 0 {
			return m, nil
		}
		m.removeCursor++
		if m.removeCursor >= len(candidates) {
			m.removeCursor = len(candidates) - 1
		}
		return m, nil
	case "k", "up":
		if len(candidates) == 0 {
			return m, nil
		}
		m.removeCursor--
		if m.removeCursor < 0 {
			m.removeCursor = 0
		}
		return m, nil
	case "enter", " ":
		if len(candidates) == 0 {
			return m, nil
		}
		if m.removeCursor < 0 || m.removeCursor >= len(candidates) {
			m.removeCursor = 0
		}
		target := candidates[m.removeCursor]
		m.removeTarget = &target
		m.removeStage = "confirm"
		m.pushLog(fmt.Sprintf("Remove 目标选中: [%s] %s (vault: %s)", target.Type, target.Name, target.Vault))
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
	m.runResult = nil
	m.runErr = nil
	m.removeResult = nil
	m.removeErr = nil
	m.runStream = stream
	input := app.RemoveAssetInput{
		ProjectRoot: m.projectRoot,
		Type:        m.removeTarget.Type,
		Name:        m.removeTarget.Name,
		Vault:       m.removeTarget.Vault,
		Confirmed:   true,
	}
	m.pushLog(fmt.Sprintf("Run page started remove: [%s] %s", input.Type, input.Name))
	return tea.Batch(startRemoveRunCmd(input, stream), waitRunMsg(stream))
}

func startRemoveRunCmd(input app.RemoveAssetInput, stream chan<- tea.Msg) tea.Cmd {
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
		err := updateDoUpdateOperation(currentVersion)
		return updateDoneMsg{targetVersion: target, err: err}
	}
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
	items = append(items, shellMutedStyle.Render("tab switch / j k move"))
	for idx, page := range m.pages {
		style := shellNavStyle
		if idx == m.pageIndex {
			style = shellActiveNav
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
	case "Assets":
		return m.renderAssetsPage(width)
	case "Project":
		return m.renderProjectPage(width)
	case "Run":
		return m.renderRunPage(width)
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

	return wrapLines(width, []string{
		fmt.Sprintf("仓库: %s", formatReady(m.overview.RepoConnected, "已连接", "未连接")),
		fmt.Sprintf("远端仓库: %s", fallbackValue(m.overview.RepoRemoteURL, "未连接")),
		fmt.Sprintf("项目配置: %s", formatReady(m.overview.ProjectConfigReady, "已初始化", "未初始化")),
		fmt.Sprintf("变量文件: %s", formatReady(m.overview.VarsFileReady, "已存在", "未生成")),
		fmt.Sprintf("可用资产: %d", m.overview.AvailableCount),
		fmt.Sprintf("已启用资产: %d", m.overview.EnabledCount),
		fmt.Sprintf("默认 IDE: %s", strings.Join(m.overview.IDEs, ", ")),
		fmt.Sprintf("编辑器: %s", fallbackValue(m.overview.Editor, "未配置")),
		fmt.Sprintf("建议下一步: %s", suggestNextAction(m.overview)),
		formatWarnings(m.overview.IDEWarnings),
	})
}

func (m model) renderAssetsPage(width int) string {
	if m.assetsErr != nil {
		return shellWarnStyle.Render("无法加载资产选择") + "\n\n" + m.assetsErr.Error()
	}
	if m.assets == nil {
		return shellMutedStyle.Render("Loading asset selection...")
	}

	summary := []string{
		fmt.Sprintf("筛选: %s", m.currentAssetFilterLabel()),
		fmt.Sprintf("资产总数: %d | 已启用: %d", len(m.assets.Items), m.countEnabledAssets()),
	}
	if m.assetsDirty {
		summary = append(summary, shellWarnStyle.Render("当前有未保存修改，按 s 保存。"))
	} else {
		summary = append(summary, shellMutedStyle.Render("当前资产选择与磁盘一致。"))
	}
	if m.assetFilterInput {
		summary = append(summary, shellMutedStyle.Render("筛选输入中：输入关键字后按 Enter 应用，Esc 退出。"))
	} else {
		summary = append(summary, shellMutedStyle.Render("快捷键：j/k 移动 · space/enter 切换 · s 保存 · / 筛选 · c 清空筛选"))
	}
	if !m.assets.ExistingConfig {
		summary = append(summary, shellMutedStyle.Render("首次保存会创建 .dec/config.yaml 与 .dec/vars.yaml。"))
	}

	if len(m.assets.Items) == 0 {
		return strings.Join(append(summary, "", "仓库中还没有可选资产。"), "\n")
	}

	visible := m.filteredAssetIndices()
	if len(visible) == 0 {
		return strings.Join(append(summary, "", "当前筛选没有结果。"), "\n")
	}

	list := m.renderAssetList(visible)
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
		fmt.Sprintf("项目配置: %s", m.projectSettings.ConfigPath),
		fmt.Sprintf("变量文件: %s", m.projectSettings.VarsPath),
		fmt.Sprintf("当前模式: %s", modeLabel),
		fmt.Sprintf("项目 IDE: %s", fallbackValue(strings.Join(normalizedStringList(m.projectSettingsSelectedIDEs), ", "), "<none>")),
		fmt.Sprintf("全局默认: %s", fallbackValue(strings.Join(normalizedStringList(m.projectSettings.GlobalIDEs), ", "), "<未配置>")),
		fmt.Sprintf("生效 IDE: %s", fallbackValue(strings.Join(projectEffectivePreview(m.projectSettings, m.projectSettingsOverride, m.projectSettingsSelectedIDEs), ", "), "<none>")),
		formatWarnings(m.projectSettings.IDEWarnings),
	}
	if !m.projectSettings.ProjectConfigReady {
		summary = append(summary, shellMutedStyle.Render("尚未初始化项目配置。首次保存项目级覆盖会创建 .dec/config.yaml。"))
	}
	if m.projectSettingsDirty {
		summary = append(summary, shellWarnStyle.Render("当前有未保存修改，按 s 保存。"))
	} else {
		summary = append(summary, shellMutedStyle.Render("当前项目设置与磁盘一致。"))
	}

	// 初始化 / 刷新 available 入口状态
	repoConnected := m.overview != nil && m.overview.RepoConnected
	initKeyHint := ""
	if m.projectSettings.ProjectConfigReady {
		initKeyHint = "R 刷新 available"
	} else {
		initKeyHint = "i 初始化项目配置"
	}
	summary = append(summary, shellMutedStyle.Render(fmt.Sprintf("快捷键：j/k 移动 · space 切换模式/IDE · s 保存 · c 清除覆盖 · %s", initKeyHint)))
	if !repoConnected {
		summary = append(summary, shellWarnStyle.Render("未连接仓库，请先在 Settings 页配置 Repo URL，然后再来初始化 / 刷新 available。"))
	}
	if m.initializingProjectConfig {
		summary = append(summary, shellWarnStyle.Render("正在扫描仓库资产..."))
	}
	if m.lastInitErr != nil {
		summary = append(summary, shellWarnStyle.Render("初始化失败: "+m.lastInitErr.Error()))
	}
	if m.lastInitResult != nil && m.lastInitErr == nil {
		switch {
		case m.lastInitResult.ProjectConfig == nil:
			summary = append(summary, shellWarnStyle.Render(fmt.Sprintf("仓库暂无资产：扫描到 %d 个可用资产。", m.lastInitResult.AssetCount)))
		case m.lastInitResult.ExistingConfig:
			summary = append(summary, shellGoodStyle.Render(fmt.Sprintf("刷新完成，共 %d 个可用资产。tab 到 Assets 页选择启用项。", m.lastInitResult.AssetCount)))
		default:
			summary = append(summary, shellGoodStyle.Render(fmt.Sprintf("初始化完成，发现 %d 个可用资产。tab 到 Assets 页选择启用项。", m.lastInitResult.AssetCount)))
		}
		if m.lastInitResult.VarsCreated {
			summary = append(summary, shellMutedStyle.Render("已生成 .dec/vars.yaml 模板。"))
		}
	}
	if m.savingProjectSettings {
		summary = append(summary, shellWarnStyle.Render("正在保存项目设置..."))
	}

	list := m.renderProjectSettingsList()
	detail := m.renderProjectSettingsDetails()
	if width < 88 {
		return strings.Join(append(summary, "", list, "", detail), "\n")
	}

	leftWidth := width / 2
	rightWidth := width - leftWidth - 2
	left := lipgloss.NewStyle().Width(leftWidth).Render(list)
	right := lipgloss.NewStyle().Width(rightWidth).Render(detail)
	return strings.Join(summary, "\n") + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m model) renderProjectSettingsList() string {
	lines := []string{shellTitleStyle.Render("Project Settings")}
	override := m.projectSettingsOverride
	checked := " "
	if override {
		checked = "x"
	}
	overrideLine := fmt.Sprintf("%s [%s] 覆盖全局 IDE", settingsCursorMarker(m.projectSettingsCursor == 0), checked)
	switch {
	case m.projectSettingsCursor == 0:
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
		line := fmt.Sprintf("%s [%s] %s", settingsCursorMarker(m.projectSettingsCursor == idx+1), mark, ideName)
		switch {
		case m.projectSettingsCursor == idx+1:
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

func (m model) renderRunPage(width int) string {
	lines := []string{
		fmt.Sprintf("状态: %s", m.runStatusLabel()),
		shellMutedStyle.Render("快捷键：p 执行 pull · x 删除资产 · u 自更新 · s 触发当前页主动作 · r 刷新概览"),
	}
	if m.runProgress != nil {
		lines = append(lines, fmt.Sprintf("阶段: %s (%d/%d)", fallbackValue(m.runProgress.Phase, "working"), m.runProgress.Current, m.runProgress.Total))
	}
	if m.runResult != nil {
		lines = append(lines,
			fmt.Sprintf("结果: 请求 %d | 成功 %d | 失败 %d", m.runResult.RequestedCount, m.runResult.PulledCount, m.runResult.FailedCount),
			fmt.Sprintf("IDE: %s", fallbackValue(strings.Join(m.runResult.EffectiveIDEs, ", "), "<none>")),
		)
		if strings.TrimSpace(m.runResult.VersionCommit) != "" {
			lines = append(lines, fmt.Sprintf("Commit: %s", m.runResult.VersionCommit))
		}
	}
	if m.removeResult != nil {
		lines = append(lines, fmt.Sprintf("Remove 结果: [%s] %s (vault: %s)", m.removeResult.Type, m.removeResult.Name, m.removeResult.Vault))
		if strings.TrimSpace(m.removeResult.VersionCommit) != "" {
			lines = append(lines, fmt.Sprintf("Remove Commit: %s", m.removeResult.VersionCommit))
		}
	}
	if m.runErr != nil {
		lines = append(lines, shellWarnStyle.Render("错误: "+m.runErr.Error()))
	}
	if m.removeErr != nil {
		lines = append(lines, shellWarnStyle.Render("Remove 错误: "+m.removeErr.Error()))
	}

	switch m.removeStage {
	case "select":
		lines = append(lines, "")
		lines = append(lines, m.renderRemoveSelect()...)
	case "confirm":
		lines = append(lines, "")
		lines = append(lines, m.renderRemoveConfirm()...)
	}

	if m.updateStage != "" {
		lines = append(lines, "")
		lines = append(lines, m.renderUpdatePanel()...)
	}

	if len(m.runEvents) == 0 && m.removeStage == "" && m.updateStage == "" {
		lines = append(lines, shellMutedStyle.Render("执行日志会显示在这里。当前接入 pull / remove / update。"))
		return wrapLines(width, lines)
	}

	formatted := make([]string, 0, len(lines)+len(m.runEvents)+2)
	formatted = append(formatted, lines...)
	if len(m.runEvents) > 0 {
		formatted = append(formatted, shellTitleStyle.Render("Execution Log"))
		for _, line := range m.runEvents {
			formatted = append(formatted, "- "+line)
		}
	}
	return wrapLines(width, formatted)
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
	candidates := m.enabledRemoveCandidates()
	lines := []string{shellTitleStyle.Render("Remove 选择器")}
	lines = append(lines, fmt.Sprintf("筛选: %s", m.currentRemoveFilterLabel()))
	if m.removeFilterInput {
		lines = append(lines, shellMutedStyle.Render("筛选输入中：输入关键字后按 Enter 应用，Esc 退出。"))
	} else {
		lines = append(lines, shellMutedStyle.Render("快捷键：j/k 移动 · enter/space 选中 · / 筛选 · c 清空 · esc 取消"))
	}
	if len(candidates) == 0 {
		lines = append(lines, "没有匹配的已启用资产。")
		return lines
	}
	for idx, item := range candidates {
		marker := " "
		if idx == m.removeCursor {
			marker = ">"
		}
		line := fmt.Sprintf("%s [%s] %s / %s", marker, item.Type, item.Vault, item.Name)
		if idx == m.removeCursor {
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
		lines = append(lines, shellWarnStyle.Render("未选择目标资产，按 esc 返回。"))
		return lines
	}
	lines = append(lines,
		fmt.Sprintf("Type: %s", m.removeTarget.Type),
		fmt.Sprintf("Vault: %s", m.removeTarget.Vault),
		fmt.Sprintf("Name: %s", m.removeTarget.Name),
		shellWarnStyle.Render("⚠️  删除操作不可逆，将从远端仓库、IDE、本地缓存一并清理。"),
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
	repoLine := fmt.Sprintf("%s Repo URL: %s", settingsCursorMarker(m.settingsCursor == 0), fallbackValue(strings.TrimSpace(m.settingsRepoInput), "<none>"))
	if m.settingsCursor == 0 {
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
		line := fmt.Sprintf("%s [%s] %s", settingsCursorMarker(m.settingsCursor == idx+1), checked, ideName)
		switch {
		case m.settingsCursor == idx+1:
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
			"保存时会在用户级目录安装内置 dec / dec-extract-asset。",
			fmt.Sprintf("当前状态: %s", formatReady(settingsContainsIDE(m.settingsSelectedIDEs, ideName), "已选中", "未选中")),
		)
	}
	if m.settingsRepoEditing {
		lines = append(lines, "", shellWarnStyle.Render("Repo URL 输入模式已开启。"))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderAssetList(visible []int) string {
	lines := []string{shellTitleStyle.Render("Asset List")}
	for _, idx := range visible {
		item := m.assets.Items[idx]
		marker := " "
		if idx == m.assetCursor {
			marker = ">"
		}
		checked := " "
		if item.Enabled {
			checked = "x"
		}
		line := fmt.Sprintf("%s [%s] %s / %s / %s", marker, checked, item.Vault, item.Type, item.Name)
		switch {
		case idx == m.assetCursor:
			lines = append(lines, shellSelectedRow.Render(line))
		case item.Enabled:
			lines = append(lines, shellEnabledRow.Render(line))
		default:
			lines = append(lines, shellLogStyle.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) renderAssetDetails() string {
	lines := []string{shellTitleStyle.Render("Details")}
	item, ok := m.currentAssetItem()
	if ok {
		lines = append(lines,
			fmt.Sprintf("Vault: %s", item.Vault),
			fmt.Sprintf("Type: %s", item.Type),
			fmt.Sprintf("Name: %s", item.Name),
			fmt.Sprintf("Enabled: %s", formatReady(item.Enabled, "yes", "no")),
		)
	} else {
		lines = append(lines, "当前没有匹配的资产。")
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
		lines = append(lines, "", shellWarnStyle.Render("正在保存资产选择..."))
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
	visible := m.filteredAssetIndices()
	if len(visible) == 0 {
		return app.AssetSelectionItem{}, false
	}
	for _, idx := range visible {
		if idx == m.assetCursor {
			return m.assets.Items[idx], true
		}
	}
	return m.assets.Items[visible[0]], true
}

func (m model) filteredAssetIndices() []int {
	if m.assets == nil {
		return nil
	}
	filter := strings.ToLower(strings.TrimSpace(m.assetFilter))
	visible := make([]int, 0, len(m.assets.Items))
	for idx, item := range m.assets.Items {
		if filter == "" {
			visible = append(visible, idx)
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{item.Vault, item.Type, item.Name}, " "))
		if strings.Contains(haystack, filter) {
			visible = append(visible, idx)
		}
	}
	return visible
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
	return m.assets != nil && len(m.filteredAssetIndices()) > 0
}

func (m *model) normalizeAssetCursor() {
	visible := m.filteredAssetIndices()
	if len(visible) == 0 {
		m.assetCursor = 0
		return
	}
	for _, idx := range visible {
		if idx == m.assetCursor {
			return
		}
	}
	m.assetCursor = visible[0]
}

func (m *model) moveAssetCursor(delta int) {
	visible := m.filteredAssetIndices()
	if len(visible) == 0 {
		return
	}
	position := 0
	found := false
	for idx, assetIndex := range visible {
		if assetIndex == m.assetCursor {
			position = idx
			found = true
			break
		}
	}
	if !found {
		m.assetCursor = visible[0]
		return
	}
	position += delta
	if position < 0 {
		position = 0
	}
	if position >= len(visible) {
		position = len(visible) - 1
	}
	m.assetCursor = visible[position]
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
	m.normalizeAssetCursor()
	if m.assetCursor < 0 || m.assetCursor >= len(m.assets.Items) {
		return
	}
	m.assets.Items[m.assetCursor].Enabled = !m.assets.Items[m.assetCursor].Enabled
	m.assetsDirty = true
	item := m.assets.Items[m.assetCursor]
	state := "disabled"
	if item.Enabled {
		state = "enabled"
	}
	m.pushLog(fmt.Sprintf("Asset %s: %s / %s / %s", state, item.Vault, item.Type, item.Name))
}

func (m model) countEnabledAssets() int {
	if m.assets == nil {
		return 0
	}
	count := 0
	for _, item := range m.assets.Items {
		if item.Enabled {
			count++
		}
	}
	return count
}

func (m model) currentAssetFilterLabel() string {
	filter := strings.TrimSpace(m.assetFilter)
	if filter == "" {
		return "<none>"
	}
	return filter
}

func (m model) isAssetsPage() bool {
	return m.pages[m.pageIndex] == "Assets"
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
	left := "q quit | tab switch | r refresh"
	right := fmt.Sprintf("page %s", m.pages[m.pageIndex])
	if m.isAssetsPage() && m.assets != nil {
		right = fmt.Sprintf("%s | %d/%d enabled", right, m.countEnabledAssets(), len(m.assets.Items))
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
		if m.runProgress != nil {
			right += fmt.Sprintf(" | %s %d/%d", fallbackValue(m.runProgress.Phase, "working"), m.runProgress.Current, m.runProgress.Total)
		}
		if m.removeStage != "" {
			right += " | remove-" + m.removeStage
		}
		if m.updateStage != "" {
			right += " | update-" + m.updateStage
		}
	} else if m.overview != nil {
		right = fmt.Sprintf("%s | enabled %d", right, m.overview.EnabledCount)
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
	if m.isAssetsPage() {
		if m.assetsErr != nil {
			return "Asset selection unavailable"
		}
		if m.assets == nil {
			return "Loading asset selection"
		}
		if m.assetsDirty {
			return fmt.Sprintf("Unsaved selection: %d enabled", m.countEnabledAssets())
		}
		return fmt.Sprintf("Assets ready, %d enabled", m.countEnabledAssets())
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
			return "Pull running"
		}
		if m.runningRemove {
			return "Remove running"
		}
		if m.updatingBinary {
			return "Update running"
		}
		if m.removeStage == "select" {
			return "Selecting asset to remove"
		}
		if m.removeStage == "confirm" {
			return "Confirming remove"
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
			return "Last pull failed"
		}
		if m.removeErr != nil {
			return "Last remove failed"
		}
		if m.runResult != nil {
			return fmt.Sprintf("Last pull: %d ok / %d failed", m.runResult.PulledCount, m.runResult.FailedCount)
		}
		if m.removeResult != nil {
			return fmt.Sprintf("Last remove: [%s] %s", m.removeResult.Type, m.removeResult.Name)
		}
		return "Run page ready"
	}
	if m.overview == nil {
		return "Loading project state"
	}
	if !m.overview.RepoConnected {
		return "Repository not connected yet"
	}
	if !m.overview.ProjectConfigReady {
		return "Project config missing"
	}
	return fmt.Sprintf("Repo ready, %d assets enabled", m.overview.EnabledCount)
}

func suggestNextAction(overview *app.ProjectOverview) string {
	if overview == nil {
		return "等待项目概览加载完成"
	}
	if !overview.RepoConnected {
		return "先切到 Settings 页连接资产仓库，或运行 dec config repo <url>"
	}
	if !overview.ProjectConfigReady {
		return "先切到 Assets 页选择资产并保存，或运行 dec config init"
	}
	if overview.EnabledCount == 0 {
		return "当前还没有启用资产，先切到 Assets 页选择并保存"
	}
	return "可以切到 Run 页执行 pull，或继续使用 dec pull"
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
		return shellMutedStyle.Render("Remove 选择中")
	case m.removeStage == "confirm":
		return shellWarnStyle.Render("Remove 确认中")
	case m.updateStage == "checking":
		return shellMutedStyle.Render("Update 检查中")
	case m.updateStage == "confirm":
		return shellWarnStyle.Render("Update 确认中")
	case m.updateStage == "done":
		if m.updateErr != nil {
			return shellWarnStyle.Render("Update 失败")
		}
		return shellGoodStyle.Render("Update 已完成")
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
