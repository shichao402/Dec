package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/serviceapi"
)

// 登记新 secret 的分阶段状态：先选归属，再输入相对路径；（Remote）再选内容来源。
const (
	addSecretStageTarget  = "target"
	addSecretStagePath    = "path"
	addSecretStageSource  = "source" // Remote only
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
		result, err := serviceapi.AddSecretToTarget(context.Background(), projectRoot, target, noteRel, reporter)
		return addSecretDoneMsg{result: result, err: err, logs: logs}
	}
}

func registerRemoteFromPathCmd(projectRoot, folder, noteRel, localPath string) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		result, err := serviceapi.RegisterRemoteNoteFromPath(context.Background(), projectRoot, folder, noteRel, localPath, reporter)
		return addSecretDoneMsg{result: result, err: err, logs: logs}
	}
}

func prepareRemoteRegisterCmd(projectRoot, folder, noteRel, editorCmd string) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		sess, err := serviceapi.PrepareRemoteNoteRegister(context.Background(), projectRoot, folder, noteRel, reporter)
		return remoteEditPreparedMsg{
			kind:      remoteEditKindNote,
			noteSess:  sess,
			editorCmd: editorCmd,
			err:       err,
			logs:      logs,
			register:  true,
		}
	}
}

func (m *model) beginAddSecret(remoteMode bool) {
	m.addSecretStage = addSecretStageTarget
	m.addSecretPathInput = ""
	m.addSecretContentPath = ""
	m.addSecretSourceMode = "temp"
	m.addSecretTargetIdx = 0
	m.addSecretResult = nil
	m.addSecretErr = nil
	m.addSecretRemoteMode = remoteMode
	m.remoteRegisterPending = false

	if remoteMode {
		targets, err := serviceapi.SuggestRemoteRegisterFolders(context.Background(), m.projectRoot)
		if err != nil {
			m.addSecretTargets = nil
			m.pushLog("读取远端 folder 失败: " + err.Error())
		} else {
			m.addSecretTargets = targets
		}
		m.pushLog("Remote 登记：选择任意远端 folder（不绑死 enabled SyncTarget）")
		return
	}

	targets, err := serviceapi.SuggestSecretTargets(m.projectRoot)
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
	m.addSecretContentPath = ""
	m.addSecretTargets = nil
	m.addSecretRemoteMode = false
	m.remoteRegisterPending = false
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
		if m.addSecretStage == addSecretStageSource && m.addSecretRemoteMode {
			if m.addSecretSourceMode == "temp" {
				m.addSecretSourceMode = "path"
			} else {
				m.addSecretSourceMode = "temp"
			}
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		if m.addSecretStage == addSecretStagePath {
			m.addSecretPathInput = trimLastRune(m.addSecretPathInput)
		}
		if m.addSecretStage == addSecretStageSource && m.addSecretSourceMode == "path" {
			m.addSecretContentPath = trimLastRune(m.addSecretContentPath)
		}
		return m, nil
	case tea.KeyEnter:
		return m.advanceAddSecret()
	}

	if len(msg.Runes) > 0 && !msg.Alt {
		switch m.addSecretStage {
		case addSecretStagePath:
			m.addSecretPathInput += string(msg.Runes)
		case addSecretStageSource:
			if m.addSecretSourceMode == "path" {
				m.addSecretContentPath += string(msg.Runes)
			}
		}
	}
	return m, nil
}

