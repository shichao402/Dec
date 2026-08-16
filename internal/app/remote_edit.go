package app

import (
	"context"
	"fmt"
	"os"
	"sort"
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
}

// RemoteSSHHostsEditSession 描述 SSH Hosts Notes 外部编辑会话。
type RemoteSSHHostsEditSession struct {
	Path          string // 临时文件绝对路径
	ProjectRoot   string
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
		// 无 LocalRoot 的裸 folder：构造仅含 Folder 的 target 供 pull 正文。
		target = secrets.SyncTarget{Folder: folder, Name: folder, Kind: secrets.SyncKindProject}
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
		Target:      secrets.SyncTarget{Folder: folder, Name: target.Name, Kind: target.Kind, LocalRoot: target.LocalRoot, Plane: target.Plane},
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
	if err := ensureBitwardenSession(ctx, reporter, "remote.edit"); err != nil {
		return err
	}
	content, err := os.ReadFile(session.Path)
	if err != nil {
		return fmt.Errorf("读取编辑结果失败: %w", err)
	}
	client := secretsClientFactory()
	req := secrets.PushBundleRequest{
		ProjectRoot: session.ProjectRoot,
		Target:      secrets.SyncTarget{Folder: folder, Name: session.Target.Name, Kind: session.Target.Kind},
		Binding: secrets.BundleBinding{
			DecBundleName:     session.Target.Name,
			SecretsBundleName: folder,
		},
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
		Folder:        folder,
		KeyName:       keyName,
		DecBundleName: strings.TrimSpace(item.DecBundleName),
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
	if err := ensureBitwardenSession(ctx, reporter, "remote.edit"); err != nil {
		return err
	}
	client := secretsClientFactory()
	req := secrets.UpdateSSHKeyHostsRequest{
		Binding: secrets.BundleBinding{SecretsBundleName: session.Folder, DecBundleName: session.DecBundleName},
		Target:  secrets.SyncTarget{Folder: session.Folder},
		KeyName: session.KeyName,
		Hosts:   hosts,
	}
	if err := client.UpdateSSHKeyHosts(ctx, req); err != nil {
		return err
	}
	emit(reporter, EventInfo, "remote.edit",
		fmt.Sprintf("已更新 SSH %q Hosts（%d）", session.KeyName, len(hosts)), nil)

	decName := session.DecBundleName
	if decName == "" {
		decName = strings.TrimPrefix(session.Folder, secrets.BundleFolderPrefix)
	}
	if err := refreshLocalSSHHosts(ctx, client, session.Folder, decName, session.KeyName); err != nil {
		return fmt.Errorf("Bitwarden 已更新，但本地 ~/.ssh 未刷新；请重新 Pull: %w", err)
	}
	return nil
}

// syncTargetFromRemoteItem 还原候选项对应的 SyncTarget。
// 必须带上 item.Plane：machine 平面的 LocalRoot 是 bundles/<name>（相对 ~/.dec/secrets），
// 丢了平面会让 AbsolutePath 误落到 <project>/bundles/...。
func syncTargetFromRemoteItem(item DeleteSelectionItem) (secrets.SyncTarget, error) {
	folder := strings.TrimSpace(item.SecretsBundle)
	localRoot := strings.TrimSpace(item.LocalRoot)
	if folder == "" {
		return secrets.SyncTarget{}, fmt.Errorf("缺少 secrets folder")
	}
	if !secrets.IsMachinePlane(item.Plane) {
		if localRoot == secrets.ProjectSecretsLocalRel || (!strings.HasPrefix(folder, secrets.BundleFolderPrefix) && localRoot == "") {
			return secrets.NewProjectSyncTarget(folder, folder)
		}
	}
	bundleName := strings.TrimSpace(item.DecBundleName)
	if bundleName == "" {
		bundleName = strings.TrimPrefix(folder, secrets.BundleFolderPrefix)
	}
	if localRoot != "" {
		return secrets.SyncTarget{
			Kind:      secrets.SyncKindBundle,
			Name:      bundleName,
			Folder:    folder,
			LocalRoot: localRoot,
			Plane:     item.Plane,
		}, nil
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
	result, err := client.PullBundle(ctx, secrets.PullBundleRequest{
		Target: secrets.SyncTarget{Folder: folder},
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

// SuggestRemoteRegisterFolders 列出 Remote「A 登记」可选 folder：远端全量 + 本机已启用 SyncTarget。
// 不绑死当前 enabled 列表；裸 folder / 其它项目 folder 均可选。
func SuggestRemoteRegisterFolders(ctx context.Context, projectRoot string, reporter Reporter) ([]SecretTargetOption, error) {
	reporter = defaultReporter(reporter)
	opts, err := SuggestSecretTargets(projectRoot)
	if err != nil {
		// 无 .dec/config 时仍允许纯远端 folder 登记。
		opts = nil
		emit(reporter, EventWarn, "remote.register", "读取本机 SyncTarget 失败，仅列远端 folder: "+err.Error(), nil)
	}
	byFolder := make(map[string]SecretTargetOption, len(opts))
	for _, opt := range opts {
		folder := strings.TrimSpace(opt.Folder)
		if folder == "" {
			continue
		}
		byFolder[folder] = opt
	}

	configured, cfgErr := secrets.IsConfigured()
	if cfgErr == nil && configured {
		if !secrets.HasSession() {
			if err := ensureBitwardenSession(ctx, reporter, "remote.register"); err != nil {
				emit(reporter, EventWarn, "remote.register", "未能解锁，仅展示本机 SyncTarget: "+err.Error(), nil)
			}
		}
		if secrets.HasSession() && secrets.HasUserKey() {
			client := secretsClientFactory()
			folders, listErr := client.ListAllFolderNames(ctx)
			if listErr != nil {
				emit(reporter, EventWarn, "remote.register", "枚举远端 folder 失败: "+listErr.Error(), nil)
			} else {
				for _, folder := range folders {
					folder = strings.TrimSpace(folder)
					if folder == "" {
						continue
					}
					if _, ok := byFolder[folder]; ok {
						continue
					}
					label := folder
					if strings.HasPrefix(folder, secrets.BundleFolderPrefix) {
						label = formatSyncTargetLabel(secrets.SyncTarget{
							Kind:   secrets.SyncKindBundle,
							Name:   strings.TrimPrefix(folder, secrets.BundleFolderPrefix),
							Folder: folder,
						})
					} else {
						label = folder + "（裸 folder）"
					}
					byFolder[folder] = SecretTargetOption{
						Kind:   secrets.SyncKindProject,
						Name:   folder,
						Folder: folder,
						Label:  label + " · 仅远端（不种本地）",
					}
				}
			}
		}
	}

	out := make([]SecretTargetOption, 0, len(byFolder))
	for _, opt := range byFolder {
		out = append(out, opt)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Folder < out[j].Folder
	})
	return out, nil
}

// PrepareRemoteNoteRegister 为「向任意 folder 新建 Secure Note」准备空临时文件（不种本地同步根）。
func PrepareRemoteNoteRegister(ctx context.Context, projectRoot, folder, noteRel string, reporter Reporter) (*RemoteNoteEditSession, error) {
	reporter = defaultReporter(reporter)
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return nil, fmt.Errorf("必须指定远端 folder")
	}
	rel, err := secrets.RemoteNoteName(secrets.SyncTarget{Folder: folder}, noteRel)
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
	if _, err := tmp.WriteString(header); err != nil {
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
		Path:        tmp.Name(),
		ProjectRoot: projectRoot,
		Target:      secrets.SyncTarget{Folder: folder, Name: folder, Kind: secrets.SyncKindProject},
		NoteRel:     rel,
		TempFile:    true,
	}, nil
}

// CommitRemoteNoteRegister 与 CommitRemoteNoteEdit 相同：把临时文件推回远端 folder。
func CommitRemoteNoteRegister(ctx context.Context, session RemoteNoteEditSession, reporter Reporter) error {
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
	rel, err := secrets.RemoteNoteName(secrets.SyncTarget{Folder: folder}, noteRel)
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
	client := secretsClientFactory()
	req := secrets.PushBundleRequest{
		ProjectRoot: projectRoot,
		Target:      secrets.SyncTarget{Folder: folder, Name: folder, Kind: secrets.SyncKindProject},
		Binding: secrets.BundleBinding{
			DecBundleName:     folder,
			SecretsBundleName: folder,
		},
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
		Kind:           secrets.SyncKindProject,
		TargetName:     folder,
		Folder:         folder,
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
	result, err := client.PullBundle(ctx, secrets.PullBundleRequest{
		Target:        secrets.SyncTarget{Folder: folder},
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
