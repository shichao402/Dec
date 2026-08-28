package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/secrets"
)

func projectPageModelForAddSecret(t *testing.T) model {
	t.Helper()
	m := newModel(t.TempDir(), "v1.0.0")
	m.pageIndex = 3 // 设置（IDE / 登记 secret）
	m.focus = focusContent
	m.projectSettings = &app.ProjectSettingsState{ProjectConfigReady: true}
	return m
}

func typeRunes(m tea.Model, text string) tea.Model {
	for _, r := range text {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestAddSecret_TwoStagePromptRunsCommand(t *testing.T) {
	m := projectPageModelForAddSecret(t)

	opened, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	withTargets := opened.(model)
	withTargets.addSecretTargets = []app.SecretTargetOption{
		{Name: "vikunja", Plane: secrets.SyncPlaneProject, Address: "vikunja/private/project", LocalRoot: ".secrets/vikunja", Label: "vikunja/private/project → .secrets/vikunja"},
	}

	updated, _ := withTargets.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after := updated.(model)
	if after.addSecretStage != addSecretStagePath {
		t.Fatalf("stage = %q, 期望进入输入路径", after.addSecretStage)
	}

	updated = typeRunes(updated, "env/vikunja.env")
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretStage != addSecretStageRunning {
		t.Fatalf("stage = %q, 期望开始执行", after.addSecretStage)
	}
	if cmd == nil {
		t.Fatal("确认后应返回执行命令")
	}
}

func TestAddSecret_TabCyclesTargets(t *testing.T) {
	m := projectPageModelForAddSecret(t)
	opened, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	withTargets := opened.(model)
	withTargets.addSecretTargets = []app.SecretTargetOption{
		{Label: "vikunja", LocalRoot: ".secrets/bundles/vikunja"},
		{Label: "relkit", LocalRoot: ".secrets/bundles/relkit"},
	}

	updated, _ := withTargets.Update(tea.KeyMsg{Type: tea.KeyTab})
	after := updated.(model)
	if after.addSecretTargetIdx != 1 {
		t.Fatalf("tab 后 targetIdx = %d, 期望 1", after.addSecretTargetIdx)
	}
}

func TestAddSecret_EscCancelsWithoutRunning(t *testing.T) {
	m := projectPageModelForAddSecret(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	updated = typeRunes(updated, "env/local.env")
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	after := updated.(model)
	if after.addSecretStage != "" {
		t.Fatalf("stage = %q, 期望已取消", after.addSecretStage)
	}
	if after.addSecretPathInput != "" {
		t.Fatalf("取消后应清空输入, got %q", after.addSecretPathInput)
	}
	if cmd != nil {
		t.Fatal("取消不应触发任何命令")
	}
}

func TestAddSecret_EmptyPathDoesNotAdvance(t *testing.T) {
	m := projectPageModelForAddSecret(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	withTargets := updated.(model)
	withTargets.addSecretTargets = []app.SecretTargetOption{{Label: "vikunja", LocalRoot: ".secrets/bundles/vikunja"}}
	updated, _ = withTargets.Update(tea.KeyMsg{Type: tea.KeyEnter})

	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after := updated.(model)
	if after.addSecretStage != addSecretStagePath {
		t.Fatalf("stage = %q, 空路径不应开始执行", after.addSecretStage)
	}
	if cmd != nil {
		t.Fatal("空路径不应触发命令")
	}
}

func TestAddSecret_RequiresProjectConfig(t *testing.T) {
	m := newModel(t.TempDir(), "v1.0.0")
	m.pageIndex = 3
	m.focus = focusContent
	m.projectSettings = &app.ProjectSettingsState{ProjectConfigReady: false}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	after := updated.(model)
	if after.addSecretStage != "" {
		t.Fatalf("未初始化 .dec/config.yaml 时不应开启流程, stage = %q", after.addSecretStage)
	}
}

func TestAddSecret_RendersPromptAndOutcome(t *testing.T) {
	m := projectPageModelForAddSecret(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	view := updated.(model).View()
	if !strings.Contains(view, "登记新 secret") || !strings.Contains(view, "归属") {
		t.Fatalf("Project 页应展示登记输入区:\n%s", view)
	}

	running := updated.(model)
	running.addSecretStage = addSecretStageRunning
	done, _ := running.Update(addSecretDoneMsg{result: &app.AddSecretResult{
		Address:        "demo/private/project",
		NoteRelPath:    "env/vikunja.env",
		ProjectRelPath: ".secrets/demo/env/vikunja.env",
		LandingPath:    ".secrets/demo/env/vikunja.env",
	}})
	after := done.(model)
	if after.addSecretStage != "" {
		t.Fatalf("完成后应退出流程, stage = %q", after.addSecretStage)
	}
	if !strings.Contains(after.View(), "已登记 env/vikunja.env") {
		t.Fatalf("应展示登记结果:\n%s", after.View())
	}
}
