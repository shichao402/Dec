package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/editor"
	"github.com/shichao402/Dec/internal/serviceapi"
)

type remoteEditKind string

const (
	remoteEditKindNote remoteEditKind = "note"
	remoteEditKindSSH  remoteEditKind = "ssh"
)

type remoteEditPreparedMsg struct {
	kind      remoteEditKind
	noteSess  *app.RemoteNoteEditSession
	sshSess   *app.RemoteSSHHostsEditSession
	editorCmd string
	err       error
	logs      []string
	register  bool // true = n 登记新建，而非编辑已有
}

type remoteEditDoneMsg struct {
	kind remoteEditKind
	err  error
	logs []string
}

func (m model) cursorRemoteCandidate() (app.DeleteCandidate, bool) {
	rows := m.deleteTree.VisibleRows()
	if m.deleteTree.Cursor < 0 || m.deleteTree.Cursor >= len(rows) {
		return app.DeleteCandidate{}, false
	}
	row := rows[m.deleteTree.Cursor]
	if row.Node == nil || row.Node.SelectMode != TreeSelectLeaf {
		return app.DeleteCandidate{}, false
	}
	idx, ok := row.Node.Payload.(int)
	if !ok {
		return app.DeleteCandidate{}, false
	}
	visible := m.visibleDeleteCandidates()
	if idx < 0 || idx >= len(visible) {
		return app.DeleteCandidate{}, false
	}
	return visible[idx], true
}

func (m *model) startRemoteEditAtCursor() tea.Cmd {
	cand, ok := m.cursorRemoteCandidate()
	if !ok {
		m.pushLog("Remote：请先把光标停在 Secure Note 或 SSH Key 叶子上")
		return nil
	}
	item := selectionFromCandidate(cand)
	switch item.Kind {
	case app.DeleteKindSecret, app.DeleteKindSSHKey:
		editorCmd := m.effectiveEditor()
		return prepareRemoteEditCmd(m.projectRoot, item, editorCmd)
	default:
		m.pushLog("Remote：只能编辑 Secure Note 或 SSH Hosts（e）")
		return nil
	}
}

func prepareRemoteEditCmd(projectRoot string, item app.DeleteSelectionItem, editorCmd string) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		switch item.Kind {
		case app.DeleteKindSecret:
			sess, err := serviceapi.PrepareRemoteNoteEdit(context.Background(), projectRoot, item, reporter)
			return remoteEditPreparedMsg{kind: remoteEditKindNote, noteSess: sess, editorCmd: editorCmd, err: err, logs: logs}
		case app.DeleteKindSSHKey:
			sess, err := serviceapi.PrepareRemoteSSHHostsEdit(context.Background(), projectRoot, item, reporter)
			return remoteEditPreparedMsg{kind: remoteEditKindSSH, sshSess: sess, editorCmd: editorCmd, err: err, logs: logs}
		default:
			return remoteEditPreparedMsg{err: fmt.Errorf("不支持的编辑类型"), logs: logs}
		}
	}
}

func (m *model) handleRemoteEditPrepared(msg remoteEditPreparedMsg) tea.Cmd {
	for _, line := range msg.logs {
		m.pushLog(line)
	}
	// 用户已 Esc 退出登记等待：迟到的会话不再拉起编辑器。
	if msg.register && !m.remoteRegisterPending {
		m.pushLog("Remote：登记已退出，忽略迟到的编辑会话")
		return nil
	}
	if msg.err != nil {
		if msg.register {
			m.addSecretStage = ""
			m.addSecretErr = msg.err
			m.remoteRegisterPending = false
			m.pushLog("Remote 登记准备失败: " + msg.err.Error())
		} else {
			m.pushLog("Remote 编辑准备失败: " + msg.err.Error())
		}
		return nil
	}
	path := ""
	switch msg.kind {
	case remoteEditKindNote:
		if msg.noteSess == nil {
			m.pushLog("Remote 编辑准备失败: 空会话")
			if msg.register {
				m.addSecretStage = ""
				m.remoteRegisterPending = false
			}
			return nil
		}
		m.remoteNoteEdit = msg.noteSess
		m.remoteSSHEdit = nil
		path = msg.noteSess.Path
		if msg.register {
			m.pushLog("Remote：外部编辑新 Secure Note…")
		} else {
			m.pushLog("Remote：外部编辑 Secure Note…")
		}
	case remoteEditKindSSH:
		if msg.sshSess == nil {
			m.pushLog("Remote 编辑准备失败: 空会话")
			return nil
		}
		m.remoteSSHEdit = msg.sshSess
		m.remoteNoteEdit = nil
		path = msg.sshSess.Path
		m.pushLog("Remote：外部编辑 SSH Hosts…")
	}
	cmd, err := editor.BuildCommand(path, msg.editorCmd)
	if err != nil {
		m.pushLog("Remote 打开编辑器失败: " + err.Error())
		m.clearRemoteEditSession()
		if msg.register {
			m.addSecretStage = ""
			m.remoteRegisterPending = false
		}
		return nil
	}
	kind := msg.kind
	register := msg.register
	return tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		return remoteEditEditorClosedMsg{kind: kind, err: runErr, register: register}
	})
}

