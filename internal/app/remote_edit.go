package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/internal/secrets"
)

// RemoteNoteEditSession 描述 Secure Note 外部编辑一次会话的本地文件。
type RemoteNoteEditSession struct {
	Path        string // 供编辑器打开的绝对路径
	ProjectRoot string
	Target      secrets.SyncTarget
	NoteRel     string
	TempFile    bool // true 时 Commit 后删除临时文件
	// CreateFolder 为登记新 folder 的会话：Commit 时 folder 不存在则先建。
	CreateFolder bool
}

// RemoteSSHHostsEditSession 描述 SSH Hosts Notes 外部编辑会话。
type RemoteSSHHostsEditSession struct {
	Path          string // 临时文件绝对路径
	ProjectRoot   string
	Target        secrets.SyncTarget
	KeyName       string
	DecBundleName string
	TempFile      bool
}

// PrepareRemoteNoteEdit 把远端 Secure Note 写入临时文件供外部编辑（不种本地同步根）。
// 推回远端后本地同步根不自动更新；用户需 pull 才拿到最新。
func PrepareRemoteNoteEdit(ctx context.Context, projectRoot string, item DeleteSelectionItem, reporter Reporter) (*RemoteNoteEditSession, error) {
	reporter = defaultReporter(reporter)
	if item.Kind != DeleteKindSecret {
		return nil, fmt.Errorf("只能编辑 Secure Note")
	}
	noteRel := strings.TrimSpace(item.SecretPath)
	if noteRel == "" {
		return nil, fmt.Errorf("缺少 note 路径")
	}
	if strings.TrimSpace(item.SecretsBundle) == "" {
		return nil, fmt.Errorf("缺少远端地址")
	}
	if err := ensureBitwardenSession(ctx, reporter, "remote.edit"); err != nil {
		return nil, err
	}
	target, err := syncTargetFromRemoteItem(item)
	if err != nil {
		return nil, err
	}
	content, fetchErr := fetchRemoteNoteContent(ctx, target, noteRel)
	if fetchErr != nil {
		return nil, fetchErr
	}
	tmp, err := os.CreateTemp("", "dec-remote-note-*.txt")
	if err != nil {
		return nil, err
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	emit(reporter, EventInfo, "remote.edit", fmt.Sprintf("已拉取 %s 到临时文件供编辑（不写本地同步根）", noteRel), nil)
	return &RemoteNoteEditSession{
		Path:        tmp.Name(),
		ProjectRoot: projectRoot,
		Target:      target.Clone(),
		NoteRel:     noteRel,
		TempFile:    true,
	}, nil
}

// CommitRemoteNoteEdit 把临时文件正文推回 Bitwarden（create/update，不删孤儿；不更新本地同步根）。
func CommitRemoteNoteEdit(ctx context.Context, session RemoteNoteEditSession, reporter Reporter) error {
	reporter = defaultReporter(reporter)
	defer func() {
		if session.TempFile && session.Path != "" {
			_ = os.Remove(session.Path)
		}
	}()
	if strings.TrimSpace(session.Path) == "" || strings.TrimSpace(session.NoteRel) == "" {
		return fmt.Errorf("编辑会话无效")
	}
	if strings.TrimSpace(session.Target.Address) == "" {
		return fmt.Errorf("缺少远端地址")
	}
	if err := requireRemoteEditableTarget(session.Target); err != nil {
		return err
	}
	if err := ensureBitwardenSession(ctx, reporter, "remote.edit"); err != nil {
		return err
	}
	content, err := os.ReadFile(session.Path)
	if err != nil {
		return fmt.Errorf("读取编辑结果失败: %w", err)
	}
	client := secretsClientFactory()
	req := secrets.PushBundleRequest{
		ProjectRoot:           session.ProjectRoot,
		Target:                session.Target.Clone(),
		CreateFolderIfMissing: session.CreateFolder,
	}
	result, err := client.PushBundle(ctx, req, []secrets.SecureNote{{
		RelativePath: session.NoteRel,
		Content:      string(content),
	}})
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("已推送 %s（本地同步根未更新，需 pull）", session.NoteRel)
	if result != nil {
		msg = fmt.Sprintf("已推送 %s（created=%d updated=%d；本地同步根未更新，需 pull）", session.NoteRel, result.Created, result.Updated)
	}
	emit(reporter, EventInfo, "remote.edit", msg, nil)
	return nil
}

// PrepareRemoteSSHHostsEdit 把当前 Hosts 写成临时文件供外部编辑。
func PrepareRemoteSSHHostsEdit(ctx context.Context, projectRoot string, item DeleteSelectionItem, reporter Reporter) (*RemoteSSHHostsEditSession, error) {
	reporter = defaultReporter(reporter)
	if item.Kind != DeleteKindSSHKey {
		return nil, fmt.Errorf("只能编辑 SSH Key 的 Hosts")
	}
	keyName := strings.TrimSpace(item.SSHKeyName)
	if keyName == "" || strings.TrimSpace(item.SecretsBundle) == "" {
		return nil, fmt.Errorf("缺少 SSH Key 或远端地址")
	}
	if err := ensureBitwardenSession(ctx, reporter, "remote.edit"); err != nil {
		return nil, err
	}
	target, err := syncTargetFromRemoteItem(item)
	if err != nil {
		return nil, err
	}
	hosts, err := fetchRemoteSSHHosts(ctx, target, keyName)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "dec-ssh-hosts-*.txt")
	if err != nil {
		return nil, err
	}
	body := strings.Join(hosts, "\n")
	if body != "" {
		body += "\n"
	} else {
		body = "# one host per line\n"
	}
	if _, err := tmp.WriteString(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	return &RemoteSSHHostsEditSession{
		Path:          tmp.Name(),
		ProjectRoot:   projectRoot,
		Target:        target.Clone(),
		KeyName:       keyName,
		DecBundleName: target.Name,
		TempFile:      true,
	}, nil
}

// CommitRemoteSSHHostsEdit 解析临时文件、写回 Notes，并刷新本机 SSH config（若私钥已落地）。
func CommitRemoteSSHHostsEdit(ctx context.Context, session RemoteSSHHostsEditSession, reporter Reporter) error {
	reporter = defaultReporter(reporter)
	defer func() {
		if session.TempFile && session.Path != "" {
			_ = os.Remove(session.Path)
		}
	}()
	raw, err := os.ReadFile(session.Path)
	if err != nil {
		return fmt.Errorf("读取 Hosts 编辑结果失败: %w", err)
	}
	hosts, err := parseHostsEditFile(string(raw))
	if err != nil {
		return err
	}
	target := session.Target.Clone()
	if err := requireRemoteEditableTarget(target); err != nil {
		return err
	}
	if err := ensureBitwardenSession(ctx, reporter, "remote.edit"); err != nil {
		return err
	}
	client := secretsClientFactory()
	req := secrets.UpdateSSHKeyHostsRequest{
		Target:  target.Clone(),
		KeyName: session.KeyName,
		Hosts:   hosts,
	}
	if err := client.UpdateSSHKeyHosts(ctx, req); err != nil {
		return err
	}
	emit(reporter, EventInfo, "remote.edit",
		fmt.Sprintf("已更新 SSH %q Hosts（%d）", session.KeyName, len(hosts)), nil)

	if err := refreshLocalSSHHosts(ctx, client, session.ProjectRoot, target, target.Name, session.KeyName); err != nil {
		return fmt.Errorf("Bitwarden 已更新，但本地 ~/.ssh 未刷新；请重新 Pull: %w", err)
	}
	return nil
}

func requireRemoteEditableTarget(target secrets.SyncTarget) error {
	if err := secrets.RequireDeclared(target); err != nil {
		return fmt.Errorf(
			"不能编辑非托管地址 %q：它不属于任何项目，pull 不维护；请先迁移到 <p>/private/<plane>: %w",
			strings.TrimSpace(target.Address), err)
	}
	return nil
}

// syncTargetFromRemoteItem 还原候选项对应的 SyncTarget。
// 项目地址走声明型构造；存量非项目地址只能浏览，写入会被 RequireDeclared 拒绝。
func syncTargetFromRemoteItem(item DeleteSelectionItem) (secrets.SyncTarget, error) {
	address := strings.TrimSpace(item.SecretsBundle)
	if address == "" {
		return secrets.SyncTarget{}, fmt.Errorf("缺少远端地址")
	}
	if scope, err := secrets.ParseRemoteScope(address); err == nil {
		return secrets.NewPSyncTarget(scope.P, scope.Plane)
	}
	return secrets.NewBrowseAddress(address)
}

func fetchRemoteNoteContent(ctx context.Context, target secrets.SyncTarget, noteRel string) (string, error) {
	client := secretsClientFactory()
	result, err := client.PullBundle(ctx, secrets.PullBundleRequest{Target: target})
	if err != nil {
		return "", err
	}
	for _, note := range result.Notes {
		if note.RelativePath == noteRel {
			return note.Content, nil
		}
	}
	return "", fmt.Errorf("远端未找到 note %q（%s）", noteRel, target.Address)
}

func fetchRemoteSSHHosts(ctx context.Context, target secrets.SyncTarget, keyName string) ([]string, error) {
	client := secretsClientFactory()
	result, err := client.PullBundle(ctx, secrets.PullBundleRequest{Target: target})
	if err != nil {
		return nil, err
	}
	for _, key := range result.SSHKeys {
		if key.Name == keyName {
			return append([]string(nil), key.Hosts...), nil
		}
	}
	return nil, fmt.Errorf("远端未找到 SSH Key %q", keyName)
}

func parseHostsEditFile(raw string) ([]string, error) {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return secrets.NormalizeSSHHosts(lines)
}

// PrepareRemoteNoteRegister 为「向某个项目平面新建 Secure Note」准备临时文件（不种本地同步根）。
// initialBody 为可选预填正文（不含注释头）。
func PrepareRemoteNoteRegister(ctx context.Context, projectRoot string, scope secrets.RemoteScope, noteRel, initialBody string, reporter Reporter) (*RemoteNoteEditSession, error) {
	reporter = defaultReporter(reporter)
	target, err := resolveRemoteRegisterTarget(NewWorkspace(WorkspaceProject, projectRoot), scope, false, reporter)
	if err != nil {
		return nil, err
	}
	rel, err := secrets.NormalizeNoteRel(noteRel)
	if err != nil {
		return nil, err
	}
	address := target.Address
	if err := ensureBitwardenSession(ctx, reporter, "remote.register"); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "dec-remote-register-*.txt")
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# Dec Remote 登记 → %s / note %s\n# 保存后写回 Bitwarden；不落本地同步根\n", address, rel)
	body := header
	if strings.TrimSpace(initialBody) != "" {
		body = header + "\n" + initialBody
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
	}
	if _, err := tmp.WriteString(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	emit(reporter, EventInfo, "remote.register",
		fmt.Sprintf("已准备临时文件登记 %s → %s（不写本地同步根）", rel, address), nil)
	return &RemoteNoteEditSession{
		Path:         tmp.Name(),
		ProjectRoot:  projectRoot,
		Target:       target.Clone(),
		NoteRel:      rel,
		TempFile:     true,
		CreateFolder: true,
	}, nil
}

// CommitRemoteNoteRegister 与 CommitRemoteNoteEdit 相同：把临时文件推回远端。
func CommitRemoteNoteRegister(ctx context.Context, session RemoteNoteEditSession, reporter Reporter) error {
	scope, err := session.Target.Scope()
	if err != nil {
		return err
	}
	target, err := resolveRemoteRegisterTarget(
		NewWorkspace(WorkspaceProject, session.ProjectRoot),
		scope,
		true,
		reporter,
	)
	if err != nil {
		return err
	}
	session.Target = target
	return CommitRemoteNoteEdit(ctx, session, reporter)
}

// RegisterRemoteNoteFromPath 从显式本地路径读内容并写到远端（不要求落在 SyncTarget.LocalRoot）。
func RegisterRemoteNoteFromPath(ctx context.Context, projectRoot string, scope secrets.RemoteScope, noteRel, localPath string, reporter Reporter) (*AddSecretResult, error) {
	reporter = defaultReporter(reporter)
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return nil, fmt.Errorf("必须指定本地文件路径")
	}
	target, err := resolveRemoteRegisterTarget(NewWorkspace(WorkspaceProject, projectRoot), scope, false, reporter)
	if err != nil {
		return nil, err
	}
	rel, err := secrets.NormalizeNoteRel(noteRel)
	if err != nil {
		return nil, err
	}
	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		return nil, fmt.Errorf("Bitwarden 未配置，请先在 Settings 页填写连接信息")
	}
	info, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("本地文件不存在: %s", localPath)
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s 是目录：一个 Secure Note 对应一个文件", localPath)
	}
	content, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("读取本地文件失败: %w", err)
	}
	if err := ensureBitwardenSession(ctx, reporter, "remote.register"); err != nil {
		return nil, err
	}
	target, err = resolveRemoteRegisterTarget(NewWorkspace(WorkspaceProject, projectRoot), scope, true, reporter)
	if err != nil {
		return nil, err
	}
	client := secretsClientFactory()
	req := secrets.PushBundleRequest{
		ProjectRoot:           projectRoot,
		Target:                target.Clone(),
		CreateFolderIfMissing: true,
	}
	result, err := client.PushBundle(ctx, req, []secrets.SecureNote{{
		RelativePath: rel,
		Content:      string(content),
	}})
	if err != nil {
		return nil, err
	}
	msg := fmt.Sprintf("已登记 %s → %s（不写本地同步根）", rel, target.Address)
	if result != nil {
		msg = fmt.Sprintf("已登记 %s → %s（created=%d updated=%d；不写本地同步根）",
			rel, target.Address, result.Created, result.Updated)
	}
	emit(reporter, EventInfo, "remote.register", msg, nil)
	return &AddSecretResult{
		TargetName:     target.Name,
		Address:        target.Address,
		NoteRelPath:    rel,
		ProjectRelPath: localPath,
		LandingPath:    localPath,
	}, nil
}