func (m model) advanceAddSecret() (tea.Model, tea.Cmd) {
	if m.addSecretStage == addSecretStageTarget {
		if len(m.addSecretTargets) == 0 {
			if m.addSecretRemoteMode {
				m.pushLog("没有可选的远端 folder（需 Bitwarden 已配置并解锁）")
			} else {
				m.pushLog("没有可选的 secrets 归属，请先在 Bundles 页启用 bundle")
			}
			return m, nil
		}
		m.addSecretStage = addSecretStagePath
		opt := m.addSecretTargets[m.addSecretTargetIdx]
		if m.addSecretRemoteMode {
			m.pushLog(fmt.Sprintf("输入 Note 名（相对 folder %s，如 env/x.env）", opt.Folder))
		} else {
			m.pushLog(fmt.Sprintf("输入相对 %s 的路径（如 env/vikunja.env）", opt.LocalRoot))
		}
		return m, nil
	}

	rel := strings.TrimSpace(m.addSecretPathInput)
	if rel == "" {
		m.pushLog("相对路径 / Note 名不能为空")
		return m, nil
	}
	if len(m.addSecretTargets) == 0 {
		m.pushLog("没有可选的 secrets 归属")
		return m, nil
	}
	opt := m.addSecretTargets[m.addSecretTargetIdx]

	if m.addSecretRemoteMode {
		if m.addSecretStage == addSecretStagePath {
			m.addSecretStage = addSecretStageSource
			m.addSecretSourceMode = "temp"
			m.pushLog("选择内容来源：temp 外部编辑器，或显式本地路径（tab 切换）")
			return m, nil
		}
		if m.addSecretStage == addSecretStageSource {
			if m.addSecretSourceMode == "path" {
				localPath := strings.TrimSpace(m.addSecretContentPath)
				if localPath == "" {
					m.pushLog("请输入本地文件路径，或 tab 切到 temp 编辑器")
					return m, nil
				}
				m.addSecretStage = addSecretStageRunning
				m.pushLog(fmt.Sprintf("登记 %s → %s（本地路径）", rel, opt.Folder))
				return m, registerRemoteFromPathCmd(m.projectRoot, opt.Folder, rel, localPath)
			}
			m.addSecretStage = addSecretStageRunning
			m.remoteRegisterPending = true
			m.pushLog(fmt.Sprintf("打开临时编辑器登记 %s → %s", rel, opt.Folder))
			return m, prepareRemoteRegisterCmd(m.projectRoot, opt.Folder, rel, m.effectiveEditor())
		}
	}

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
	title := "登记新 secret"
	if m.addSecretRemoteMode {
		title = "Remote · 登记 Secure Note"
	}
	lines := []string{shellTitleStyle.Render(title)}
	if m.addSecretRemoteMode {
		lines = append(lines, shellMutedStyle.Render("任意远端 folder；内容来自 temp 或本地路径；不种本地同步根。"))
	} else {
		lines = append(lines, shellMutedStyle.Render("Note 名 = 相对同步根路径；文件须先写到对应 .secrets/ 目录。"))
	}

	targetLine := "归属: <待选择>"
	if len(m.addSecretTargets) > 0 {
		opt := m.addSecretTargets[m.addSecretTargetIdx]
		targetLine = fmt.Sprintf("归属: %s", opt.Label)
	}
	pathLine := fmt.Sprintf("Note 路径: %s", fallbackValue(m.addSecretPathInput, "<待输入>"))
	sourceLine := "内容: 外部编辑器 (temp)"
	if m.addSecretSourceMode == "path" {
		sourceLine = fmt.Sprintf("内容: 本地路径 %s", fallbackValue(m.addSecretContentPath, "<待输入>"))
	}

	switch m.addSecretStage {
	case addSecretStageTarget:
		lines = append(lines, shellSelectedRow.Render(targetLine+"▌"), shellLogStyle.Render(pathLine))
		if len(m.addSecretTargets) > 0 {
			labels := make([]string, 0, len(m.addSecretTargets))
			for _, opt := range m.addSecretTargets {
				labels = append(labels, opt.Folder)
			}
			if len(labels) > 8 {
				labels = append(labels[:8], "…")
			}
			lines = append(lines, shellMutedStyle.Render("候选 folder: "+strings.Join(labels, " · ")+"（tab 轮转）"))
		}
		lines = append(lines, shellMutedStyle.Render("Enter 下一步 · Esc 取消"))
	case addSecretStagePath:
		lines = append(lines, shellLogStyle.Render(targetLine), shellSelectedRow.Render(pathLine+"▌"))
		lines = append(lines, shellMutedStyle.Render("Enter 下一步 · Esc 取消"))
	case addSecretStageSource:
		lines = append(lines, shellLogStyle.Render(targetLine), shellLogStyle.Render(pathLine))
		lines = append(lines, shellSelectedRow.Render(sourceLine+"▌"))
		lines = append(lines, shellMutedStyle.Render("tab 切换 temp/路径 · Enter 执行 · Esc 取消"))
	case addSecretStageRunning:
		lines = append(lines, shellLogStyle.Render(targetLine), shellLogStyle.Render(pathLine))
		if m.addSecretRemoteMode {
			lines = append(lines, shellLogStyle.Render(sourceLine))
		}
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
		extra := m.addSecretResult.ProjectRelPath
		if m.addSecretRemoteMode {
			extra = "不种本地同步根"
		}
		return shellMutedStyle.Render(fmt.Sprintf("已登记 %s → %s（%s）",
			m.addSecretResult.NoteRelPath, m.addSecretResult.Folder, extra))
	}
	return ""
}