package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/pkg/app"
)

// 登记新 secret 的分阶段状态。
const (
	addSecretStagePath    = "path"
	addSecretStageFolder  = "folder"
	addSecretStageRunning = "running"
)

type addSecretDoneMsg struct {
	result *app.AddSecretResult
	err    error
	// logs 是 app 层事件（含 web unlock 链接与失败原因）。
	// 登记是一次性动作，没有 Run 页那样的事件流，只能随结果一并回传。
	logs []string
}

func addSecretCmd(projectRoot, folder, relPath string) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		result, err := app.AddProjectSecret(context.Background(), projectRoot, folder, relPath, reporter)
		return addSecretDoneMsg{result: result, err: err, logs: logs}
	}
}

// beginAddSecret 打开「登记新 secret」流程。
//
// 只登记项目内已存在的文件：落地路径就是消费者读取的位置，先把文件放对位置再登记，
// 顺序反过来会先在 Bitwarden 里留下一条指向不存在路径的 Note。
func (m *model) beginAddSecret() {
	m.addSecretStage = addSecretStagePath
	m.addSecretPathInput = ""
	m.addSecretFolderInput = ""
	m.addSecretFolderIdx = 0
	m.addSecretResult = nil
	m.addSecretErr = nil

	folders, err := app.SuggestSecretFolders(m.projectRoot)
	if err != nil {
		m.addSecretFolders = nil
		m.pushLog("读取可选 Bitwarden folder 失败: " + err.Error())
	} else {
		m.addSecretFolders = folders
	}
	m.pushLog("登记新 secret：输入项目根相对落地路径")
}

func (m *model) cancelAddSecret() {
	m.addSecretStage = ""
	m.addSecretPathInput = ""
	m.addSecretFolderInput = ""
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
		if m.addSecretStage == addSecretStageFolder && len(m.addSecretFolders) > 0 {
			m.addSecretFolderIdx = (m.addSecretFolderIdx + 1) % len(m.addSecretFolders)
			m.addSecretFolderInput = m.addSecretFolders[m.addSecretFolderIdx]
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		if m.addSecretStage == addSecretStagePath {
			m.addSecretPathInput = trimLastRune(m.addSecretPathInput)
		} else {
			m.addSecretFolderInput = trimLastRune(m.addSecretFolderInput)
		}
		return m, nil
	case tea.KeyEnter:
		return m.advanceAddSecret()
	}

	if len(msg.Runes) > 0 && !msg.Alt {
		if m.addSecretStage == addSecretStagePath {
			m.addSecretPathInput += string(msg.Runes)
		} else {
			m.addSecretFolderInput += string(msg.Runes)
		}
	}
	return m, nil
}

func (m model) advanceAddSecret() (tea.Model, tea.Cmd) {
	if m.addSecretStage == addSecretStagePath {
		rel := strings.TrimSpace(m.addSecretPathInput)
		if rel == "" {
			m.pushLog("落地路径不能为空")
			return m, nil
		}
		m.addSecretStage = addSecretStageFolder
		if len(m.addSecretFolders) > 0 {
			m.addSecretFolderIdx = 0
			m.addSecretFolderInput = m.addSecretFolders[0]
		}
		m.pushLog("选择归属 Bitwarden folder（tab 轮转候选）")
		return m, nil
	}

	folder := strings.TrimSpace(m.addSecretFolderInput)
	if folder == "" {
		m.pushLog("Bitwarden folder 不能为空")
		return m, nil
	}
	rel := strings.TrimSpace(m.addSecretPathInput)
	m.addSecretStage = addSecretStageRunning
	m.pushLog(fmt.Sprintf("登记 %s → Bitwarden folder %q", rel, folder))
	return m, addSecretCmd(m.projectRoot, folder, rel)
}

func (m model) renderAddSecretBlock() string {
	lines := []string{shellTitleStyle.Render("登记新 secret")}
	lines = append(lines, shellMutedStyle.Render("Note 名就是项目根相对落地路径；folder 只用来分组。"))

	pathLine := fmt.Sprintf("落地路径: %s", fallbackValue(m.addSecretPathInput, "<待输入>"))
	folderLine := fmt.Sprintf("Bitwarden folder: %s", fallbackValue(m.addSecretFolderInput, "<待输入>"))
	switch m.addSecretStage {
	case addSecretStagePath:
		lines = append(lines, shellSelectedRow.Render(pathLine+"▌"), shellLogStyle.Render(folderLine))
		lines = append(lines, shellMutedStyle.Render("例：.config/mise/conf.d/tencent.toml · Enter 下一步 · Esc 取消"))
	case addSecretStageFolder:
		lines = append(lines, shellLogStyle.Render(pathLine), shellSelectedRow.Render(folderLine+"▌"))
		if len(m.addSecretFolders) > 0 {
			lines = append(lines, shellMutedStyle.Render("候选: "+strings.Join(m.addSecretFolders, " · ")+"（tab 轮转）"))
		}
		lines = append(lines, shellMutedStyle.Render("Enter 登记 · Esc 取消"))
	case addSecretStageRunning:
		lines = append(lines, shellLogStyle.Render(pathLine), shellLogStyle.Render(folderLine))
		lines = append(lines, shellWarnStyle.Render("正在写入 Bitwarden..."))
	}
	return strings.Join(lines, "\n")
}

// renderAddSecretOutcome 渲染上一次登记结果；流程进行中不显示，避免与输入区抢注意力。
func (m model) renderAddSecretOutcome() string {
	if m.addSecretStage != "" {
		return ""
	}
	if m.addSecretErr != nil {
		return shellWarnStyle.Render("登记 secret 失败: " + m.addSecretErr.Error())
	}
	if m.addSecretResult != nil {
		return shellMutedStyle.Render(fmt.Sprintf("已登记 %s → Bitwarden folder %q",
			m.addSecretResult.LandingPath, m.addSecretResult.Folder))
	}
	return ""
}
