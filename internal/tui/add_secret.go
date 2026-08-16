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

// 登记新 secret 的分阶段状态。
// Project 页：先选归属（本地同步根），再输入相对路径。
// Remote 页：归属来自光标所在 folder（n）或手输新 folder（N），再选类型、路径、内容来源。
const (
	addSecretStageTarget  = "target" // Project only：轮转本地同步根
	addSecretStageFolder  = "folder" // Remote only：手输新 folder
	addSecretStageType    = "type"   // Remote only：点类型
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

// addSecretTargetsMsg 回送异步枚举出的候选归属（Project 页登记）。
type addSecretTargetsMsg struct {
	targets []app.SecretTargetOption
	err     error
	loadGen uint64
}

// suggestSecretTargetsCmd 走服务 RPC 枚举候选归属，耗时不可预期，
// 必须放在 tea.Cmd 里跑，不能在 Update 中同步调用。
func suggestSecretTargetsCmd(projectRoot string, loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		targets, err := serviceapi.SuggestSecretTargets(projectRoot)
		return addSecretTargetsMsg{targets: targets, err: err, loadGen: loadGen}
	}
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

func prepareRemoteRegisterCmd(projectRoot, folder, noteRel, initialBody, editorCmd string) tea.Cmd {
	return func() tea.Msg {
		var logs []string
		reporter := app.ReporterFunc(func(event app.OperationEvent) {
			if msg := strings.TrimSpace(event.Message); msg != "" {
				logs = append(logs, msg)
			}
		})
		sess, err := serviceapi.PrepareRemoteNoteRegister(context.Background(), projectRoot, folder, noteRel, initialBody, reporter)
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

// remoteAddSecretTypes 返回 Remote 登记可选类型：普通 note + 以 Secure Note 落地的点类型。
// .sshkey 是 BW SSH Key Item，不能由本流程创建，因此不进可选列表（选中即死路）。
func remoteAddSecretTypes() []secrets.SecretType {
	out := []secrets.SecretType{{ID: secrets.SecretTypePlain, Dir: "", Source: "note"}}
	for _, t := range secrets.RegisteredSecretTypes() {
		if t.ID == secrets.SecretTypeSSHKey {
			continue
		}
		out = append(out, t)
	}
	return out
}

// sshKeyRegisterHint 是 .sshkey 不走本流程时给用户的去处说明。
const sshKeyRegisterHint = ".sshkey 是 Bitwarden SSH Key Item，不能登记成 Secure Note：请在 Bitwarden 新建 SSH Key，Item 名用 .sshkey/<实例>"

func remoteAddSecretTypeLabel(t secrets.SecretType) string {
	switch t.ID {
	case secrets.SecretTypePlain:
		return "note（任意路径）"
	case secrets.SecretTypeGCM:
		return ".gcm（Git Credential Manager）"
	case secrets.SecretTypeEnv:
		return ".env（dotenv / dec-exec）"
	default:
		if t.Dir != "" {
			return t.Dir
		}
		return string(t.ID)
	}
}

// remoteRegisterAnchor 是从 Remote 光标就近反推出的登记落点。
type remoteRegisterAnchor struct {
	Folder string // Bitwarden folder
	Dir    string // folder 内相对目录（光标停在 .env/ 之下时为 ".env"）
	Local  bool   // 光标停在本地分区（folder 身份一致，登记仍写远端）
}

// cursorRegisterAnchor 反查光标所在的 folder 分组节点。
// 分组节点 ID 是其所有子孙 ID 的前缀，直接按前缀归属即可，无需回溯父链。
func (m model) cursorRegisterAnchor() (remoteRegisterAnchor, bool) {
	rows := m.deleteTree.VisibleRows()
	if m.deleteTree.Cursor < 0 || m.deleteTree.Cursor >= len(rows) {
		return remoteRegisterAnchor{}, false
	}
	node := rows[m.deleteTree.Cursor].Node
	if node == nil {
		return remoteRegisterAnchor{}, false
	}
	group, ref, ok := findSecretsFolderGroup(m.deleteTree.Roots, node.ID)
	if !ok || strings.TrimSpace(ref.Folder) == "" {
		return remoteRegisterAnchor{}, false
	}
	return remoteRegisterAnchor{
		Folder: strings.TrimSpace(ref.Folder),
		Dir:    treeDirUnderGroup(group.ID, node.ID),
		Local:  ref.Partition == app.PartitionLocal,
	}, true
}

func findSecretsFolderGroup(nodes []*TreeNode, nodeID string) (*TreeNode, secretsFolderRef, bool) {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if ref, ok := n.Payload.(secretsFolderRef); ok {
			if n.ID == nodeID || strings.HasPrefix(nodeID, n.ID+"/") {
				return n, ref, true
			}
		}
		if group, ref, ok := findSecretsFolderGroup(n.Children, nodeID); ok {
			return group, ref, true
		}
	}
	return nil, secretsFolderRef{}, false
}

// treeDirUnderGroup 取节点在 folder 内的目录路径；叶子段（leaf:）不算目录。
func treeDirUnderGroup(groupID, nodeID string) string {
	rel := strings.Trim(strings.TrimPrefix(nodeID, groupID), "/")
	if rel == "" {
		return ""
	}
	segs := make([]string, 0, 4)
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || strings.HasPrefix(seg, "leaf:") {
			continue
		}
		segs = append(segs, seg)
	}
	return strings.Join(segs, "/")
}

// validateRemoteNotePath 在提交前拦掉本流程写不出来的 Note 名：
// 未知点目录会在服务端硬失败，.sshkey 则根本不是 Secure Note。
func validateRemoteNotePath(rel string) error {
	tp, ok, err := secrets.ParseTypePath(rel)
	if err != nil {
		return err
	}
	if ok && tp.Type.ID == secrets.SecretTypeSSHKey {
		return fmt.Errorf("%s", sshKeyRegisterHint)
	}
	return nil
}

func normalizeRemoteFolderInput(raw string) string {
	folder := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	return strings.Trim(folder, "/")
}

// remoteTypeIdxForDir 让光标所在的点目录直接决定默认类型。
func remoteTypeIdxForDir(dir string) int {
	head := dir
	if idx := strings.Index(head, "/"); idx >= 0 {
		head = head[:idx]
	}
	head = strings.TrimSpace(head)
	if head == "" {
		return 0
	}
	for i, t := range remoteAddSecretTypes() {
		if t.Dir != "" && t.Dir == head {
			return i
		}
	}
	return 0
}

func (m *model) resetAddSecretForm(remoteMode bool) {
	m.addSecretPathInput = ""
	m.addSecretContentPath = ""
	m.addSecretSourceMode = "temp"
	m.addSecretTargetIdx = 0
	m.addSecretTypeIdx = 0
	m.addSecretInitialBody = ""
	m.addSecretTargets = nil
	m.addSecretFolder = ""
	m.addSecretFolderNew = false
	m.addSecretNotice = ""
	m.addSecretResult = nil
	m.addSecretErr = nil
	m.addSecretRemoteMode = remoteMode
	m.remoteRegisterPending = false
}

// noteAddSecret 同时写表单内提示与日志：表单整页渲染时日志区不可见。
func (m *model) noteAddSecret(msg string) {
	m.addSecretNotice = msg
	m.pushLog(msg)
}

// beginAddSecret 是 Project 页 A：归属只能是本机 SyncTarget，仍需枚举后轮转选择。
func (m *model) beginAddSecret() tea.Cmd {
	m.resetAddSecretForm(false)
	m.addSecretStage = addSecretStageTarget
	_, gen := m.addSecretTargetsLoad.begin()
	m.pushLog("登记新 secret：读取可选归属…（Esc 取消）")
	return suggestSecretTargetsCmd(m.projectRoot, gen)
}

// beginRemoteRegisterAtCursor 是 Remote 页 n：归属直接取光标所在 folder，表单内不再选归属。
// 归属不对就 Esc 退出、移动光标重按 n。
func (m *model) beginRemoteRegisterAtCursor() tea.Cmd {
	anchor, ok := m.cursorRegisterAnchor()
	if !ok {
		m.pushLog("Remote 登记：把光标移到某个 folder（或其目录 / 条目）上再按 n；新建 folder 按 N")
		return nil
	}
	m.resetAddSecretForm(true)
	m.addSecretFolder = anchor.Folder
	m.addSecretTypeIdx = remoteTypeIdxForDir(anchor.Dir)
	m.addSecretStage = addSecretStageType
	where := anchor.Folder
	if anchor.Dir != "" {
		where += "/" + anchor.Dir
	}
	m.pushLog(fmt.Sprintf("Remote 登记 → %s（光标所在）；选择类型（tab 轮转 · Esc 取消）", where))
	if isSSHKeyDir(anchor.Dir) {
		m.noteAddSecret(sshKeyRegisterHint)
	}
	return nil
}

func isSSHKeyDir(dir string) bool {
	head := dir
	if idx := strings.Index(head, "/"); idx >= 0 {
		head = head[:idx]
	}
	return strings.TrimSpace(head) == secrets.TypeDirSSHKey
}

// beginRemoteRegisterNewFolder 是 Remote 页 N：folder 尚未出现在树上（新 bundle / 新 project / 空 folder）时手输。
func (m *model) beginRemoteRegisterNewFolder() tea.Cmd {
	m.resetAddSecretForm(true)
	m.addSecretFolderNew = true
	m.addSecretStage = addSecretStageFolder
	m.pushLog("Remote 登记：输入 folder 名（bundle/<名> = secrets bundle；裸名 = project folder）")
	return nil
}

func (m *model) applyAddSecretTargets(msg addSecretTargetsMsg) {
	if !m.addSecretTargetsLoad.finish(msg.loadGen) {
		return
	}
	if m.addSecretStage == "" {
		return
	}
	if msg.err != nil {
		m.addSecretTargets = nil
		m.pushLog("读取可选 secrets 归属失败: " + msg.err.Error())
		return
	}
	m.addSecretTargets = msg.targets
	if m.addSecretTargetIdx >= len(m.addSecretTargets) {
		m.addSecretTargetIdx = 0
	}
	m.pushLog(fmt.Sprintf("登记新 secret：%d 个候选归属（project 或已启用 bundle）", len(msg.targets)))
}

func (m *model) cancelAddSecret() {
	m.addSecretTargetsLoad.clear()
	m.addSecretStage = ""
	m.addSecretPathInput = ""
	m.addSecretContentPath = ""
	m.addSecretTargets = nil
	m.addSecretFolder = ""
	m.addSecretFolderNew = false
	m.addSecretRemoteMode = false
	m.remoteRegisterPending = false
	m.pushLog("已取消登记 secret")
}

// abandonAddSecretRun 让用户在远端请求久久不返回时也能退出表单。
// 在飞的请求由服务端继续跑完，结果只落日志，不再回写这轮表单。
func (m *model) abandonAddSecretRun() {
	m.addSecretStage = ""
	m.remoteRegisterPending = false
	m.pushLog("已退出登记等待：后台请求仍在进行，结果只记入日志")
}

func (m model) handleAddSecretKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.addSecretStage == addSecretStageRunning {
		if msg.Type == tea.KeyEsc {
			m.abandonAddSecretRun()
		}
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
		if m.addSecretStage == addSecretStageType && m.addSecretRemoteMode {
			types := remoteAddSecretTypes()
			if len(types) > 0 {
				m.addSecretTypeIdx = (m.addSecretTypeIdx + 1) % len(types)
			}
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
		m.addSecretNotice = ""
		if m.addSecretStage == addSecretStageFolder {
			m.addSecretFolder = trimLastRune(m.addSecretFolder)
		}
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
		m.addSecretNotice = ""
		switch m.addSecretStage {
		case addSecretStageFolder:
			m.addSecretFolder += string(msg.Runes)
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
	m.addSecretNotice = ""

	if m.addSecretStage == addSecretStageTarget {
		if m.addSecretTargetsLoad.busy() && len(m.addSecretTargets) == 0 {
			m.noteAddSecret("候选列表读取中，请稍候（Esc 取消）")
			return m, nil
		}
		if len(m.addSecretTargets) == 0 {
			m.noteAddSecret("没有可选的 secrets 归属，请先在 Bundles 页启用 bundle")
			return m, nil
		}
		m.addSecretStage = addSecretStagePath
		opt := m.addSecretTargets[m.addSecretTargetIdx]
		m.pushLog(fmt.Sprintf("输入相对 %s 的路径（如 .env/vikunja.env）", opt.LocalRoot))
		return m, nil
	}

	if m.addSecretStage == addSecretStageFolder {
		folder := normalizeRemoteFolderInput(m.addSecretFolder)
		if folder == "" {
			m.noteAddSecret("folder 名不能为空（bundle/<名> 或裸 project 名）")
			return m, nil
		}
		m.addSecretFolder = folder
		m.addSecretStage = addSecretStageType
		m.pushLog(fmt.Sprintf("Remote 登记 → folder %s；选择类型（tab 轮转）", folder))
		return m, nil
	}

	if m.addSecretStage == addSecretStageType {
		types := remoteAddSecretTypes()
		if len(types) == 0 {
			return m, nil
		}
		if m.addSecretTypeIdx >= len(types) {
			m.addSecretTypeIdx = 0
		}
		st := types[m.addSecretTypeIdx]
		m.addSecretInitialBody = st.Template
		switch st.ID {
		case secrets.SecretTypeGCM:
			m.addSecretPathInput = secrets.SuggestNotePath(secrets.SecretTypeGCM, "cnb")
		case secrets.SecretTypeEnv:
			m.addSecretPathInput = secrets.SuggestNotePath(secrets.SecretTypeEnv, "app")
		default:
			m.addSecretPathInput = ""
		}
		m.addSecretStage = addSecretStagePath
		m.pushLog(fmt.Sprintf("输入 Note 名（相对 folder %s）；已按类型预填建议路径", m.addSecretFolder))
		return m, nil
	}

	rel := strings.TrimSpace(m.addSecretPathInput)
	if rel == "" {
		m.noteAddSecret("相对路径 / Note 名不能为空")
		return m, nil
	}

	if m.addSecretRemoteMode {
		folder := strings.TrimSpace(m.addSecretFolder)
		if folder == "" {
			m.noteAddSecret("没有解析到归属 folder，请 Esc 后重新按 n / N")
			return m, nil
		}
		if err := validateRemoteNotePath(rel); err != nil {
			m.noteAddSecret(err.Error())
			return m, nil
		}
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
					m.noteAddSecret("请输入本地文件路径，或 tab 切到 temp 编辑器")
					return m, nil
				}
				m.addSecretStage = addSecretStageRunning
				m.pushLog(fmt.Sprintf("登记 %s → %s（本地路径）", rel, folder))
				return m, registerRemoteFromPathCmd(m.projectRoot, folder, rel, localPath)
			}
			m.addSecretStage = addSecretStageRunning
			m.remoteRegisterPending = true
			m.pushLog(fmt.Sprintf("打开临时编辑器登记 %s → %s", rel, folder))
			return m, prepareRemoteRegisterCmd(m.projectRoot, folder, rel, m.addSecretInitialBody, m.effectiveEditor())
		}
	}

	if len(m.addSecretTargets) == 0 {
		m.noteAddSecret("没有可选的 secrets 归属")
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
	return strings.Join(m.addSecretBlockLines(), "\n")
}

func (m model) addSecretBlockLines() []string {
	title := "登记新 secret"
	if m.addSecretRemoteMode {
		title = "Remote · 登记 Secure Note"
	}
	lines := []string{shellTitleStyle.Render(title)}
	if m.addSecretRemoteMode {
		lines = append(lines, shellMutedStyle.Render("归属来自光标所在 folder；内容来自 temp 或本地路径；不种本地同步根。"))
	} else {
		lines = append(lines, shellMutedStyle.Render("Note 名 = 相对同步根路径；文件须先写到对应 .secrets/ 目录。"))
	}

	loadingTargets := m.addSecretTargetsLoad.busy() && len(m.addSecretTargets) == 0
	targetLine := "归属: <待选择>"
	switch {
	case m.addSecretRemoteMode && m.addSecretFolderNew:
		targetLine = fmt.Sprintf("归属: 新 folder %s", fallbackValue(m.addSecretFolder, "<待输入>"))
	case m.addSecretRemoteMode:
		targetLine = fmt.Sprintf("归属: folder %s（光标所在）", fallbackValue(m.addSecretFolder, "<未解析>"))
	case loadingTargets:
		targetLine = "归属: <读取中…>"
	case len(m.addSecretTargets) > 0:
		opt := m.addSecretTargets[m.addSecretTargetIdx]
		targetLine = fmt.Sprintf("归属: %s", opt.Label)
	}
	pathLine := fmt.Sprintf("Note 路径: %s", fallbackValue(m.addSecretPathInput, "<待输入>"))
	typeLine := "类型: note（任意路径）"
	if types := remoteAddSecretTypes(); m.addSecretTypeIdx >= 0 && m.addSecretTypeIdx < len(types) {
		typeLine = "类型: " + remoteAddSecretTypeLabel(types[m.addSecretTypeIdx])
	}
	sourceLine := "内容: 外部编辑器 (temp)"
	if m.addSecretSourceMode == "path" {
		sourceLine = fmt.Sprintf("内容: 本地路径 %s", fallbackValue(m.addSecretContentPath, "<待输入>"))
	}

	switch m.addSecretStage {
	case addSecretStageTarget:
		lines = append(lines, shellSelectedRow.Render(targetLine+"▌"), shellLogStyle.Render(pathLine))
		if loadingTargets {
			lines = append(lines, shellWarnStyle.Render("正在枚举候选归属…"))
		}
		if len(m.addSecretTargets) > 0 {
			labels := make([]string, 0, len(m.addSecretTargets))
			for _, opt := range m.addSecretTargets {
				labels = append(labels, opt.Folder)
			}
			if len(labels) > 8 {
				labels = append(labels[:8], "…")
			}
			lines = append(lines, shellMutedStyle.Render("候选归属: "+strings.Join(labels, " · ")+"（tab 轮转）"))
		}
		lines = append(lines, shellMutedStyle.Render("Enter 下一步 · Esc 取消"))
	case addSecretStageFolder:
		lines = append(lines, shellSelectedRow.Render(targetLine+"▌"), shellLogStyle.Render(typeLine), shellLogStyle.Render(pathLine))
		lines = append(lines,
			shellMutedStyle.Render("新 secrets bundle 用 bundle/<名>；新 project folder 用裸名；已存在的 folder 也可直接写。"),
			shellMutedStyle.Render("Enter 下一步 · Esc 取消"))
	case addSecretStageType:
		lines = append(lines, shellLogStyle.Render(targetLine), shellSelectedRow.Render(typeLine+"▌"), shellLogStyle.Render(pathLine))
		lines = append(lines,
			shellMutedStyle.Render("SSH Key 不在此列：请在 Bitwarden 建 SSH Key Item，Item 名用 .sshkey/<实例>。"),
			shellMutedStyle.Render("tab 轮转类型 · Enter 下一步 · Esc 取消"))
	case addSecretStagePath:
		lines = append(lines, shellLogStyle.Render(targetLine))
		if m.addSecretRemoteMode {
			lines = append(lines, shellLogStyle.Render(typeLine))
		}
		lines = append(lines, shellSelectedRow.Render(pathLine+"▌"))
		lines = append(lines, shellMutedStyle.Render("Enter 下一步 · Esc 取消"))
	case addSecretStageSource:
		lines = append(lines, shellLogStyle.Render(targetLine), shellLogStyle.Render(typeLine), shellLogStyle.Render(pathLine))
		lines = append(lines, shellSelectedRow.Render(sourceLine+"▌"))
		lines = append(lines, shellMutedStyle.Render("tab 切换 temp/路径 · Enter 执行 · Esc 取消"))
	case addSecretStageRunning:
		lines = append(lines, shellLogStyle.Render(targetLine))
		if m.addSecretRemoteMode {
			lines = append(lines, shellLogStyle.Render(typeLine), shellLogStyle.Render(pathLine), shellLogStyle.Render(sourceLine))
		} else {
			lines = append(lines, shellLogStyle.Render(pathLine))
		}
		lines = append(lines, shellWarnStyle.Render("正在写入 Bitwarden..."), shellMutedStyle.Render("Esc 退出等待（后台继续，结果记入日志）"))
	}
	if notice := strings.TrimSpace(m.addSecretNotice); notice != "" {
		lines = append(lines, shellWarnStyle.Render(notice))
	}
	return lines
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
