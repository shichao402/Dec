package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/secrets"
)

func TestBuildDeleteTree_UnfiledReadOnlyCollapsed(t *testing.T) {
	roots := buildDeleteTree([]app.DeleteCandidate{
		{
			Kind:          app.DeleteKindSecret,
			SecretPath:    "env/a.env",
			SecretsBundle: "Dec",
			LocalRoot:     ".secrets/project",
			Partition:     app.PartitionRemote,
			GroupTitle:    "Dec (project)",
		},
		{
			Kind:       app.DeleteKindSecret,
			Type:       "login",
			Name:       "loose",
			SecretPath: "loose",
			ReadOnly:   true,
			Unmanaged:  true,
			Partition:  app.PartitionRemote,
			GroupTitle: "无文件夹 · 非Dec管理",
		},
	})
	var hasUnfiled bool
	for _, r := range roots {
		if r != nil && r.ID == "delete-root:unfiled" {
			hasUnfiled = true
			if r.SelectMode != TreeSelectNone {
				t.Fatalf("无文件夹根应为 TreeSelectNone")
			}
			if len(r.Children) != 1 || r.Children[0].SelectMode != TreeSelectReadOnly {
				t.Fatalf("只读叶子异常: %#v", r.Children)
			}
		}
	}
	if !hasUnfiled {
		t.Fatalf("缺少无文件夹根: %#v", roots)
	}

	m := newModel(t.TempDir(), "v1")
	m.pages = []string{"Remote"}
	m.pageIndex = 0
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindSecret, SecretPath: "env/a.env", SecretsBundle: "Dec", LocalRoot: ".secrets/project", Partition: app.PartitionRemote, GroupTitle: "Dec"},
		{Kind: app.DeleteKindSecret, Type: "note", Name: "x", SecretPath: "x", ReadOnly: true, Unmanaged: true, Partition: app.PartitionRemote},
	}
	m.rebuildDeleteTree()
	if m.deleteTree.Expanded["delete-root:unfiled"] {
		t.Fatal("无文件夹区默认应折叠")
	}
}

func TestDeleteTypedConfirm_RequiresInput(t *testing.T) {
	m := newModel(t.TempDir(), "v1")
	m.pages = []string{"Remote"}
	m.pageIndex = 0
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{{
		Kind:          app.DeleteKindSecret,
		SecretPath:    "env/x.env",
		SecretsBundle: "relkit",
		Partition:     app.PartitionRemote,
		Unmanaged:     true,
	}}
	m.rebuildDeleteTree()
	// 勾选叶子
	for i := range m.deleteTree.Selected {
		m.deleteTree.Selected[i] = true
	}
	m.deleteStage = "summary"
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	after := updated.(model)
	if after.deleteStage != "typed" {
		t.Fatalf("跨上下文 summary y 后应进 typed, got %q", after.deleteStage)
	}
	view := after.View()
	if !strings.Contains(view, "relkit") || !strings.Contains(view, "DELETE") {
		t.Fatalf("typed 页应提示输入:\n%s", view)
	}
	// 错误输入
	updated, _ = after.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("nope")})
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.deleteStage != "typed" {
		t.Fatalf("错误输入后应留在 typed, got %q", after.deleteStage)
	}
	if cmd != nil {
		t.Fatal("错误输入不应启动删除")
	}
	// 正确输入 DELETE
	after.deleteTypedInput = ""
	updated = typeRunes(after, "DELETE")
	updated, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.deleteStage != "running" {
		t.Fatalf("DELETE 后应 running, got %q", after.deleteStage)
	}
	if cmd == nil {
		t.Fatal("应启动删除命令")
	}
}