func refreshLocalSSHHosts(ctx context.Context, client secrets.Client, projectRoot string, target secrets.SyncTarget, decBundleName, keyName string) error {
	var exists bool
	var err error
	if secrets.IsMachinePlane(target.Plane) {
		exists, err = secrets.LocalSSHKeyExists(decBundleName, keyName)
	} else {
		exists, err = secrets.InspectProjectSSHKeyLanding(projectRoot, decBundleName, keyName)
	}
	if err != nil || !exists {
		return err
	}
	result, err := client.PullBundle(ctx, secrets.PullBundleRequest{
		ProjectRoot: projectRoot,
		Target:      target.Clone(),
	})
	if err != nil {
		return err
	}
	// 用同一归属下全部 Key 做冲突检测，避免 Remote 编辑绕过跨 Key Host 冲突。
	all := append([]secrets.SSHKeyItem(nil), result.SSHKeys...)
	if len(all) == 0 {
		return fmt.Errorf("Bitwarden 中该 bundle 没有 SSH Key")
	}
	found := false
	for _, key := range all {
		if key.Name == keyName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("Bitwarden 中找不到 SSH Key %q", keyName)
	}
	landings, err := secrets.PrepareSSHKeyLandings(decBundleName, all)
	if err != nil {
		return err
	}
	// 仅写回已落地私钥对应的条目，避免覆盖其他 Key 文件；冲突已在 Prepare 检出。
	var one []secrets.SSHKeyLanding
	for _, landing := range landings {
		if landing.Name == keyName {
			one = append(one, landing)
			break
		}
	}
	if len(one) == 0 {
		return fmt.Errorf("无法为 SSH Key %q 生成本地 ~/.ssh 条目", keyName)
	}
	if secrets.IsMachinePlane(target.Plane) {
		return secrets.WriteSSHKeyLandings(one)
	}
	return secrets.WriteProjectSSHKeyLandings(projectRoot, one)
}
