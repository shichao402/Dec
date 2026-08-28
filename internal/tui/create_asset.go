package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/serviceapi"
	"github.com/shichao402/Dec/internal/types"
)

const (
	createStageKind = "kind"
	createStageName = "name"
)

type createLocalAssetDoneMsg struct {
	result *app.CreateLocalAssetResult
	err    error
}

func createLocalAssetCmd(workspace app.Workspace, in app.CreateLocalAssetInput) tea.Cmd {
	return func() tea.Msg {
		res, err := serviceapi.CreateLocalAsset(context.Background(), workspace, in, nil)
		return createLocalAssetDoneMsg{result: res, err: err}
	}
}

func (m model) beginCreateLocalAsset() (model, tea.Cmd) {
	if m.overview == nil || !m.overview.ProjectConfigReady {
		m.pushLog("请先初始化绑定项目")
		return m, nil
	}
	m.createStage = createStageKind
	m.createKindIdx = 0
	m.createName = ""
	m.createShared = false
	m.pushLog("新建资产：选择类型")
	return m, nil
}

func (m model) handleCreateLocalAssetKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	kinds := app.LocalAssetKinds()
	switch m.createStage {
	case createStageKind:
		switch msg.String() {
		case "esc":
			m.createStage = ""
			return m, nil
		case "j", "down":
			if m.createKindIdx+1 < len(kinds) {
				m.createKindIdx++
			}
			return m, nil
		case "k", "up":
			if m.createKindIdx > 0 {
				m.createKindIdx--
			}
			return m, nil
		case "enter":
			m.createStage = createStageName
			return m, nil
		}
	case createStageName:
		switch msg.String() {
		case "esc":
			m.createStage = createStageKind
			return m, nil
		case "ctrl+s":
			m.createShared = !m.createShared
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.createName)
			if name == "" {
				m.pushLog("名称不能为空")
				return m, nil
			}
			kind := kinds[m.createKindIdx]
			vis := types.AssetVisibilityPrivate
			if m.createShared {
				vis = types.AssetVisibilityPublic
			}
			plane := types.AssetPlaneLocal
			if m.plane == app.WorkspaceGlobal || m.plane == app.WorkspaceUser {
				plane = types.AssetPlaneGlobal
			}
			project := strings.TrimSpace(m.overview.HomeProject)
			if project == "" {
				project = strings.TrimSpace(m.overview.ProjectName)
			}
			in := app.CreateLocalAssetInput{
				Workspace:  m.workspace(),
				Project:    project,
				Kind:       kind.ID,
				Name:       name,
				Visibility: vis,
				Plane:      plane,
			}
			m.createStage = ""
			m.pushLog("写入本地模板…")
			return m, createLocalAssetCmd(m.workspace(), in)
		case "backspace", "ctrl+h":
			runes := []rune(m.createName)
			if len(runes) > 0 {
				m.createName = string(runes[:len(runes)-1])
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes {
				m.createName += string(msg.Runes)
			}
			return m, nil
		}
	}
	return m, nil
}

func (m model) renderCreateLocalAsset(width int) string {
	kinds := app.LocalAssetKinds()
	lines := []string{shellTitleStyle.Render("新建本地资产")}
	if m.createStage == createStageKind {
		lines = append(lines, shellMutedStyle.Render("选择类型 · Enter 下一步 · Esc 取消"))
		for i, k := range kinds {
			mark := "  "
			if i == m.createKindIdx {
				mark = "> "
			}
			line := mark + k.Label
			if i == m.createKindIdx {
				lines = append(lines, shellSelectedRow.Render(line))
			} else {
				lines = append(lines, line)
			}
		}
	} else {
		share := "关（私有）"
		if m.createShared {
			share = "开（共享）"
		}
		label := ""
		if m.createKindIdx >= 0 && m.createKindIdx < len(kinds) {
			label = kinds[m.createKindIdx].Label
		}
		lines = append(lines,
			fmt.Sprintf("类型: %s", label),
			fmt.Sprintf("名称: %s", m.createName),
			fmt.Sprintf("共享: %s  （Ctrl+S 切换）", share),
			shellMutedStyle.Render("Enter 写入模板 · Esc 返回类型"),
		)
	}
	return wrapLines(width, lines)
}
