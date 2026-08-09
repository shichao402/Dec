package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/pkg/app"
	"github.com/shichao402/Dec/pkg/secrets"
)

// 登记新 secret 的分阶段状态：先选 SyncTarget 归属，再输入相对同步根路径。
const (
	addSecretStageTarget  = "target"
	addSecretStagePath    = "path"
	addSecretStageRunning = "running"
)

type addSecretDoneMsg struct {
	result *app.AddSecretResult
	err    error
	// logs 是 app 层事件（含 web unlock 链接与失败原因）。
	logs []string
}

func addSecretCmd(projectRoot string, target secrets.SyncTarget, noteRel string) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		result, err := app.AddSecretToTarget(context.Background(), projectRoot, target, noteRel, reporter)
		return addSecretDoneMsg{result: result, err: err, logs: logs}
	}
}

func (m *model) beginAddSecret() {
	m.addSecretStage = addSecretStageTarget
	m.addSecretPathInput = ""
	m.addSecretTargetIdx = 0
	m.addSecretResult = nil
	m.addSecretErr = nil

	targets, err := app.SuggestSecretTargets(m.projectRoot)
	if err != nil {
		m.addSecretTargets = nil
		m.pushLog("读取可选 secrets 归属失败: " + err.Error())
	} else {
		m.addSecretTargets = targets
	}
	m.pushLog("登记新 secret：选择归属（project 或已启用 bundle）")
}

func (m *model) cancelAddSecret() {
	m.addSecretStage = ""
	m.addSecretPathInput = ""
	m.addSecretTargets = nil
	m.pushLog("已取消登记 secret")
}

func (m model) handleAddSecretKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.addSecretStage == addSecretStageRunning {
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.cancelAddSecret()
		return m, nil
	case tea.KeyTab:
		if m.addSecretStage == addSecretStageTarget && len(m.addSecretTargets) > 0 {
			m.addSecretTargetIdx = (m.addSecretTargetIdx + 1) % len(m.addSecretTargets)
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		if m.addSecretStage == addSecretStagePath {
			m.addSecretPathInput = trimLastRune(m.addSecretPathInput)
		}
		return m, nil
	case tea.KeyEnter:
		return m.advanceAddSecret()
	}

	if len(msg.Runes) > 0 && !msg.Alt && m.addSecretStage == addSecretStagePath {
		m.addSecretPathInput += string(msg.Runes)
	}
	return m, nil
}

func (m model) advanceAddSecret() (tea.Model, tea.Cmd) {
	if m.addSecretStage == addSecretStageTarget {
		if len(m.addSecretTargets) == 0 {
			m.pushLog("没有可选的 secrets 归属，请先在 Bundles 页启用 bundle")
			return m, nil
		}
		m.addSecretStage = addSecretStagePath
		opt := m.addSecretTargets[m.addSecretTargetIdx]
		m.pushLog(fmt.Sprintf("输入相对 %s 的路径（如 env/vikunja.env）", opt.LocalRoot))
		return m, nil
	}

	rel := strings.TrimSpace(m.addSecretPathInput)
	if rel == "" {
		m.pushLog("相对同步根路径不能为空")
		return m, nil
	}
	if len(m.addSecretTargets) == 0 {
		m.pushLog("没有可选的 secrets 归属")
		return m, nil
	}
	opt := m.addSecretTargets[m.addSecretTargetIdx]
	target := secrets.SyncTarget{
		Kind:      opt.Kind,
		Name:      opt.Name,
		Folder:    opt.Folder,
		LocalRoot: opt.LocalRoot,
	}
	m.addSecretStage = addSecretStageRunning
	m.pushLog(fmt.Sprintf("登记 %s → %s", rel, opt.Label))
	return m, addSecretCmd(m.projectRoot, target, rel)
}

func (m model) renderAddSecretBlock() string {
	lines := []string{shellTitleStyle.Render("登记新 secret")}
	lines = append(lines, shellMutedStyle.Render("Note 名 = 相对同步根路径；文件须先写到对应 .secrets/ 目录。"))

	targetLine := "归属: <待选择>"
	if len(m.addSecretTargets) > 0 {
		opt := m.addSecretTargets[m.addSecretTargetIdx]
		targetLine = fmt.Sprintf("归属: %s", opt.Label)
	}
	pathLine := fmt.Sprintf("相对路径: %s", fallbackValue(m.addSecretPathInput, "<待输入>"))

	switch m.addSecretStage {
	case addSecretStageTarget:
		lines = append(lines, shellSelectedRow.Render(targetLine+"▌"), shellLogStyle.Render(pathLine))
		if len(m.addSecretTargets) > 0 {
			labels := make([]string, 0, len(m.addSecretTargets))
			for _, opt := range m.addSecretTargets {
				labels = append(labels, opt.Label)
			}
			lines = append(lines, shellMutedStyle.Render("候选: "+strings.Join(labels, " · ")+"（tab 轮转）"))
		}
		lines = append(lines, shellMutedStyle.Render("例：env/vikunja.env · Enter 下一步 · Esc 取消"))
	case addSecretStagePath:
		lines = append(lines, shellLogStyle.Render(targetLine), shellSelectedRow.Render(pathLine+"▌"))
		lines = append(lines, shellMutedStyle.Render("Enter 登记 · Esc 取消"))
	case addSecretStageRunning:
		lines = append(lines, shellLogStyle.Render(targetLine), shellLogStyle.Render(pathLine))
		lines = append(lines, shellWarnStyle.Render("正在写入 Bitwarden..."))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderAddSecretOutcome() string {
	if m.addSecretStage != "" {
		return ""
	}
	if m.addSecretErr != nil {
		return shellWarnStyle.Render("登记 secret 失败: " + m.addSecretErr.Error())
	}
	if m.addSecretResult != nil {
		return shellMutedStyle.Render(fmt.Sprintf("已登记 %s → %s（%s）",
			m.addSecretResult.NoteRelPath, m.addSecretResult.Folder, m.addSecretResult.ProjectRelPath))
	}
	return ""
}