func TestRemoteAddSecret_TakesFolderFromCursor(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	focusTreeRow(t, &m, "Dec")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	after := updated.(model)
	if !after.addSecretRemoteMode || after.addSecretStage != addSecretStageType {
		t.Fatalf("Remote n 应直接进类型阶段: mode=%v stage=%q", after.addSecretRemoteMode, after.addSecretStage)
	}
	if after.addSecretFolder != "Dec" {
		t.Fatalf("归属应来自光标所在 folder, got %q", after.addSecretFolder)
	}
	if after.addSecretFolderNew {
		t.Fatal("光标反推的 folder 不是新建")
	}
	if cmd != nil {
		t.Fatal("归属来自光标，不应再触发候选枚举")
	}
	view := after.View()
	if !strings.Contains(view, "Remote · 登记 Secret") || !strings.Contains(view, "folder Dec") {
		t.Fatalf("表单应展示光标解析出的归属:\n%s", view)
	}

	updated, _ = after.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretStage != addSecretStagePath {
		t.Fatalf("stage=%q want path", after.addSecretStage)
	}
	// 默认 note：输入普通路径（点类型路径须先改选对应 Processor）
	updated = typeRunes(after, "config/x.json")
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretStage != addSecretStageSource {
		t.Fatalf("stage=%q want source; notice=%q", after.addSecretStage, after.addSecretNotice)
	}
}

func TestRemoteAddSecret_CursorInTypeDirPreselectsType(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	focusTreeRowByLabel(t, &m, ".env")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	after := updated.(model)
	procs := remoteAddSecretProcessors()
	if after.addSecretTypeIdx >= len(procs) || procs[after.addSecretTypeIdx].ID != secrets.SecretTypeEnv {
		t.Fatalf("光标停在 .env 目录时应预选 .env 类型, idx=%d", after.addSecretTypeIdx)
	}
	updated, _ = after.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if !strings.HasPrefix(after.addSecretPathInput, secrets.TypeDirEnv+"/") {
		t.Fatalf("路径应按类型预填, got %q", after.addSecretPathInput)
	}
}

// 四种 Processor 同级：note / .gcm / .env / .sshkey 都在轮转里，且都能进入名称阶段。
func TestRemoteAddSecret_TypeCycleIncludesAllProcessors(t *testing.T) {
	procs := remoteAddSecretProcessors()
	want := map[secrets.SecretTypeID]bool{
		secrets.SecretTypePlain:  false,
		secrets.SecretTypeGCM:    false,
		secrets.SecretTypeEnv:    false,
		secrets.SecretTypeSSHKey: false,
	}
	for _, p := range procs {
		if _, ok := want[p.ID]; ok {
			want[p.ID] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("类型表缺少 %s", id)
		}
	}

	m := remotePageModelWithCandidates(t)
	focusTreeRow(t, &m, "Dec")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	for i := 0; i < len(procs); i++ {
		next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if stage := next.(model).addSecretStage; stage != addSecretStagePath {
			t.Fatalf("第 %d 个类型 Enter 后 stage=%q，期望进入 path", i, stage)
		}
		updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
}

// 光标停在 .sshkey 目录下按 n：预选 .sshkey，进入名称后进 generate/path/picker 来源阶段。
func TestRemoteAddSecret_SSHKeyDirEntersSSHSources(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	m.deleteCandidates = append(m.deleteCandidates, app.DeleteCandidate{
		Kind: app.DeleteKindSSHKey, SSHKeyName: ".sshkey/deploy", SecretsBundle: "Dec",
		Partition: app.PartitionRemote, GroupTitle: "Dec",
	})
	m.rebuildDeleteTree()
	focusTreeRowByLabel(t, &m, ".sshkey")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	after := updated.(model)
	procs := remoteAddSecretProcessors()
	if after.addSecretTypeIdx >= len(procs) || procs[after.addSecretTypeIdx].ID != secrets.SecretTypeSSHKey {
		t.Fatalf("应预选 .sshkey, idx=%d", after.addSecretTypeIdx)
	}
	updated, _ = after.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretPathInput != ".sshkey/deploy" {
		t.Fatalf("应预填 .sshkey/deploy, got %q", after.addSecretPathInput)
	}
	updated, _ = after.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretStage != addSecretStageSource {
		t.Fatalf("应进入来源阶段, stage=%q", after.addSecretStage)
	}
	if after.addSecretSourceMode != string(secrets.SourceGenerate) {
		t.Fatalf("默认来源应为 generate, got %q", after.addSecretSourceMode)
	}
	view := after.View()
	if !strings.Contains(view, "本机生成") || !strings.Contains(view, "系统选文件") {
		t.Fatalf("来源提示应列出 generate/path/picker:\n%s", view)
	}
}

func TestRemotePage_QuitsFromList(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("Remote 列表按 q 应退出")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("q 应返回 tea.Quit")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("q 返回 %T, 期望 tea.QuitMsg", msg)
	}
}

