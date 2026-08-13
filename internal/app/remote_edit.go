package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/secrets"
)

// RemoteNoteEditSession 描述 Secure Note 外部编辑一次会话的本地文件。
type RemoteNoteEditSession struct {
	Path        string // 供编辑器打开的绝对路径
	ProjectRoot string
	Target      secrets.SyncTarget
	NoteRel     string
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

// PrepareRemoteNoteEdit 确保本地同步根上有该 note 文件（缺则从远端拉正文），返回编辑路径。
func PrepareRemoteNoteEdit(ctx context.Context, projectRoot string, item DeleteSelectionItem, reporter Reporter) (*RemoteNoteEditSession, error) {
	reporter = defaultReporter(reporter)
	if item.Kind != DeleteKindSecret {
		return nil, fmt.Errorf("只能编辑 Secure Note")
	}
	noteRel := strings.TrimSpace(item.SecretPath)
	if noteRel == "" {
		return nil, fmt.Errorf("缺少 note 路径")
	}
	target, err := syncTargetFromRemoteItem(item)
	if err != nil {
		return nil, err
	}
	abs, err := secrets.AbsolutePath(projectRoot, target, noteRel)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := ensureBitwardenSession(ctx, reporter, "remote.edit"); err != nil {
			return nil, err
		}
		content, fetchErr := fetchRemoteNoteContent(ctx, target, noteRel)
		if fetchErr != nil {
			return nil, fetchErr
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			return nil, err
		}
		emit(reporter, EventInfo, "remote.edit", fmt.Sprintf("已从远端拉取 %s 到本地供编辑", noteRel), nil)
	}
	return &RemoteNoteEditSession{
		Path:        abs,
		ProjectRoot: projectRoot,
		Target:      target,
		NoteRel:     noteRel,
	}, nil
}

// CommitRemoteNoteEdit 把本地 note 推回 Bitwarden（create/update，不删孤儿）。
func CommitRemoteNoteEdit(ctx context.Context, session RemoteNoteEditSession, reporter Reporter) error {
	reporter = defaultReporter(reporter)
	if strings.TrimSpace(session.Path) == "" || strings.TrimSpace(session.NoteRel) == "" {
		return fmt.Errorf("编辑会话无效")
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
		Target:      session.Target,
		Binding: secrets.BundleBinding{
			DecBundleName:     session.Target.Name,
			SecretsBundleName: session.Target.Folder,
		},
	}
	result, err := client.PushBundle(ctx, req, []secrets.SecureNote{{
		RelativePath: session.NoteRel,
		Content:      string(content),
	}})
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("已推送 %s", session.NoteRel)
	if result != nil {
		msg = fmt.Sprintf("已推送 %s（created=%d updated=%d）", session.NoteRel, result.Created, result.Updated)
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
	hosts := parseHostsEditFile(string(raw))
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
		emit(reporter, EventWarn, "remote.edit", "远端已更新，刷新本地 SSH config 失败: "+err.Error(), nil)
	}
	return nil
}

func syncTargetFromRemoteItem(item DeleteSelectionItem) (secrets.SyncTarget, error) {
	folder := strings.TrimSpace(item.SecretsBundle)
	localRoot := strings.TrimSpace(item.LocalRoot)
	if folder == "" {
		return secrets.SyncTarget{}, fmt.Errorf("缺少 secrets folder")
	}
	if localRoot == secrets.ProjectSecretsLocalRel || (!strings.HasPrefix(folder, secrets.BundleFolderPrefix) && localRoot == "") {
		name := folder
		return secrets.NewProjectSyncTarget(name, folder)
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
		}, nil
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

func parseHostsEditFile(raw string) []string {
	var hosts []string
	seen := make(map[string]struct{})
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		hosts = append(hosts, line)
	}
	return hosts
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
	var one []secrets.SSHKeyItem
	for _, key := range result.SSHKeys {
		if key.Name == keyName {
			one = append(one, key)
			break
		}
	}
	if len(one) == 0 {
		return fmt.Errorf("刷新失败：远端缺少 %s", keyName)
	}
	landings, err := secrets.PrepareSSHKeyLandings(decBundleName, one)
	if err != nil {
		return err
	}
	return secrets.WriteSSHKeyLandings(landings)
}