type remoteEditEditorClosedMsg struct {
	kind     remoteEditKind
	err      error
	register bool
}

func (m *model) handleRemoteEditEditorClosed(msg remoteEditEditorClosedMsg) tea.Cmd {
	if msg.err != nil {
		m.pushLog("Remote 编辑器异常: " + msg.err.Error())
	}
	switch msg.kind {
	case remoteEditKindNote:
		if m.remoteNoteEdit == nil {
			if msg.register {
				m.addSecretStage = ""
				m.remoteRegisterPending = false
			}
			return nil
		}
		sess := *m.remoteNoteEdit
		if msg.register {
			return commitRemoteNoteRegisterCmd(sess)
		}
		return commitRemoteNoteEditCmd(sess)
	case remoteEditKindSSH:
		if m.remoteSSHEdit == nil {
			return nil
		}
		sess := *m.remoteSSHEdit
		return commitRemoteSSHEditCmd(sess)
	}
	return nil
}

func commitRemoteNoteRegisterCmd(sess app.RemoteNoteEditSession) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		err := serviceapi.CommitRemoteNoteRegister(context.Background(), sess, reporter)
		result := &app.AddSecretResult{
			TargetName:  sess.Target.Name,
			Address:     sess.Target.Address,
			NoteRelPath: sess.NoteRel,
		}
		return addSecretDoneMsg{result: result, err: err, logs: logs}
	}
}

func commitRemoteNoteEditCmd(sess app.RemoteNoteEditSession) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		err := serviceapi.CommitRemoteNoteEdit(context.Background(), sess, reporter)
		return remoteEditDoneMsg{kind: remoteEditKindNote, err: err, logs: logs}
	}
}

func commitRemoteSSHEditCmd(sess app.RemoteSSHHostsEditSession) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		err := serviceapi.CommitRemoteSSHHostsEdit(context.Background(), sess, reporter)
		return remoteEditDoneMsg{kind: remoteEditKindSSH, err: err, logs: logs}
	}
}

func (m *model) handleRemoteEditDone(msg remoteEditDoneMsg) tea.Cmd {
	for _, line := range msg.logs {
		m.pushLog(line)
	}
	m.clearRemoteEditSession()
	if msg.err != nil {
		m.pushLog("Remote 保存失败: " + msg.err.Error())
		return nil
	}
	m.pushLog("Remote 编辑已保存")
	return m.startDeleteCandidatesLoad(true, true)
}

func (m *model) clearRemoteEditSession() {
	m.remoteNoteEdit = nil
	m.remoteSSHEdit = nil
}

func (m model) effectiveEditor() string {
	if m.projectVars != nil {
		if ed := strings.TrimSpace(m.projectVars.EditorCommand); ed != "" {
			return ed
		}
	}
	if m.overview != nil {
		if ed := strings.TrimSpace(m.overview.Editor); ed != "" {
			return ed
		}
	}
	if m.settings != nil {
		if ed := strings.TrimSpace(m.settings.ConfiguredEditor); ed != "" {
			return ed
		}
	}
	return ""
}
