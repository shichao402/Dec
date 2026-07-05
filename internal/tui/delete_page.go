package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/pkg/app"
)

type deleteLoadedMsg struct {
	candidates []app.DeleteCandidate
	err        error
	logs       []string
}

type deleteEventMsg struct {
	event app.OperationEvent
}

type deleteCompletedMsg struct {
	result *app.DeleteProjectResult
	err    error
}

var listDeleteCandidatesOperation = func(ctx context.Context, projectRoot string, reporter app.Reporter) ([]app.DeleteCandidate, error) {
	return app.ListDeleteCandidates(ctx, projectRoot, reporter)
}

var runDeleteOperation = func(ctx context.Context, input app.DeleteProjectInput, reporter app.Reporter) (*app.DeleteProjectResult, error) {
	return app.DeleteProjectItems(ctx, input, reporter)
}

func loadDeleteCandidatesCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		candidates, err := listDeleteCandidatesOperation(context.Background(), projectRoot, reporter)
		return deleteLoadedMsg{candidates: candidates, err: err, logs: logs}
	}
}

func (m model) isDeletePage() bool {
	return m.pages[m.pageIndex] == "Delete"
}

func (m model) deleteNavigableRows() []deleteDisplayRow {
	rows := m.deleteDisplayRows()
	if len(rows) == 0 {
		return nil
	}
	out := make([]deleteDisplayRow, 0, len(rows))
	for _, row := range rows {
		if !row.header {
			out = append(out, row)
		}
	}
	return out
}

func (m *model) normalizeDeleteSelection() {
	navigable := m.deleteNavigableRows()
	visible := m.visibleDeleteCandidates()
	if len(m.deleteSelected) != len(visible) {
		m.deleteSelected = make([]bool, len(visible))
	}
	if len(navigable) == 0 {
		m.deleteCursor = 0
		return
	}
	if m.deleteCursor >= len(navigable) {
		m.deleteCursor = 0
	}
	if m.deleteCursor < 0 {
		m.deleteCursor = 0
	}
}

func (m model) routeDeletePageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.isDeletePage() {
		return m, nil, false
	}
	if m.focus == focusSidebar && msg.String() != "r" {
		return m, nil, false
	}
	if m.deleteFilterInput {
		model, cmd := m.handleDeleteFilterInput(msg)
		return model, cmd, true
	}
	if m.deleteStage == "summary" || m.deleteStage == "confirm" {
		model, cmd := m.handleDeleteStageKey(msg)
		return model, cmd, true
	}
	if m.runningDelete {
		if msg.String() == "esc" {
			if m.runCancel != nil {
				m.runCancel()
				m.pushLog("Delete cancel requested")
			}
			return m, nil, true
		}
		return m, nil, true
	}
	if m.focus == focusSidebar && msg.String() == "r" {
		m.deleteCandidatesLoaded = false
		m.loadingDeleteCandidates = true
		return m, loadDeleteCandidatesCmd(m.projectRoot), true
	}
	if m.focus != focusSidebar {
		model, cmd := m.handleDeletePageKey(msg)
		return model, cmd, true
	}
	return m, nil, false
}

func (m model) cmdOnPageSwitch() tea.Cmd {
	if m.isDeletePage() {
		return loadDeleteCandidatesCmd(m.projectRoot)
	}
	return nil
}

type deleteDisplayRow struct {
	header         bool
	label          string
	candidateIndex int
}

func (m model) deleteDisplayRows() []deleteDisplayRow {
	visible := m.visibleDeleteCandidates()
	if len(visible) == 0 {
		return nil
	}

	type groupMeta struct {
		order   int
		title   string
		indices []int
	}
	grouped := make(map[string]*groupMeta)
	groupOrder := make([]string, 0)

	for i, c := range visible {
		key := strings.TrimSpace(c.GroupBundle)
		if key == "" {
			key = deleteFallbackGroupKey(c)
		}
		meta, ok := grouped[key]
		if !ok {
			title := strings.TrimSpace(c.GroupTitle)
			if title == "" {
				title = deleteBundleGroupTitle(key)
			}
			meta = &groupMeta{order: c.GroupOrder, title: title}
			grouped[key] = meta
			groupOrder = append(groupOrder, key)
		}
		meta.indices = append(meta.indices, i)
	}

	sort.Slice(groupOrder, func(i, j int) bool {
		gi, gj := grouped[groupOrder[i]], grouped[groupOrder[j]]
		if gi.order != gj.order {
			return gi.order < gj.order
		}
		return groupOrder[i] < groupOrder[j]
	})

	rows := make([]deleteDisplayRow, 0, len(visible)+len(groupOrder))
	for _, key := range groupOrder {
		meta := grouped[key]
		rows = append(rows, deleteDisplayRow{header: true, label: meta.title, candidateIndex: -1})
		for _, idx := range meta.indices {
			rows = append(rows, deleteDisplayRow{
				label:          deleteChildLabel(visible[idx]),
				candidateIndex: idx,
			})
		}
	}
	return rows
}

