package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// layoutMetrics 统一终端尺寸预算，保证横向合计不超过 width。
type layoutMetrics struct {
	width         int
	height        int
	sidebarWidth  int
	mainWidth     int
	statusHeight  int
	contentHeight int
	innerWidth    int
}

func computeLayoutMetrics(width, height, statusHeight int) layoutMetrics {
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	if statusHeight < 1 {
		statusHeight = 1
	}

	sidebarWidth := 16
	switch {
	case width < 80:
		sidebarWidth = 12
	case width >= 110:
		sidebarWidth = 18
	}

	// 侧栏卡与主列卡各贡献左右边框；两卡并排时相邻边共 4 列外框。
	mainWidth := width - sidebarWidth - 4
	if mainWidth < 1 {
		mainWidth = 1
	}

	contentHeight := height - statusHeight
	if contentHeight < 8 {
		contentHeight = 8
	}

	innerWidth := mainWidth - cardChromeHorizontal
	if innerWidth < 1 {
		innerWidth = 1
	}

	return layoutMetrics{
		width:         width,
		height:        height,
		sidebarWidth:  sidebarWidth,
		mainWidth:     mainWidth,
		statusHeight:  statusHeight,
		contentHeight: contentHeight,
		innerWidth:    innerWidth,
	}
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

	if m.serverRestartStage == "confirm" || m.serverRestartStage == "running" ||
		(m.serverRestartStage == "done" && m.serverRestartReason != "update") {
		statusBar := m.renderStatusBar(width)
		body := m.renderServerRestartOverlay(width)
		return lipgloss.JoinVertical(lipgloss.Left, body, statusBar)
	}

	statusBar := m.renderStatusBar(width)
	lm := computeLayoutMetrics(width, height, lipgloss.Height(statusBar))

	sidebar := m.renderSidebar(lm.sidebarWidth, lm.contentHeight)
	main := m.renderMain(lm.mainWidth, lm.contentHeight, lm.innerWidth)

	sideH := lipgloss.Height(sidebar)
	mainH := lipgloss.Height(main)
	if sideH > lm.contentHeight {
		sidebar = clipBlockHeight(sidebar, lm.contentHeight)
	}
	if mainH > lm.contentHeight {
		main = clipBlockHeight(main, lm.contentHeight)
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
	available := height - lipgloss.Height(statusBar)
	if available < 1 {
		available = 1
	}
	content = clipBlockHeight(content, available)
	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}

func (m model) renderSidebar(width, height int) string {
	items := make([]string, 0, len(m.pages)+1)
	items = append(items, shellTitleStyle.Render("Dec Shell"))
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

	innerH := height - cardChromeVertical
	if innerH < 1 {
		innerH = 1
	}
	return shellCardStyle.Width(width).Render(padLines(clipLines(strings.Join(items, "\n"), innerH), innerH))
}

func (m model) renderMain(width, height, innerWidth int) string {
	header := fitLine(m.renderPageHeader(innerWidth), innerWidth)
	headerBlock := shellCardStyle.Width(width).Render(header)
	headerH := lipgloss.Height(headerBlock)

	bodyHeight := height - headerH
	if bodyHeight < cardChromeVertical+1 {
		bodyHeight = cardChromeVertical + 1
	}
	innerBodyH := bodyHeight - cardChromeVertical
	if innerBodyH < 1 {
		innerBodyH = 1
	}

	pageBody := padLines(clipLines(m.renderPageBody(innerWidth, innerBodyH), innerBodyH), innerBodyH)
	body := shellCardStyle.Width(width).Render(pageBody)
	main := lipgloss.JoinVertical(lipgloss.Left, headerBlock, body)
	return clipBlockHeight(main, height)
}

func (m model) renderPageHeader(innerWidth int) string {
	page := m.pages[m.pageIndex]
	summary := m.currentSummary()
	line := fmt.Sprintf("%s · %s", page, summary)
	if m.isHomePage() || m.isProjectPage() {
		root := compactPath(m.projectRoot, max(8, innerWidth/3))
		line = fmt.Sprintf("%s · %s · %s", page, root, summary)
	}
	return fitLine(shellTitleStyle.Render(line), innerWidth)
}

func (m model) renderPageBody(width, height int) string {
	switch m.pages[m.pageIndex] {
	case "Home":
		return m.renderHomePage(width)
	case "Bundles":
		return m.renderBundlesPage(width, height)
	case "Project":
		return m.renderProjectPage(width)
	case "Run":
		return m.renderRunPage(width)
	case "Remote":
		return m.renderDeletePage(width, height)
	default:
		return m.renderSettingsPage(width)
	}
}

// mainLayoutMetrics 复算 View 使用的尺寸预算，供渲染前的视口估算使用。
func (m model) mainLayoutMetrics() layoutMetrics {
	width := m.width
	if width <= 0 {
		width = 100
	}
	height := m.height
	if height <= 0 {
		height = 30
	}
	statusH := 1
	if bar := m.renderStatusBar(width); bar != "" {
		statusH = lipgloss.Height(bar)
	}
	return computeLayoutMetrics(width, height, statusH)
}

// mainInnerBodyHeight 估计主卡内正文可用行数（与 renderMain 预算一致）。
func (m model) mainInnerBodyHeight() int {
	lm := m.mainLayoutMetrics()
	header := fitLine(m.renderPageHeader(lm.innerWidth), lm.innerWidth)
	headerH := lipgloss.Height(shellCardStyle.Width(lm.mainWidth).Render(header))
	bodyHeight := lm.contentHeight - headerH
	if bodyHeight < cardChromeVertical+1 {
		bodyHeight = cardChromeVertical + 1
	}
	innerBodyH := bodyHeight - cardChromeVertical
	if innerBodyH < 1 {
		return 1
	}
	return innerBodyH
}

func (m *model) syncTreeViewports() {
	lm := m.mainLayoutMetrics()
	bodyH := m.mainInnerBodyHeight()
	m.deleteTree.SetViewport(max(1, bodyH-m.remoteListChromeHeight(lm.innerWidth)))
	m.assetTree.SetViewport(max(1, bodyH-m.bundlesListChromeLines()))
}

func (m model) bundlesListChromeLines() int {
	n := 1 // 状态行
	if m.configInitMode {
		n++
	}
	if m.assetsDirty {
		n++
	}
	if m.assetFilterInput {
		n++
	}
	if m.assets != nil && !m.assets.ExistingConfig {
		n++
	}
	if filter := m.currentAssetFilterLabel(); filter != "<none>" {
		// 筛选项合进状态行
	}
	// 「Bundle 列表」标题 + 与 Detail 并列时的滚动提示
	n += 2
	return n
}

func padLines(content string, height int) string {
	if height <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if content == "" {
		lines = nil
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m model) renderStatusBar(width int) string {
	left := m.statusBarLeftHints()
	right := fmt.Sprintf("page %s", m.pages[m.pageIndex])
	if busy := m.ioBusyLabel(); busy != "" {
		// IO 忙碌时右侧也标 busy，窄宽度丢掉 left 时仍能看到状态。
		right = fmt.Sprintf("%s | %s", right, busy)
		if m.isRunPage() && m.runProgress != nil && (m.runningPull || m.runningRemove) {
			right += fmt.Sprintf(" | %s %d/%d", runPhaseLabel(m.runProgress.Phase), m.runProgress.Current, m.runProgress.Total)
		}
	} else if m.isRemotePage() {
		selected := len(m.selectedDeleteItems())
		right = fmt.Sprintf("%s | %d items · %d selected", right, len(m.deleteCandidates), selected)
		if m.deleteIncludeRemote {
			right += " | +remote"
		}
	} else if m.isBundlesPage() && m.assets != nil {
		right = fmt.Sprintf("%s | %d/%d bundles", right, len(m.bundleSelection), len(m.assets.Bundles))
		if m.assetsDirty {
			right += " | modified"
		}
		if m.assetFilterInput {
			right += " | filter"
		}
	} else if m.isSettingsPage() && m.settings != nil {
		right = fmt.Sprintf("%s | %s", right, m.settingsCountsLabel())
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

	innerWidth := width - 2
	if innerWidth < 1 {
		innerWidth = 1
	}
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

func clipLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}
	return strings.Join(lines[:maxLines], "\n")
}

func clipBlockHeight(content string, maxHeight int) string {
	if maxHeight <= 0 {
		return ""
	}
	if lipgloss.Height(content) <= maxHeight {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) > maxHeight {
		lines = lines[:maxHeight]
	}
	for lipgloss.Height(strings.Join(lines, "\n")) > maxHeight && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func fitLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(line) <= width {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}

func wrapLines(width int, lines []string) string {
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// 保留左侧缩进（树形列表依赖），只去掉行尾空白。
		filtered = append(filtered, lipgloss.NewStyle().Width(width).Render(strings.TrimRight(line, " \t")))
	}
	return strings.Join(filtered, "\n")
}

// wrappedHeight 返回这些行经 wrapLines 折行后实际占用的终端行数。
func wrappedHeight(width int, lines []string) int {
	block := wrapLines(width, lines)
	if block == "" {
		return 0
	}
	return lipgloss.Height(block)
}

func joinSections(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimRight(p, "\n")
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "\n\n")
}

func renderSplitPane(width int, left, right string) string {
	if width < 88 {
		return joinSections(left, right)
	}
	leftWidth := width / 2
	rightWidth := width - leftWidth - 2
	if leftWidth < 10 {
		leftWidth = 10
	}
	if rightWidth < 10 {
		rightWidth = 10
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).Render(left),
		lipgloss.NewStyle().Width(rightWidth).Render(right),
	)
}

func compactPath(path string, maxWidth int) string {
	path = strings.TrimSpace(path)
	if maxWidth < 4 {
		maxWidth = 4
	}
	if lipgloss.Width(path) <= maxWidth {
		return path
	}
	keep := maxWidth - 1
	if keep < 3 {
		keep = 3
	}
	runes := []rune(path)
	if len(runes) <= keep {
		return path
	}
	return "…" + string(runes[len(runes)-keep:])
}
