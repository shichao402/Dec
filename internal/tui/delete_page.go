package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/pkg/app"
)

type deleteLoadedMsg struct {
	candidates    []app.DeleteCandidate
	err           error
	logs          []string
	loadGen       uint64
	includeRemote bool
}

type deleteEventMsg struct {
	event app.OperationEvent
}

type deleteCompletedMsg struct {
	result *app.DeleteProjectResult
	err    error
}

var listDeleteCandidatesOperation = func(ctx context.Context, projectRoot string, includeRemote bool, reporter app.Reporter) ([]app.DeleteCandidate, error) {
	return app.ListDeleteCandidates(ctx, projectRoot, includeRemote, reporter)
}

var runDeleteOperation = func(ctx context.Context, input app.DeleteProjectInput, reporter app.Reporter) (*app.DeleteProjectResult, error) {
	return app.DeleteProjectItems(ctx, input, reporter)
}

func loadDeleteCandidatesCmd(ctx context.Context, projectRoot string, includeRemote bool, loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		candidates, err := listDeleteCandidatesOperation(ctx, projectRoot, includeRemote, reporter)
		return deleteLoadedMsg{candidates: candidates, err: err, logs: logs, loadGen: loadGen, includeRemote: includeRemote}
	}
}

func (m model) isDeletePage() bool {
	return m.pages[m.pageIndex] == "Delete"
}

func (m *model) cancelDeleteCandidatesLoad() {
	if m.deleteLoadCancel != nil {
		m.deleteLoadCancel()
		m.deleteLoadCancel = nil
	}
}

func (m *model) startDeleteCandidatesLoad(includeRemote, force bool) tea.Cmd {
	if !force {
		if m.loadingDeleteCandidates {
			return nil
		}
		if m.deleteCandidatesLoaded && (!includeRemote || m.deleteIncludeRemote) {
			return nil
		}
	}
	m.cancelDeleteCandidatesLoad()
	m.deleteLoadGen++
	gen := m.deleteLoadGen
	ctx, cancel := context.WithCancel(context.Background())
	m.deleteLoadCancel = cancel
	m.loadingDeleteCandidates = true
	return loadDeleteCandidatesCmd(ctx, m.projectRoot, includeRemote, gen)
}

func (m *model) onPageChanged(fromPage string) tea.Cmd {
	if fromPage == "Delete" {
		m.cancelDeleteCandidatesLoad()
		m.loadingDeleteCandidates = false
	}
	if m.isDeletePage() {
		return m.startDeleteCandidatesLoad(false, false)
	}
	return nil
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
		return m, m.startDeleteCandidatesLoad(true, true), true
	}
	if m.focus != focusSidebar {
		model, cmd := m.handleDeletePageKey(msg)
		return model, cmd, true
	}
	return m, nil, false
}

func (m model) visibleDeleteCandidates() []app.DeleteCandidate {
	filter := strings.ToLower(strings.TrimSpace(m.deleteFilter))
	if filter == "" {
		return m.deleteCandidates
	}
	out := make([]app.DeleteCandidate, 0, len(m.deleteCandidates))
	for _, c := range m.deleteCandidates {
		haystack := strings.ToLower(strings.Join([]string{
			c.Label, c.TreeRoot, c.TreeBranch, c.GroupTitle, c.SecretPath, c.SSHKeyName, c.Vault, c.Name,
		}, " "))
		if strings.Contains(haystack, filter) {
			out = append(out, c)
		}
	}
	return out
}