func deleteFallbackGroupKey(c app.DeleteCandidate) string {
	switch c.Kind {
	case app.DeleteKindBundle:
		return c.BundleName
	case app.DeleteKindSecret:
		if bundle := strings.TrimSpace(c.SecretsBundle); bundle != "" {
			return bundle
		}
	default:
		if vault := strings.TrimSpace(c.Vault); vault != "" {
			return vault
		}
	}
	return "other"
}

func deleteBundleGroupTitle(groupBundle string) string {
	if groupBundle == "_project" {
		return "? (project)"
	}
	return fmt.Sprintf("%s (bundle)", groupBundle)
}

func deleteChildLabel(c app.DeleteCandidate) string {
	switch c.Kind {
	case app.DeleteKindBundle:
		return "   ↳ [bundle] " + strings.TrimPrefix(c.Label, "[bundle] ")
	case app.DeleteKindSecret:
		return "   ↳ [secret] " + strings.TrimPrefix(c.Label, "[secret] ")
	default:
		return "   ↳ " + c.Label
	}
}

func (m model) visibleDeleteCandidates() []app.DeleteCandidate {
	filter := strings.ToLower(strings.TrimSpace(m.deleteFilter))
	if filter == "" {
		return m.deleteCandidates
	}
	out := make([]app.DeleteCandidate, 0, len(m.deleteCandidates))
	for _, c := range m.deleteCandidates {
		if strings.Contains(strings.ToLower(c.Label), filter) {
			out = append(out, c)
		}
	}
	return out
}

func (m model) selectedDeleteItems() []app.DeleteSelectionItem {
	visible := m.visibleDeleteCandidates()
	items := make([]app.DeleteSelectionItem, 0)
	for i, candidate := range visible {
		if i >= len(m.deleteSelected) || !m.deleteSelected[i] {
			continue
		}
		items = append(items, selectionFromCandidate(candidate))
	}
	return items
}

func selectionFromCandidate(c app.DeleteCandidate) app.DeleteSelectionItem {
	return app.DeleteSelectionItem{
		Kind:            c.Kind,
		Type:            c.Type,
		Name:            c.Name,
		Vault:           c.Vault,
		SecretPath:      c.SecretPath,
		SecretsBundle:   c.SecretsBundle,
		RelWithinBundle: c.RelWithinBundle,
		BundleName:      c.BundleName,
		Members:         append([]app.AssetSelectionItem(nil), c.Members...),
	}
}