func TestRemoteAddSecret_NoFolderUnderCursorKeepsFormClosed(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	m.deleteTree.Cursor = 0 // 分区根，归属不唯一

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	after := updated.(model)
	if after.addSecretStage != "" {
		t.Fatalf("解析不出归属时不应开表单, stage=%q", after.addSecretStage)
	}
	if cmd != nil {
		t.Fatal("解析不出归属时不应触发命令")
	}
	if !strings.Contains(strings.Join(after.logs, "\n"), "按 N") {
		t.Fatalf("应提示移动光标或按 N 新建 folder:\n%s", strings.Join(after.logs, "\n"))
	}
}

func TestRemoteAddSecret_NewFolderTypedByHand(t *testing.T) {
	m := remotePageModelWithCandidates(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	after := updated.(model)
	if after.addSecretStage != addSecretStageFolder || !after.addSecretFolderNew {
		t.Fatalf("N 应进入新 folder 输入: stage=%q new=%v", after.addSecretStage, after.addSecretFolderNew)
	}
	if view := after.View(); !strings.Contains(view, "新 folder") {
		t.Fatalf("表单应标明这是新 folder:\n%s", view)
	}

	updated = typeRunes(after, "bundle/newpkg")
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretStage != addSecretStageType || after.addSecretFolder != "bundle/newpkg" {
		t.Fatalf("folder=%q stage=%q", after.addSecretFolder, after.addSecretStage)
	}

	updated, _ = after.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = typeRunes(updated, "x.txt")
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretStage != addSecretStageSource {
		t.Fatalf("stage=%q want source", after.addSecretStage)
	}
}

func TestRemoteAddSecret_EmptyNewFolderDoesNotAdvance(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after := updated.(model)
	if after.addSecretStage != addSecretStageFolder {
		t.Fatalf("空 folder 名不应推进, stage=%q", after.addSecretStage)
	}
	if cmd != nil {
		t.Fatal("空 folder 名不应触发命令")
	}
}

func TestRemoteAddSecret_NewBareFolderRequiresDeclaredProject(t *testing.T) {
	oldValidate := validateRemoteRegisterFolderOperation
	validateRemoteRegisterFolderOperation = func(context.Context, app.Workspace, string) error {
		return fmt.Errorf("folder %q 不是 vault 已声明的 project；新建归属请改用 %q", "relkit", "bundle/relkit")
	}
	t.Cleanup(func() { validateRemoteRegisterFolderOperation = oldValidate })

	m := remotePageModelWithCandidates(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	updated = typeRunes(updated, "relkit")
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after := updated.(model)
	if after.addSecretStage != addSecretStageFolderCheck || cmd == nil {
		t.Fatalf("裸名应先异步校验 vault 声明: stage=%q cmd=%v", after.addSecretStage, cmd)
	}
	updated, _ = after.Update(cmd())
	after = updated.(model)
	if after.addSecretStage != addSecretStageFolder || !strings.Contains(after.addSecretNotice, "bundle/relkit") {
		t.Fatalf("非法裸名应留在 folder 阶段并提示 bundle/<名>: stage=%q notice=%q", after.addSecretStage, after.addSecretNotice)
	}
}

// focusTreeRow 把光标停在指定 folder 的分组节点上。
func focusTreeRow(t *testing.T, m *model, folder string) {
	t.Helper()
	for i, row := range m.deleteTree.VisibleRows() {
		if row.Node == nil {
			continue
		}
		if ref, ok := row.Node.Payload.(secretsFolderRef); ok && ref.Folder == folder {
			m.deleteTree.Cursor = i
			return
		}
	}
	t.Fatalf("树中找不到 folder %q 的分组节点", folder)
}

func focusTreeRowByLabel(t *testing.T, m *model, label string) {
	t.Helper()
	for i, row := range m.deleteTree.VisibleRows() {
		if row.Node != nil && row.Node.Label == label {
			m.deleteTree.Cursor = i
			return
		}
	}
	t.Fatalf("树中找不到标签为 %q 的节点", label)
}

func TestRemoteAddSecret_EscLeavesRunningStage(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	m.addSecretRemoteMode = true
	m.addSecretStage = addSecretStageRunning
	m.remoteRegisterPending = true

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	after := updated.(model)
	if after.addSecretStage != "" {
		t.Fatalf("running 阶段 Esc 应退出等待, stage=%q", after.addSecretStage)
	}
	if after.remoteRegisterPending {
		t.Fatal("退出等待后不应再拉起编辑器")
	}
}

func TestRemoteAddSecret_StaleResultKeepsNewForm(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	focusTreeRow(t, &m, "Dec")
	m.addSecretRemoteMode = true
	m.addSecretStage = addSecretStageRunning

	abandoned, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	reopened, _ := abandoned.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	late, _ := reopened.Update(addSecretDoneMsg{result: &app.AddSecretResult{Folder: "relkit", NoteRelPath: ".env/x.env"}})

	after := late.(model)
	if after.addSecretStage != addSecretStageType {
		t.Fatalf("迟到的结果不应清掉新开的表单, stage=%q", after.addSecretStage)
	}
}

func TestRemoteSelection_ToggleAllAndClear(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	total := m.deleteTree.CountSelectable()
	if total == 0 {
		t.Fatal("测试树应有可选项")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	after := updated.(model)
	if after.deleteTree.CountSelected() != total {
		t.Fatalf("a 应全选 %d 项, got %d", total, after.deleteTree.CountSelected())
	}

	updated, _ = after.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	after = updated.(model)
	if after.deleteTree.CountSelected() != 0 {
		t.Fatalf("A 应全不选, 仍剩 %d 项", after.deleteTree.CountSelected())
	}
	if after.addSecretStage != "" {
		t.Fatalf("A 不应再开启登记流程, stage=%q", after.addSecretStage)
	}
}

func TestRemoteHeadLines_ShowsNewKeyHints(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	head := strings.Join(m.remoteHeadLines(), "\n")
	for _, want := range []string{"a 全选", "A 全不选", "n 登记到光标 folder", "N 登记到新 folder"} {
		if !strings.Contains(head, want) {
			t.Fatalf("帮助行缺少 %q:\n%s", want, head)
		}
	}
}

func remotePageModelWithCandidates(t *testing.T) model {
	t.Helper()
	m := newModel(t.TempDir(), "v1")
	m.pages = []string{"Remote"}
	m.pageIndex = 0
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindSecret, SecretPath: ".env/a.env", SecretsBundle: "Dec", LocalRoot: ".secrets/project", Partition: app.PartitionRemote, GroupTitle: "Dec"},
		{Kind: app.DeleteKindSecret, SecretPath: ".env/b.env", SecretsBundle: "Dec", LocalRoot: ".secrets/project", Partition: app.PartitionRemote, GroupTitle: "Dec"},
	}
	m.deleteCandidatesLoaded = true
	m.rebuildDeleteTree()
	return m
}