func selectionFromCandidate(c app.DeleteCandidate) app.DeleteSelectionItem {
	return app.DeleteSelectionItem{
		Kind:          c.Kind,
		Type:          c.Type,
		Name:          c.Name,
		Vault:         c.Vault,
		SecretPath:    c.SecretPath,
		SecretsBundle: c.SecretsBundle,
		SSHKeyName:    c.SSHKeyName,
		DecBundleName: c.DecBundleName,
		BundleName:    c.BundleName,
		Members:       append([]app.AssetSelectionItem(nil), c.Members...),
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
		if m.deleteTree.CollapseAtCursor() {
			m.pushLog("Delete 折叠目录")
			return m, nil
		}
		m.focus = focusSidebar
		m.pushLog("返回导航")
		return m, nil
	case "l", "right":
		if m.deleteTree.CursorOnExpandable() && !m.deleteTree.CursorExpanded() {
			m.deleteTree.ExpandAtCursor()
			m.pushLog("Delete 展开目录")
			return m, nil
		}
		return m, nil
	case "enter":
		if m.deleteTree.CursorOnExpandable() {
			if m.deleteTree.CursorExpanded() {
				m.deleteTree.CollapseAtCursor()
				m.pushLog("Delete 折叠目录")
			} else {
				m.deleteTree.ExpandAtCursor()
				m.pushLog("Delete 展开目录")
			}
			return m, nil
		}
		m.deleteTree.ToggleSelectAtCursor()
		return m, nil
	case "/":
		m.deleteFilterInput = true
		return m, nil
	case "c":
		if strings.TrimSpace(m.deleteFilter) != "" {
			m.deleteFilter = ""
			m.rebuildDeleteTree()
			m.pushLog("Delete 筛选已清空")
		}
		return m, nil
	case "a":
		m.deleteTree.SelectAllAtCursor()
		m.pushLog(fmt.Sprintf("Delete 已全选 %d 项", m.deleteTree.CountSelectable()))
		return m, nil
	case "j", "down":
		m.deleteTree.MoveCursor(1)
		return m, nil
	case "k", "up":
		m.deleteTree.MoveCursor(-1)
		return m, nil
	case " ":
		m.deleteTree.ToggleSelectAtCursor()
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
		return m, m.startDeleteCandidatesLoad(true, true)
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
		m.rebuildDeleteTree()
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.deleteFilter = trimLastRune(m.deleteFilter)
		m.rebuildDeleteTree()
		return m, nil
	}
	if len(msg.Runes) > 0 && !msg.Alt {
		m.deleteFilter += string(msg.Runes)
		m.rebuildDeleteTree()
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
	mm := m
	if len(mm.deleteCandidates) > 0 && len(mm.deleteTree.Roots) == 0 {
		mm.rebuildDeleteTree()
	}
	tree := mm.deleteTree
	if m.loadingDeleteCandidates && len(m.deleteCandidates) == 0 {
		return shellMutedStyle.Render("加载可删除项…")
	}
	if m.deleteLoadErr != nil {
		return shellWarnStyle.Render("加载失败: "+m.deleteLoadErr.Error()) + "\n" + shellMutedStyle.Render("按 r 重试")
	}

	visible := mm.visibleDeleteCandidates()
	rows := tree.VisibleRows()
	selectedCount := tree.CountSelected()
	lines := []string{
		shellTitleStyle.Render("Delete"),
		shellMutedStyle.Render("Dec 资产 · secrets · SSH Key · bundle · 两次确认后执行"),
		fmt.Sprintf("共 %d 项 · 已选 %d · 筛选 %q", len(visible), selectedCount, mm.currentDeleteFilterLabel()),
	}
	if !mm.deleteIncludeRemote {
		lines = append(lines, shellMutedStyle.Render("未扫描 Bitwarden 远端 orphan · 按 r 刷新（含远端）"))
	}
	if mm.deleteFilterInput {
		lines = append(lines, shellMutedStyle.Render("筛选输入中：Enter 应用 · Esc 退出"))
	} else {
		lines = append(lines, shellMutedStyle.Render("j/k 移动 · l/h 展开折叠 · space 选择 · d 删除 · a 全选 · / 筛选 · r 刷新 · Esc 返回导航"))
	}
	if len(visible) == 0 {
		lines = append(lines, shellWarnStyle.Render("没有可删除项。"))
		return wrapLines(width, lines)
	}
	for i, row := range rows {
		line := renderDeleteTreeLine(row, i, &tree, mm.focus != focusSidebar && i == tree.Cursor)
		if mm.focus != focusSidebar && i == tree.Cursor {
			lines = append(lines, shellSelectedRow.Render(line))
		} else {
			lines = append(lines, shellLogStyle.Render(line))
		}
	}
	if m.runningDelete {
		lines = append(lines, "", shellWarnStyle.Render("正在删除… Esc 取消"))
	}
	if m.deleteErr != nil {
		lines = append(lines, shellWarnStyle.Render("删除失败: "+m.deleteErr.Error()))
	}
	if m.deleteResult != nil {
		lines = append(lines, shellGoodStyle.Render(fmt.Sprintf("上次删除：Dec %d · secrets %d · ssh %d · bundle %d",
			m.deleteResult.DecDeleted, m.deleteResult.SecretsDeleted, m.deleteResult.SSHKeysDeleted, m.deleteResult.BundlesDeleted)))
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
			case app.DeleteKindSSHKey:
				lines = append(lines, fmt.Sprintf("  ssh %s", item.SSHKeyName))
			default:
				lines = append(lines, fmt.Sprintf("  [%s] %s / %s", item.Type, item.Name, item.Vault))
			}
		}
	case "confirm":
		lines = append(lines,
			shellMutedStyle.Render("操作  y 确认删除 · n/Esc/h 返回摘要"),
			"",
			shellWarnStyle.Render("⚠️  删除不可逆：远端 vault、本地 cache/IDE、Bitwarden Note/SSH Key 将被移除。"),
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
