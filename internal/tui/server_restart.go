package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/internal/service"
	"github.com/shichao402/Dec/internal/serviceapi"
)

type serverVersionMsg struct {
	serverVersion string
	clientVersion string
	mismatch      bool
	err           error
}

type serverRestartDoneMsg struct {
	serverVersion string
	err           error
	reason        string
}

var restartServerOperation = func(projectRoot, reason string) (string, error) {
	api, err := serviceapi.Default()
	if err != nil {
		return "", err
	}
	if err := api.RestartServer(context.Background(), serviceapi.RestartOptions{
		Reason:      reason,
		ProjectRoot: projectRoot,
	}); err != nil {
		return "", err
	}
	return api.ServerVersion(), nil
}

var probeServerVersionOperation = func() (serverVersion string, clientVersion string, mismatch bool, err error) {
	api, err := serviceapi.Default()
	if err != nil {
		return "", "", false, err
	}
	return api.ServerVersion(), api.ClientVersion(), api.VersionMismatch(), nil
}

func VersionsMismatch(clientVersion, serverVersion string) bool {
	return service.VersionsMismatch(clientVersion, serverVersion)
}

func probeServerVersionCmd() tea.Cmd {
	return func() tea.Msg {
		server, client, mismatch, err := probeServerVersionOperation()
		return serverVersionMsg{
			serverVersion: server,
			clientVersion: client,
			mismatch:      mismatch,
			err:           err,
		}
	}
}

func restartServerCmd(projectRoot, reason string) tea.Cmd {
	return func() tea.Msg {
		ver, err := restartServerOperation(projectRoot, reason)
		return serverRestartDoneMsg{serverVersion: ver, err: err, reason: reason}
	}
}

func (m *model) beginServerRestartConfirm(reason string) {
	m.serverRestartStage = "confirm"
	m.serverRestartReason = reason
	m.serverRestartErr = nil
}

func (m *model) startServerRestart() tea.Cmd {
	if m.hasLocalWriteBusy() {
		m.serverRestartErr = fmt.Errorf("有进行中的 pull/push/更新，请等待完成后再重启")
		m.serverRestartStage = "done"
		m.pushLog("重启被拒绝: " + m.serverRestartErr.Error())
		return nil
	}
	m.serverRestartStage = "running"
	m.restartingServer = true
	m.serverRestartErr = nil
	reason := m.serverRestartReason
	if reason == "" {
		reason = "manual"
	}
	m.pushLog(fmt.Sprintf("正在重启 dec-server（%s）…", reason))
	return restartServerCmd(m.projectRoot, reason)
}

func (m model) hasLocalWriteBusy() bool {
	return m.runningPull || m.runningRemove || m.runningDelete || m.updatingBinary ||
		m.addSecretStage == addSecretStageRunning
}

func (m model) handleServerRestartKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.serverRestartStage {
	case "confirm":
		switch msg.String() {
		case "y", "enter":
			return m, m.startServerRestart()
		case "n", "esc":
			if m.serverRestartReason == "mismatch" {
				m.serverVersionMismatchDismissed = true
			}
			m.serverRestartStage = ""
			m.serverRestartReason = ""
			m.pushLog("已取消重启 dec-server")
			return m, nil
		}
	case "done":
		switch msg.String() {
		case "esc", "enter", " ", "q", "n":
			m.serverRestartStage = ""
			m.serverRestartReason = ""
			m.serverRestartErr = nil
			return m, nil
		}
	case "running":
		return m, nil
	}
	return m, nil
}

func (m model) renderServerRestartOverlay(width int) string {
	lines := []string{shellTitleStyle.Render("重启 dec-server")}
	switch m.serverRestartStage {
	case "confirm":
		if m.serverRestartReason == "mismatch" {
			lines = append(lines,
				shellWarnStyle.Render("客户端与服务版本不一致"),
				fmt.Sprintf("客户端: %s", fallbackValue(m.currentVersion, "未知")),
				fmt.Sprintf("服务端: %s", fallbackValue(m.serverVersion, "未知")),
			)
		} else {
			lines = append(lines, "手动重启本机 dec-server。")
		}
		lines = append(lines,
			"",
			shellWarnStyle.Render("将清除进程内 Bitwarden session，需重新解锁。"),
			"有进行中的 pull/push 时会拒绝重启。",
			"",
			shellMutedStyle.Render("y/Enter 重启 · n/Esc 取消"),
		)
	case "running":
		lines = append(lines, shellWarnStyle.Render("正在停止并重新拉起 dec-server…"))
	case "done":
		if m.serverRestartErr != nil {
			lines = append(lines, shellWarnStyle.Render("重启失败: "+m.serverRestartErr.Error()))
		} else {
			lines = append(lines,
				shellEnabledRow.Render("重启成功"),
				fmt.Sprintf("服务版本: %s", fallbackValue(m.serverVersion, "未知")),
			)
			if m.serverVersionMismatch {
				lines = append(lines, shellWarnStyle.Render("版本仍不一致；请确认已安装匹配的 dec / dec-server"))
			} else {
				lines = append(lines, "与客户端版本一致。")
			}
		}
		lines = append(lines, "", shellMutedStyle.Render("Enter/Esc 关闭"))
	}
	return wrapLines(width, lines)
}
