package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shichao402/Dec/pkg/app"
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

type runEventMsg struct {
	event app.OperationEvent
}

type runCompletedMsg struct {
	result *app.PullProjectAssetsResult
	err    error
}

var runPullOperation = func(projectRoot string, reporter app.Reporter) (*app.PullProjectAssetsResult, error) {
	return app.PullProjectAssets(projectRoot, "", reporter)
}

var loadGlobalSettingsOperation = func(reporter app.Reporter) (*app.GlobalSettingsState, error) {
	return app.LoadGlobalSettings(reporter)
}

var saveGlobalSettingsOperation = func(input app.SaveGlobalSettingsInput, reporter app.Reporter) (*app.SaveGlobalSettingsResult, error) {
	return app.SaveGlobalSettings(input, reporter)
}

type model struct {
	projectRoot          string
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
	runningPull          bool
	runProgress          *app.Progress
	runEvents            []string
	runResult            *app.PullProjectAssetsResult
	runErr               error
	runStream            <-chan tea.Msg
}

func newModel(projectRoot string) model {
	return model{
		projectRoot: projectRoot,
		pages:       []string{"Home", "Assets", "Project", "Run", "Settings"},
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
	case tea.KeyMsg:
		if m.assetFilterInput && m.isAssetsPage() {
			return m.handleAssetFilterInput(msg)
		}
		if m.settingsRepoEditing && m.isSettingsPage() {
			return m.handleSettingsRepoInput(msg)
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
			if m.isRunPage() && !m.runningPull {
				return m, m.startPullRun()
			}
			return m, nil
		case "p":
			if m.isRunPage() && !m.runningPull {
				return m, m.startPullRun()
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

	sidebarWidth := 18
	if width >= 110 {
		sidebarWidth = 22
	}
	mainWidth := width - sidebarWidth - 1
	if mainWidth < 42 {
		mainWidth = 42
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
	return tea.Batch(loadOverviewCmd(m.projectRoot), loadAssetsCmd(m.projectRoot), loadSettingsCmd())
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
	m.runProgress = nil
	m.runEvents = nil
	m.runResult = nil
	m.runErr = nil
	m.runStream = stream
	m.pushLog("Run page started pull")
	return tea.Batch(startPullRunCmd(m.projectRoot, stream), waitRunMsg(stream))
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
	if m.overviewErr != nil {
		return shellWarnStyle.Render("Failed to load project details") + "\n\n" + m.overviewErr.Error()
	}
	if m.overview == nil {
		return shellMutedStyle.Render("Loading project overview...")
	}
	return wrapLines(width, []string{
		fmt.Sprintf("项目配置路径: %s", m.overview.ProjectConfigPath),
		fmt.Sprintf("变量文件路径: %s", m.overview.VarsPath),
		"后续阶段会把项目初始化、变量编辑和 IDE 选择迁到此页。",
	})
}

func (m model) renderRunPage(width int) string {
	lines := []string{
		fmt.Sprintf("状态: %s", m.runStatusLabel()),
		shellMutedStyle.Render("快捷键：p 执行 pull · s 也可触发当前页主动作 · r 刷新概览"),
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
	if m.runErr != nil {
		lines = append(lines, shellWarnStyle.Render("错误: "+m.runErr.Error()))
	}
	if len(m.runEvents) == 0 {
		lines = append(lines, shellMutedStyle.Render("执行日志会显示在这里。当前阶段先接入 pull，后续再覆盖 push / remove / update。"))
		return wrapLines(width, lines)
	}

	formatted := make([]string, 0, len(lines)+len(m.runEvents)+2)
	formatted = append(formatted, lines...)
	formatted = append(formatted, shellTitleStyle.Render("Execution Log"))
	for _, line := range m.runEvents {
		formatted = append(formatted, "- "+line)
	}
	return wrapLines(width, formatted)
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
	} else if m.isRunPage() {
		right = fmt.Sprintf("%s | %s", right, m.runStatusLabel())
		if m.runProgress != nil {
			right += fmt.Sprintf(" | %s %d/%d", fallbackValue(m.runProgress.Phase, "working"), m.runProgress.Current, m.runProgress.Total)
		}
	} else if m.overview != nil {
		right = fmt.Sprintf("%s | enabled %d", right, m.overview.EnabledCount)
	}
	available := width - lipgloss.Width(left) - lipgloss.Width(right)
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
		if m.runErr != nil {
			return "Last pull failed"
		}
		if m.runResult != nil {
			return fmt.Sprintf("Last pull: %d ok / %d failed", m.runResult.PulledCount, m.runResult.FailedCount)
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
	case m.runErr != nil:
		return shellWarnStyle.Render("失败")
	case m.runResult != nil:
		return shellGoodStyle.Render("已完成")
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