func (m model) handleDeletePageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.deleteFilterInput {
		return m.handleDeleteFilterInput(msg)
	}
	if m.deleteStage == "summary" || m.deleteStage == "confirm" {
		return m.handleDeleteStageKey(msg)
	}
	if m.runningDelete {
		return m, nil
	}

	switch msg.String() {
	case "esc", "h", "left":
		m.focus = focusSidebar
		m.pushLog("返回导航")
		return m, nil
	case "/":
		m.deleteFilterInput = true
		return m, nil
	case "c":
		if strings.TrimSpace(m.deleteFilter) != "" {
			m.deleteFilter = ""
			m.normalizeDeleteSelection()
			m.pushLog("Delete 筛选已清空")
		}
		return m, nil
	case "a":
		visible := m.visibleDeleteCandidates()
		m.deleteSelected = make([]bool, len(visible))
		for i := range m.deleteSelected {
			m.deleteSelected[i] = true
		}
		m.pushLog(fmt.Sprintf("Delete 已全选 %d 项", len(visible)))
		return m, nil
	case "j", "down":
		navigable := m.deleteNavigableRows()
		if len(navigable) == 0 {
			return m, nil
		}
		m.deleteCursor++
		if m.deleteCursor >= len(navigable) {
			m.deleteCursor = len(navigable) - 1
		}
		return m, nil
	case "k", "up":
		if len(m.deleteNavigableRows()) == 0 {
			return m, nil
		}
		m.deleteCursor--
		if m.deleteCursor < 0 {
			m.deleteCursor = 0
		}
		return m, nil
	case " ", "enter":
		navigable := m.deleteNavigableRows()
		if len(navigable) == 0 || m.deleteCursor < 0 || m.deleteCursor >= len(navigable) {
			return m, nil
		}
		idx := navigable[m.deleteCursor].candidateIndex
		if idx < 0 || idx >= len(m.visibleDeleteCandidates()) {
			return m, nil
		}
		if len(m.deleteSelected) != len(m.visibleDeleteCandidates()) {
			m.normalizeDeleteSelection()
		}
		m.deleteSelected[idx] = !m.deleteSelected[idx]
		return m, nil
	case "d":
		selected := m.selectedDeleteItems()
		if len(selected) == 0 {
			m.pushLog("Delete 请先 space 选择要删除的项")
			return m, nil
		}
		m.deleteStage = "summary"
		m.pushLog(fmt.Sprintf("Delete 摘要确认：%d 项", len(selected)))
		return m, nil
	case "r":
		m.deleteCandidatesLoaded = false
		m.loadingDeleteCandidates = true
		return m, loadDeleteCandidatesCmd(m.projectRoot)
	}
	return m, nil
}

func (m model) handleDeleteStageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.deleteStage {
	case "summary":
		switch msg.String() {
		case "y", "enter":
			m.deleteStage = "confirm"
			m.pushLog("Delete 进入最终确认")
			return m, nil
		case "n", "esc", "h", "left":
			m.deleteStage = ""
			m.pushLog("Delete 已取消")
			return m, nil
		}
	case "confirm":
		switch msg.String() {
		case "y":
			m.deleteStage = "running"
			return m, m.startDeleteRun()
		case "n", "esc", "h", "left":
			m.deleteStage = "summary"
			m.pushLog("Delete 返回摘要")
			return m, nil
		}
	}
	return m, nil
}

func (m model) handleDeleteFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.deleteFilterInput = false
		return m, nil
	case tea.KeyEnter:
		m.deleteFilterInput = false
		m.normalizeDeleteSelection()
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.deleteFilter = trimLastRune(m.deleteFilter)
		m.normalizeDeleteSelection()
		return m, nil
	}
	if len(msg.Runes) > 0 && !msg.Alt {
		m.deleteFilter += string(msg.Runes)
		m.normalizeDeleteSelection()
	}
	return m, nil
}

func (m *model) startDeleteRun() tea.Cmd {
	selected := m.selectedDeleteItems()
	if len(selected) == 0 {
		return nil
	}
	stream := make(chan tea.Msg, 64)
	ctx, cancel := context.WithCancel(context.Background())
	m.runningDelete = true
	m.runMode = "delete"
	m.runProgress = nil
	m.runEvents = nil
	m.runPinLine = ""
	m.deleteResult = nil
	m.deleteErr = nil
	m.runStream = stream
	m.runCtx = ctx
	m.runCancel = cancel
	input := app.DeleteProjectInput{
		ProjectRoot: m.projectRoot,
		Items:       selected,
		Confirmed:   true,
	}
	m.pushLog(fmt.Sprintf("Delete page started: %d items", len(selected)))
	return tea.Batch(startDeleteRunCmd(ctx, input, stream), waitRunMsg(stream))
}

func startDeleteRunCmd(ctx context.Context, input app.DeleteProjectInput, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			result, err := runDeleteOperation(ctx, input, app.ReporterFunc(func(event app.OperationEvent) {
				stream <- deleteEventMsg{event: event}
			}))
			stream <- deleteCompletedMsg{result: result, err: err}
			close(stream)
		}()
		return nil
	}
}

