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
	Folder        string
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
	folder := strings.TrimSpace(item.SecretsBundle)
	if folder == "" {
		return nil, fmt.Errorf("缺少 secrets folder")
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
	folder := strings.TrimSpace(session.Target.Folder)
	if folder == "" {
		return fmt.Errorf("缺少 secrets folder")
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
	target := session.Target.Clone()
	bindingName := target.Name
	if target.Kind == secrets.SyncKindProject {
		bindingName = secrets.ProjectSecretsDecBundleName
	}
	req := secrets.PushBundleRequest{
		ProjectRoot: session.ProjectRoot,
		Target:      target,
		Binding: secrets.BundleBinding{
			DecBundleName:     bindingName,
			SecretsBundleName: folder,
		},
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
	folder := strings.TrimSpace(item.SecretsBundle)
	if keyName == "" || folder == "" {
		return nil, fmt.Errorf("缺少 SSH Key 或 folder")
	}
	if err := ensureBitwardenSession(ctx, reporter, "remote.edit"); err != nil {
		return nil, err
	}
	hosts, err := fetchRemoteSSHHosts(ctx, folder, keyName)
	if err != nil {
		return nil, err
	}
	target, err := syncTargetFromRemoteItem(item)
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
		Folder:        target.Folder,
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
	if strings.TrimSpace(target.Folder) == "" && strings.TrimSpace(session.Folder) != "" {
		target, err = secrets.NewBrowseFolder(session.Folder)
		if err != nil {
			return err
		}
	}
	if err := requireRemoteEditableTarget(target); err != nil {
		return err
	}
	if err := ensureBitwardenSession(ctx, reporter, "remote.edit"); err != nil {
		return err
	}
	client := secretsClientFactory()
	req := secrets.UpdateSSHKeyHostsRequest{
		Binding: secrets.BundleBinding{SecretsBundleName: target.Folder, DecBundleName: target.Name},
		Target:  target.Clone(),
		KeyName: session.KeyName,
		Hosts:   hosts,
	}
	if err := client.UpdateSSHKeyHosts(ctx, req); err != nil {
		return err
	}
	emit(reporter, EventInfo, "remote.edit",
		fmt.Sprintf("已更新 SSH %q Hosts（%d）", session.KeyName, len(hosts)), nil)

	decName := target.Name
	if decName == "" {
		decName = strings.TrimPrefix(target.Folder, secrets.BundleFolderPrefix)
	}
	if err := refreshLocalSSHHosts(ctx, client, target.Folder, decName, session.KeyName); err != nil {
		return fmt.Errorf("Bitwarden 已更新，但本地 ~/.ssh 未刷新；请重新 Pull: %w", err)
	}
	return nil
}

func requireRemoteEditableTarget(target secrets.SyncTarget) error {
	if err := secrets.RequireDeclared(target); err != nil {
		folder := strings.TrimSpace(target.Folder)
		return fmt.Errorf(
			"不能编辑非托管 folder %q：它不属于任何已声明归属，pull 不维护；请先迁移到 bundle/<名>: %w",
			folder, err)
	}
	return nil
}

// syncTargetFromRemoteItem 还原候选项对应的 SyncTarget。
// 必须带上 item.Plane：machine 平面的 LocalRoot 是 bundles/<name>（相对 ~/.dec/secrets），
// 丢了平面会让 AbsolutePath 误落到 <project>/bundles/...。
func syncTargetFromRemoteItem(item DeleteSelectionItem) (secrets.SyncTarget, error) {
	folder := strings.TrimSpace(item.SecretsBundle)
	if folder == "" {
		return secrets.SyncTarget{}, fmt.Errorf("缺少 secrets folder")
	}
	if !strings.HasPrefix(folder, secrets.BundleFolderPrefix) {
		// ADR 0014：裸 folder 一律按浏览（非声明）处理；写入由 RequireDeclared 拒绝。
		return secrets.NewBrowseFolder(folder)
	}
	bundleName := strings.TrimSpace(item.DecBundleName)
	if bundleName == "" {
		bundleName = strings.TrimPrefix(folder, secrets.BundleFolderPrefix)
	}
	if secrets.IsMachinePlane(item.Plane) {
		return secrets.NewMachineBundleSyncTarget(bundleName, folder)
	}
	return secrets.NewBundleSyncTarget(bundleName, folder)
}

func fetchRemoteNoteContent(ctx context.Context, target secrets.SyncTarget, noteRel string) (string, error) {
	client := secretsClientFactory()
	result, err := client.PullBundle(ctx, secrets.PullBundleRequest{
		Target:        target,
		DecBundleName: target.Name,
		Binding: secrets.BundleBinding{
			DecBundleName:     target.Name,
			SecretsBundleName: target.Folder,
		},
	})
	if err != nil {
		return "", err
	}
	for _, note := range result.Notes {
		if note.RelativePath == noteRel {
			return note.Content, nil
		}
	}
	return "", fmt.Errorf("远端未找到 note %q（folder %s）", noteRel, target.Folder)
}

func fetchRemoteSSHHosts(ctx context.Context, folder, keyName string) ([]string, error) {
	client := secretsClientFactory()
	target, err := secrets.NewBrowseFolder(folder)
	if err != nil {
		return nil, err
	}
	result, err := client.PullBundle(ctx, secrets.PullBundleRequest{
		Target: target,
		Binding: secrets.BundleBinding{
			SecretsBundleName: folder,
		},
	})
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

// PrepareRemoteNoteRegister 为「向任意 folder 新建 Secure Note」准备临时文件（不种本地同步根）。
// initialBody 为可选预填正文（不含注释头）。
func PrepareRemoteNoteRegister(ctx context.Context, projectRoot, folder, noteRel, initialBody string, reporter Reporter) (*RemoteNoteEditSession, error) {
	reporter = defaultReporter(reporter)
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return nil, fmt.Errorf("必须指定远端 folder")
	}
	target, err := resolveRemoteRegisterTarget(NewWorkspace(WorkspaceProject, projectRoot), folder, false, reporter)
	if err != nil {
		return nil, err
	}
	rel, err := secrets.RemoteNoteName(target, noteRel)
	if err != nil {
		return nil, err
	}
	if err := ensureBitwardenSession(ctx, reporter, "remote.register"); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "dec-remote-register-*.txt")
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# Dec Remote 登记 → folder %s / note %s\n# 保存后写回 Bitwarden；不落本地同步根\n", folder, rel)
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
		fmt.Sprintf("已准备临时文件登记 %s → %s（不写本地同步根）", rel, folder), nil)
	return &RemoteNoteEditSession{
		Path:         tmp.Name(),
		ProjectRoot:  projectRoot,
		Target:       target.Clone(),
		NoteRel:      rel,
		TempFile:     true,
		CreateFolder: true,
	}, nil
}

