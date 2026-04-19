package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shichao402/Dec/pkg/app"
)

var (
	shellTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	shellMutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	shellCardStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("67")).Padding(1, 2)
	shellActiveNav  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	shellNavStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	shellStatusBar  = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	shellLogStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	shellWarnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	shellGoodStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Bold(true)
)

type overviewLoadedMsg struct {
	overview *app.ProjectOverview
	err      error
}

type model struct {
	projectRoot string
	pages       []string
	pageIndex   int
	width       int
	height      int
	overview    *app.ProjectOverview
	err         error
	logs        []string
}

func newModel(projectRoot string) model {
	return model{
		projectRoot: projectRoot,
		pages:       []string{"Home", "Assets", "Project", "Run", "Settings"},
		logs: []string{
			"TUI shell ready",
			"Loading project overview...",
		},
	}
}

func (m model) Init() tea.Cmd {
	return loadOverviewCmd(m.projectRoot)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case overviewLoadedMsg:
		m.err = msg.err
		m.overview = msg.overview
		if msg.err != nil {
			m.pushLog("Overview load failed: " + msg.err.Error())
			return m, nil
		}
		m.pushLog(fmt.Sprintf("Overview loaded: %d enabled / %d available assets", msg.overview.EnabledCount, msg.overview.AvailableCount))
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.pushLog("Exit requested")
			return m, tea.Quit
		case "j", "down", "tab", "l", "right":
			m.pageIndex = (m.pageIndex + 1) % len(m.pages)
			m.pushLog("Switched to " + m.pages[m.pageIndex])
			return m, nil
		case "k", "up", "shift+tab", "h", "left":
			m.pageIndex = (m.pageIndex - 1 + len(m.pages)) % len(m.pages)
			m.pushLog("Switched to " + m.pages[m.pageIndex])
			return m, nil
		case "r":
			m.pushLog("Refreshing project overview")
			return m, loadOverviewCmd(m.projectRoot)
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

func loadOverviewCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		overview, err := app.LoadProjectOverview(projectRoot)
		return overviewLoadedMsg{overview: overview, err: err}
	}
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
	items = append(items, shellMutedStyle.Render("j/k or Tab to switch"))
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
	if m.err != nil {
		return shellWarnStyle.Render("Failed to load overview") + "\n\n" + m.err.Error()
	}
	if m.overview == nil {
		return shellMutedStyle.Render("Loading project overview...")
	}

	switch m.pages[m.pageIndex] {
	case "Home":
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
	case "Assets":
		return wrapLines(width, []string{
			"阶段 2 只接入了 Shell 骨架，资产浏览页将在下一阶段承接。",
			"当前可以继续使用 dec config init / dec pull 维持现有 CLI 流程。",
		})
	case "Project":
		return wrapLines(width, []string{
			fmt.Sprintf("项目配置路径: %s", m.overview.ProjectConfigPath),
			fmt.Sprintf("变量文件路径: %s", m.overview.VarsPath),
			"后续阶段会把项目初始化、变量编辑和 IDE 选择迁到此页。",
		})
	case "Run":
		return wrapLines(width, []string{
			"执行页将在后续阶段接管 pull / push / remove / update。",
			"本阶段先落默认入口和统一 shell，不改变现有 CLI 子命令语义。",
		})
	default:
		return wrapLines(width, []string{
			"Settings 页当前只保留占位，用于承接后续全局设置与调试开关。",
			"可通过 DEC_NO_TUI=1 临时回退到传统 CLI。",
		})
	}
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
	left := "q quit | r refresh | j/k switch"
	right := fmt.Sprintf("page %s", m.pages[m.pageIndex])
	if m.overview != nil {
		right = fmt.Sprintf("%s | enabled %d", right, m.overview.EnabledCount)
	}
	available := width - lipgloss.Width(left) - lipgloss.Width(right)
	if available < 1 {
		available = 1
	}
	return shellStatusBar.Width(width).Render(left + strings.Repeat(" ", available) + right)
}

func (m model) currentSummary() string {
	if m.err != nil {
		return "Overview unavailable"
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
		return "先运行 dec config repo <url> 连接资产仓库"
	}
	if !overview.ProjectConfigReady {
		return "先运行 dec config init 初始化项目配置"
	}
	if overview.EnabledCount == 0 {
		return "当前还没有启用资产，先用 dec config init 调整 enabled"
	}
	return "可以继续执行 dec pull，同步已启用资产"
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