func (m model) renderDeletePage(width int) string {
	if m.deleteStage == "summary" || m.deleteStage == "confirm" {
		return m.renderDeleteConfirmPage(width)
	}
	if m.loadingDeleteCandidates && len(m.deleteCandidates) == 0 {
		return shellMutedStyle.Render("加载可删除项…")
	}
	if m.deleteLoadErr != nil {
		return shellWarnStyle.Render("加载失败: "+m.deleteLoadErr.Error()) + "\n" + shellMutedStyle.Render("按 r 重试")
	}

	visible := m.visibleDeleteCandidates()
	displayRows := m.deleteDisplayRows()
	selectedCount := len(m.selectedDeleteItems())
	lines := []string{
		shellTitleStyle.Render("Delete"),
		shellMutedStyle.Render("Dec 资产 · secrets 文件 · bundle · 两次确认后执行"),
		fmt.Sprintf("共 %d 项 · 已选 %d · 筛选 %q", len(visible), selectedCount, m.currentDeleteFilterLabel()),
	}
	if m.deleteFilterInput {
		lines = append(lines, shellMutedStyle.Render("筛选输入中：Enter 应用 · Esc 退出"))
	} else {
		lines = append(lines, shellMutedStyle.Render("j/k 移动 · space 选择 · d 删除 · a 全选 · / 筛选 · r 刷新 · h/Esc 返回导航"))
	}
	if len(visible) == 0 {
		lines = append(lines, shellWarnStyle.Render("没有可删除项。"))
		return wrapLines(width, lines)
	}
	selected := m.deleteSelected
	if len(selected) != len(visible) {
		selected = make([]bool, len(visible))
	}
	navCursor := 0
	for _, row := range displayRows {
		if row.header {
			lines = append(lines, shellTitleStyle.Render("▾ "+row.label))
			continue
		}
		marker := " "
		if navCursor == m.deleteCursor {
			marker = ">"
		}
		check := "[ ]"
		idx := row.candidateIndex
		if idx >= 0 && idx < len(selected) && selected[idx] {
			check = "[x]"
		}
		line := fmt.Sprintf(" %s %s %s", marker, check, row.label)
		if navCursor == m.deleteCursor {
			lines = append(lines, shellSelectedRow.Render(line))
		} else {
			lines = append(lines, shellLogStyle.Render(line))
		}
		navCursor++
	}
	if m.runningDelete {
		lines = append(lines, "", shellGoodStyle.Render("删除执行中…"), shellMutedStyle.Render("Esc 取消删除"))
	}
	if m.deleteErr != nil {
		lines = append(lines, shellWarnStyle.Render("删除失败: "+m.deleteErr.Error()))
	}
	if m.deleteResult != nil {
		lines = append(lines, shellGoodStyle.Render(fmt.Sprintf("上次删除：Dec %d · secrets %d · bundle %d",
			m.deleteResult.DecDeleted, m.deleteResult.SecretsDeleted, m.deleteResult.BundlesDeleted)))
	}
	return wrapLines(width, lines)
}

func (m model) renderDeleteConfirmPage(width int) string {
	selected := m.selectedDeleteItems()
	lines := []string{shellTitleStyle.Render("Delete · 确认")}
	switch m.deleteStage {
	case "summary":
		lines = append(lines, shellMutedStyle.Render("操作  y/Enter 继续 · n/Esc/h 取消"), "")
		lines = append(lines, fmt.Sprintf("将删除 %d 项：", len(selected)))
		for _, item := range selected {
			switch item.Kind {
			case app.DeleteKindBundle:
				lines = append(lines, fmt.Sprintf("  bundle %s", item.BundleName))
			case app.DeleteKindSecret:
				lines = append(lines, fmt.Sprintf("  secret %s", item.SecretPath))
			default:
				lines = append(lines, fmt.Sprintf("  [%s] %s / %s", item.Type, item.Name, item.Vault))
			}
		}
	case "confirm":
		lines = append(lines,
			shellMutedStyle.Render("操作  y 确认删除 · n/Esc/h 返回摘要"),
			"",
			shellWarnStyle.Render("⚠️  删除不可逆：远端 vault、本地 cache/IDE、Bitwarden Secure Note 将被移除。"),
			fmt.Sprintf("共 %d 项待删除。", len(selected)),
		)
	}
	return wrapLines(width, lines)
}

func (m model) currentDeleteFilterLabel() string {
	filter := strings.TrimSpace(m.deleteFilter)
	if filter == "" {
		return "<none>"
	}
	return filter
}
