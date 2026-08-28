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

func TestRemoteAddSecret_TakesScopeFromCursor(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	focusTreeRow(t, &m, remoteTestAddress)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	after := updated.(model)
	if !after.addSecretRemoteMode || after.addSecretStage != addSecretStageType {
		t.Fatalf("Remote n 应直接进类型阶段: mode=%v stage=%q", after.addSecretRemoteMode, after.addSecretStage)
	}
	if after.addSecretPName != "dec" || after.addSecretPlane != secrets.SyncPlaneProject {
		t.Fatalf("归属应来自光标所在 P 地址, got p=%q plane=%q", after.addSecretPName, after.addSecretPlane)
	}
	if after.addSecretScopeNew {
		t.Fatal("光标反推的归属不是新建 P")
	}
	if cmd != nil {
		t.Fatal("归属来自光标，不应再触发候选枚举")
	}
	view := after.View()
	if !strings.Contains(view, "Remote · 登记 Secret") || !strings.Contains(view, remoteTestAddress) {
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
	expandDeleteTreeAll(&m)
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
	focusTreeRow(t, &m, remoteTestAddress)
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
		Kind: app.DeleteKindSSHKey, SSHKeyName: ".sshkey/deploy", SecretsBundle: remoteTestAddress,
		Partition: app.PartitionRemote, GroupTitle: remoteTestAddress,
	})
	m.rebuildDeleteTree()
	expandDeleteTreeAll(&m)
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
		t.Fatalf("应提示移动光标或按 N 新建 P:\n%s", strings.Join(after.logs, "\n"))
	}
}

func TestRemoteAddSecret_NewPTypedByHand(t *testing.T) {
	oldValidate := validateRemoteRegisterScopeOperation
	validateRemoteRegisterScopeOperation = func(context.Context, app.Workspace, secrets.RemoteScope) error { return nil }
	t.Cleanup(func() { validateRemoteRegisterScopeOperation = oldValidate })

	m := remotePageModelWithCandidates(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	after := updated.(model)
	if after.addSecretStage != addSecretStageP || !after.addSecretScopeNew {
		t.Fatalf("N 应进入新 P 输入: stage=%q new=%v", after.addSecretStage, after.addSecretScopeNew)
	}
	if view := after.View(); !strings.Contains(view, "新 P") {
		t.Fatalf("表单应标明这是新 P:\n%s", view)
	}

	updated = typeRunes(after, "newpkg")
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretStage != addSecretStagePlane || after.addSecretPName != "newpkg" {
		t.Fatalf("P=%q stage=%q，期望进入选平面", after.addSecretPName, after.addSecretStage)
	}

	// tab 轮转到本机平面，再 Enter 触发声明校验。
	updated, _ = after.Update(tea.KeyMsg{Type: tea.KeyTab})
	after = updated.(model)
	if !secrets.IsMachinePlane(after.addSecretPlane) {
		t.Fatalf("tab 应轮到本机平面, got %q", after.addSecretPlane)
	}
	updated, cmd := after.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after = updated.(model)
	if after.addSecretStage != addSecretStageScopeCheck || cmd == nil {
		t.Fatalf("选完平面应异步校验: stage=%q cmd=%v", after.addSecretStage, cmd)
	}
	updated, _ = after.Update(cmd())
	after = updated.(model)
	if after.addSecretStage != addSecretStageType {
		t.Fatalf("校验通过应进入类型阶段, stage=%q notice=%q", after.addSecretStage, after.addSecretNotice)
	}
}

func TestRemoteAddSecret_EmptyNewPDoesNotAdvance(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after := updated.(model)
	if after.addSecretStage != addSecretStageP {
		t.Fatalf("空 P 名不应推进, stage=%q", after.addSecretStage)
	}
	if cmd != nil {
		t.Fatal("空 P 名不应触发命令")
	}
}

// P 名里带斜杠说明用户还在按老的 folder 路径思维输入，必须当场拦下。
func TestRemoteAddSecret_RejectsSlashInPName(t *testing.T) {
	m := remotePageModelWithCandidates(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	updated = typeRunes(updated, "bundle/newpkg")
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after := updated.(model)
	if after.addSecretStage != addSecretStageP || cmd != nil {
		t.Fatalf("带斜杠的输入不应推进: stage=%q cmd=%v", after.addSecretStage, cmd)
	}
	if !strings.Contains(after.addSecretNotice, "只输入 P 名") {
		t.Fatalf("应提示只输入 P 名, notice=%q", after.addSecretNotice)
	}
}

func TestRemoteAddSecret_UndeclaredPStaysInPStage(t *testing.T) {
	oldValidate := validateRemoteRegisterScopeOperation
	validateRemoteRegisterScopeOperation = func(context.Context, app.Workspace, secrets.RemoteScope) error {
		return fmt.Errorf("P %q 不是 vault 已声明的 project", "relkit")
	}
	t.Cleanup(func() { validateRemoteRegisterScopeOperation = oldValidate })

	m := remotePageModelWithCandidates(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	updated = typeRunes(updated, "relkit")
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	after := updated.(model)
	if after.addSecretStage != addSecretStageScopeCheck || cmd == nil {
		t.Fatalf("应先异步校验 vault 声明: stage=%q cmd=%v", after.addSecretStage, cmd)
	}
	updated, _ = after.Update(cmd())
	after = updated.(model)
	if after.addSecretStage != addSecretStageP || !strings.Contains(after.addSecretNotice, "relkit") {
		t.Fatalf("未声明的 P 应留在 P 阶段并给出原因: stage=%q notice=%q", after.addSecretStage, after.addSecretNotice)
	}
}

// focusTreeRow 把光标停在指定远端地址的分组节点上。
func focusTreeRow(t *testing.T, m *model, folder string) {
	t.Helper()
	for i, row := range m.deleteTree.VisibleRows() {
		if row.Node == nil {
			continue
		}
		if ref, ok := row.Node.Payload.(secretsFolderRef); ok && ref.Address == folder {
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
	focusTreeRow(t, &m, remoteTestAddress)
	m.addSecretRemoteMode = true
	m.addSecretStage = addSecretStageRunning

	abandoned, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	reopened, _ := abandoned.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	late, _ := reopened.Update(addSecretDoneMsg{result: &app.AddSecretResult{Address: "relkit/private/project", NoteRelPath: ".env/x.env"}})

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
	for _, want := range []string{"a 全选", "A 全不选", "n 登记到光标 P", "N 新建 P 登记"} {
		if !strings.Contains(head, want) {
			t.Fatalf("帮助行缺少 %q:\n%s", want, head)
		}
	}
}

// remoteTestAddress 是测试树里的远端地址，必须是 <p>/private/<plane> 才能反推出 scope。
const remoteTestAddress = "dec/private/project"

func remotePageModelWithCandidates(t *testing.T) model {
	t.Helper()
	m := newModel(t.TempDir(), "v1")
	m.pages = []string{"Remote"}
	m.pageIndex = 0
	m.focus = focusContent
	m.deleteCandidates = []app.DeleteCandidate{
		{Kind: app.DeleteKindSecret, SecretPath: ".env/a.env", SecretsBundle: remoteTestAddress, LocalRoot: ".secrets/dec", Partition: app.PartitionRemote, GroupTitle: remoteTestAddress},
		{Kind: app.DeleteKindSecret, SecretPath: ".env/b.env", SecretsBundle: remoteTestAddress, LocalRoot: ".secrets/dec", Partition: app.PartitionRemote, GroupTitle: remoteTestAddress},
	}
	m.deleteCandidatesLoaded = true
	m.rebuildDeleteTree()
	return m
}