// CommitRemoteNoteRegister 与 CommitRemoteNoteEdit 相同：把临时文件推回远端 folder。
func CommitRemoteNoteRegister(ctx context.Context, session RemoteNoteEditSession, reporter Reporter) error {
	target, err := resolveRemoteRegisterTarget(
		NewWorkspace(WorkspaceProject, session.ProjectRoot),
		session.Target.Folder,
		true,
		reporter,
	)
	if err != nil {
		return err
	}
	session.Target = target
	return CommitRemoteNoteEdit(ctx, session, reporter)
}

// RegisterRemoteNoteFromPath 从显式本地路径读内容并写到任意远端 folder（不要求落在 SyncTarget.LocalRoot）。
func RegisterRemoteNoteFromPath(ctx context.Context, projectRoot, folder, noteRel, localPath string, reporter Reporter) (*AddSecretResult, error) {
	reporter = defaultReporter(reporter)
	folder = strings.TrimSpace(folder)
	localPath = strings.TrimSpace(localPath)
	if folder == "" {
		return nil, fmt.Errorf("必须指定远端 folder")
	}
	if localPath == "" {
		return nil, fmt.Errorf("必须指定本地文件路径")
	}
	target, err := resolveRemoteRegisterTarget(NewWorkspace(WorkspaceProject, projectRoot), folder, false, reporter)
	if err != nil {
		return nil, err
	}
	rel, err := secrets.RemoteNoteName(target, noteRel)
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
	target, err = resolveRemoteRegisterTarget(NewWorkspace(WorkspaceProject, projectRoot), folder, true, reporter)
	if err != nil {
		return nil, err
	}
	client := secretsClientFactory()
	req := secrets.PushBundleRequest{
		ProjectRoot: projectRoot,
		Target:      target.Clone(),
		Binding: secrets.BundleBinding{
			DecBundleName:     target.Name,
			SecretsBundleName: target.Folder,
		},
		CreateFolderIfMissing: true,
	}
	result, err := client.PushBundle(ctx, req, []secrets.SecureNote{{
		RelativePath: rel,
		Content:      string(content),
	}})
	if err != nil {
		return nil, err
	}
	msg := fmt.Sprintf("已登记 %s → folder %q（不写本地同步根）", rel, folder)
	if result != nil {
		msg = fmt.Sprintf("已登记 %s → folder %q（created=%d updated=%d；不写本地同步根）",
			rel, folder, result.Created, result.Updated)
	}
	emit(reporter, EventInfo, "remote.register", msg, nil)
	return &AddSecretResult{
		Kind:           target.Kind,
		TargetName:     target.Name,
		Folder:         target.Folder,
		NoteRelPath:    rel,
		ProjectRelPath: localPath,
		LandingPath:    localPath,
	}, nil
}

func refreshLocalSSHHosts(ctx context.Context, client secrets.Client, folder, decBundleName, keyName string) error {
	exists, err := secrets.LocalSSHKeyExists(decBundleName, keyName)
	if err != nil || !exists {
		return err
	}
	target, err := secrets.NewBrowseFolder(folder)
	if err != nil {
		return err
	}
	result, err := client.PullBundle(ctx, secrets.PullBundleRequest{
		Target:        target,
		DecBundleName: decBundleName,
		Binding: secrets.BundleBinding{
			DecBundleName:     decBundleName,
			SecretsBundleName: folder,
		},
	})
	if err != nil {
		return err
	}
	// 用 folder 内全部 Key 做冲突检测，避免 Remote 编辑绕过跨 Key Host 冲突。
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
	return secrets.WriteSSHKeyLandings(one)
}
