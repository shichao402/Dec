package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

// RemoteRegisterInput 是 Processor 驱动的远端登记输入。
type RemoteRegisterInput struct {
	ProjectRoot  string
	Plane        WorkspacePlane
	Folder       string
	TypeID       secrets.SecretTypeID
	Name         string
	SourceMode   secrets.SourceMode
	LocalPath    string // path / picker 归一后的路径
	InitialBody  string // temp 预填（可空；默认取 Processor.Template）
	CreateFolder bool
}

// RemoteRegisterSession 是统一登记会话。
// NeedsEditor=true 时 TUI 打开 EditorPath；关闭后 Commit 读正文。
// NeedsEditor=false 时素材已就绪，可直接 Commit。
type RemoteRegisterSession struct {
	NeedsEditor  bool
	EditorPath   string
	TempFile     bool
	ProjectRoot  string
	Plane        WorkspacePlane
	Folder       string
	Target       secrets.SyncTarget
	TypeID       secrets.SecretTypeID
	Name         string
	CreateFolder bool
	NoteContent  string
	SSHKey       *secrets.SSHKeyItem
}

// PrepareRemoteRegister 按 Processor 准备登记素材。
func PrepareRemoteRegister(ctx context.Context, in RemoteRegisterInput, reporter Reporter) (*RemoteRegisterSession, error) {
	reporter = defaultReporter(reporter)
	proc, err := resolveRegisterProcessor(in)
	if err != nil {
		return nil, err
	}
	name, err := proc.NormalizeName(in.Name)
	if err != nil {
		return nil, err
	}
	folder := strings.TrimSpace(in.Folder)
	if folder == "" {
		return nil, fmt.Errorf("必须指定远端 folder")
	}
	workspace := NewWorkspace(in.Plane, in.ProjectRoot)
	target, err := resolveRemoteRegisterTarget(workspace, folder, false, reporter)
	if err != nil {
		return nil, err
	}
	mode := in.SourceMode
	if mode == "" {
		mode = proc.DefaultSource
	}
	if !proc.HasSourceMode(mode) {
		return nil, fmt.Errorf("类型 %s 不支持来源 %s", proc.Label, mode)
	}
	if mode == secrets.SourcePicker {
		return nil, fmt.Errorf("picker 须先选中文件并归一为 path")
	}

	if err := ensureBitwardenSession(ctx, reporter, "remote.register"); err != nil {
		return nil, err
	}

	sess := &RemoteRegisterSession{
		ProjectRoot:  in.ProjectRoot,
		Plane:        workspace.EffectivePlane(),
		Folder:       folder,
		Target:       target.Clone(),
		TypeID:       proc.ID,
		Name:         name,
		CreateFolder: in.CreateFolder,
	}

	switch {
	case proc.WritesSSHItem():
		mat, prepErr := prepareSSHMaterial(mode, in.LocalPath, name)
		if prepErr != nil {
			return nil, prepErr
		}
		sess.SSHKey = &secrets.SSHKeyItem{
			Name:           name,
			PrivateKey:     mat.PrivateKey,
			PublicKey:      mat.PublicKey,
			KeyFingerprint: mat.KeyFingerprint,
		}
		emit(reporter, EventInfo, "remote.register",
			fmt.Sprintf("已准备 SSH Key %s → %s（来源 %s）", name, folder, mode), nil)
		return sess, nil

	case proc.WritesSecureNote():
		switch mode {
		case secrets.SourceTemp:
			body := strings.TrimSpace(in.InitialBody)
			if body == "" {
				body = proc.Template
			}
			tmp, tmpErr := os.CreateTemp("", "dec-remote-register-*.txt")
			if tmpErr != nil {
				return nil, tmpErr
			}
			header := fmt.Sprintf("# Dec Remote 登记 → folder %s / %s %s\n# 保存后写回 Bitwarden；不落本地同步根\n",
				folder, proc.ID, name)
			content := header
			if body != "" {
				content = header + "\n" + body
				if !strings.HasSuffix(content, "\n") {
					content += "\n"
				}
			}
			if _, wErr := tmp.WriteString(content); wErr != nil {
				_ = tmp.Close()
				_ = os.Remove(tmp.Name())
				return nil, wErr
			}
			if cErr := tmp.Close(); cErr != nil {
				_ = os.Remove(tmp.Name())
				return nil, cErr
			}
			sess.NeedsEditor = true
			sess.EditorPath = tmp.Name()
			sess.TempFile = true
			emit(reporter, EventInfo, "remote.register",
				fmt.Sprintf("已准备临时文件登记 %s → %s（不写本地同步根）", name, folder), nil)
			return sess, nil
		case secrets.SourcePath:
			content, readErr := readLocalSecretFile(in.LocalPath)
			if readErr != nil {
				return nil, readErr
			}
			sess.NoteContent = content
			emit(reporter, EventInfo, "remote.register",
				fmt.Sprintf("已读取本地文件登记 %s → %s", name, folder), nil)
			return sess, nil
		default:
			return nil, fmt.Errorf("未知来源模式 %s", mode)
		}
	default:
		return nil, fmt.Errorf("类型 %s 未声明写入器", proc.Label)
	}
}

// CommitRemoteRegister 把会话素材写入 Bitwarden（按 Processor 选择 Writer）。
func CommitRemoteRegister(ctx context.Context, sess RemoteRegisterSession, reporter Reporter) (*AddSecretResult, error) {
	reporter = defaultReporter(reporter)
	defer func() {
		if sess.TempFile && sess.EditorPath != "" {
			_ = os.Remove(sess.EditorPath)
		}
	}()

	proc, ok := secrets.LookupProcessor(string(sess.TypeID))
	if !ok {
		return nil, fmt.Errorf("未知类型 %q", sess.TypeID)
	}
	folder := strings.TrimSpace(sess.Folder)
	name := strings.TrimSpace(sess.Name)
	if folder == "" || name == "" {
		return nil, fmt.Errorf("登记会话无效")
	}
	workspace := NewWorkspace(sess.Plane, sess.ProjectRoot)
	target, err := resolveRemoteRegisterTarget(workspace, folder, true, reporter)
	if err != nil {
		return nil, err
	}
	if err := ensureBitwardenSession(ctx, reporter, "remote.register"); err != nil {
		return nil, err
	}
	client := secretsClientFactory()
	decName := target.Name
	bindingName := target.Name
	if target.Kind == secrets.SyncKindProject {
		bindingName = secrets.ProjectSecretsDecBundleName
	}
	binding := secrets.BundleBinding{DecBundleName: bindingName, SecretsBundleName: target.Folder}

	switch {
	case proc.WritesSSHItem():
		key := sess.SSHKey
		if key == nil {
			return nil, fmt.Errorf("SSH Key 素材缺失")
		}
		key.Name = name
		if err := client.CreateSSHKey(ctx, secrets.CreateSSHKeyRequest{
			Binding:               binding,
			Target:                target,
			Key:                   *key,
			CreateFolderIfMissing: sess.CreateFolder,
		}); err != nil {
			return nil, err
		}
		landed, landErr := landRegisteredSSHKey(decName, *key)
		msg := fmt.Sprintf("已登记 SSH Key %s → folder %q", name, folder)
		if landed {
			msg += "；已写入 ~/.ssh"
		} else if landErr != nil {
			msg += "；本机落地跳过: " + landErr.Error()
		}
		emit(reporter, EventInfo, "remote.register", msg, nil)
		return &AddSecretResult{
			Kind:           target.Kind,
			TargetName:     target.Name,
			Folder:         target.Folder,
			NoteRelPath:    name,
			ProjectRelPath: "",
			LandingPath:    "",
		}, nil

	case proc.WritesSecureNote():
		content := sess.NoteContent
		if sess.NeedsEditor || (content == "" && sess.EditorPath != "") {
			raw, err := os.ReadFile(sess.EditorPath)
			if err != nil {
				return nil, fmt.Errorf("读取编辑结果失败: %w", err)
			}
			content = string(raw)
		}
		result, err := client.PushBundle(ctx, secrets.PushBundleRequest{
			ProjectRoot:           sess.ProjectRoot,
			Target:                target,
			Binding:               binding,
			CreateFolderIfMissing: sess.CreateFolder,
		}, []secrets.SecureNote{{RelativePath: name, Content: content}})
		if err != nil {
			return nil, err
		}
		msg := fmt.Sprintf("已登记 %s → folder %q（不写本地同步根）", name, folder)
		if result != nil {
			msg = fmt.Sprintf("已登记 %s → folder %q（created=%d updated=%d；不写本地同步根）",
				name, folder, result.Created, result.Updated)
		}
		emit(reporter, EventInfo, "remote.register", msg, nil)
		return &AddSecretResult{
			Kind:        target.Kind,
			TargetName:  target.Name,
			Folder:      target.Folder,
			NoteRelPath: name,
		}, nil
	default:
		return nil, fmt.Errorf("类型 %s 未声明写入器", proc.Label)
	}
}

// ValidateRemoteRegisterFolder 校验 Remote 登记 folder 是否有合法声明来源，但不写 vault。
func ValidateRemoteRegisterFolder(workspace Workspace, folder string) error {
	_, err := resolveRemoteRegisterTarget(workspace, folder, false, nil)
	return err
}

// resolveRemoteRegisterTarget 只允许 bundle/<名>（ADR 0014）。
// bundle 在真正提交时补齐缺失的 manifest，使 Bitwarden 写入立即有声明归属。
func resolveRemoteRegisterTarget(workspace Workspace, folder string, ensureBundle bool, reporter Reporter) (secrets.SyncTarget, error) {
	folder = strings.Trim(strings.TrimSpace(strings.ReplaceAll(folder, "\\", "/")), "/")
	if folder == "" {
		return secrets.SyncTarget{}, fmt.Errorf("必须指定远端 folder")
	}
	if strings.HasPrefix(folder, secrets.BundleFolderPrefix) {
		name := strings.TrimSpace(strings.TrimPrefix(folder, secrets.BundleFolderPrefix))
		if err := validateRemoteOwnerName("bundle", name); err != nil {
			return secrets.SyncTarget{}, err
		}
		plane := workspace.EffectivePlane()
		if ensureBundle {
			resolvedPlane, err := ensureRemoteBundleManifest(name, plane, reporter)
			if err != nil {
				return secrets.SyncTarget{}, err
			}
			plane = resolvedPlane
		}
		if plane == WorkspaceUser {
			return secrets.NewMachineBundleSyncTarget(name, folder)
		}
		return secrets.NewBundleSyncTarget(name, folder)
	}

	return secrets.SyncTarget{}, fmt.Errorf(
		"folder %q 不是合法写入归属；Remote 登记只接受 %q（ADR 0014）",
		folder, secrets.DefaultBundleFolder(folder))
}

func validateRemoteOwnerName(kind, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%s 名不能为空", kind)
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("非法 %s 名 %q", kind, name)
	}
	return nil
}

func ensureRemoteBundleManifest(name string, fallbackPlane WorkspacePlane, reporter Reporter) (WorkspacePlane, error) {
	reporter = defaultReporter(reporter)
	resolvedPlane := fallbackPlane
	if resolvedPlane != WorkspaceUser {
		resolvedPlane = WorkspaceProject
	}
	created := false
	err := withAppWriteRepo(func(tx *repo.Transaction) error {
		manifestRel := types.VaultBundleManifestPath(name)
		manifestAbs := filepath.Join(tx.WorkDir(), filepath.FromSlash(manifestRel))
		data, readErr := os.ReadFile(manifestAbs)
		switch {
		case readErr == nil:
			b, _, parseErr := yamlBundleNameScope(data)
			if parseErr != nil {
				return fmt.Errorf("解析 vault bundle %q 失败: %w", name, parseErr)
			}
			if b.Scope == types.BundleScopeUser {
				resolvedPlane = WorkspaceUser
			} else {
				resolvedPlane = WorkspaceProject
			}
			return nil
		case !os.IsNotExist(readErr):
			return fmt.Errorf("检查 vault bundle %q 失败: %w", name, readErr)
		}

		if err := os.MkdirAll(filepath.Dir(manifestAbs), 0o755); err != nil {
			return err
		}
		scope := types.BundleScopeProject
		if resolvedPlane == WorkspaceUser {
			scope = types.BundleScopeUser
		}
		body, err := yaml.Marshal(types.Bundle{
			Name:        name,
			Scope:       scope,
			Description: "Remote 登记自动创建的占位（ADR 0013）",
			Members:     []string{},
		})
		if err != nil {
			return err
		}
		header := "# Dec bundle（Remote 登记时自动创建的占位；可后续补充 members）\n"
		if err := os.WriteFile(manifestAbs, append([]byte(header), body...), 0o644); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", manifestRel, err)
		}
		if _, err := tx.CommitAndPush("chore(bundles): add remote registration placeholder " + name); err != nil {
			return fmt.Errorf("推送 bundle %q 占位失败: %w", name, err)
		}
		created = true
		return nil
	})
	if err != nil {
		return WorkspaceProject, err
	}
	if created {
		emit(reporter, EventInfo, "remote.register",
			fmt.Sprintf("已在 vault 创建 bundle %q 占位声明", name), nil)
	}
	_ = secrets.RememberSecretBundles([]string{name})
	return resolvedPlane, nil
}

func resolveRegisterProcessor(in RemoteRegisterInput) (secrets.Processor, error) {
	proc, ok := secrets.LookupProcessor(string(in.TypeID))
	if !ok {
		return secrets.Processor{}, fmt.Errorf("未知类型 %q", in.TypeID)
	}
	return proc, nil
}

func prepareSSHMaterial(mode secrets.SourceMode, localPath, name string) (secrets.SSHKeyMaterial, error) {
	switch mode {
	case secrets.SourceGenerate:
		return secrets.GenerateSSHKeyMaterial(name)
	case secrets.SourcePath:
		return secrets.LoadSSHKeyMaterialFromPrivatePath(localPath)
	default:
		return secrets.SSHKeyMaterial{}, fmt.Errorf("SSH Key 不支持来源 %s", mode)
	}
}

func readLocalSecretFile(localPath string) (string, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return "", fmt.Errorf("必须指定本地文件路径")
	}
	info, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("本地文件不存在: %s", localPath)
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s 是目录：一个 Secure Note 对应一个文件", localPath)
	}
	content, err := os.ReadFile(localPath)
	if err != nil {
		return "", fmt.Errorf("读取本地文件失败: %w", err)
	}
	return string(content), nil
}

// landRegisteredSSHKey 尝试把新 Key 落到 ~/.ssh；已存在同名文件则跳过（不覆盖）。
func landRegisteredSSHKey(decBundleName string, key secrets.SSHKeyItem) (bool, error) {
	exists, err := secrets.LocalSSHKeyExists(decBundleName, key.Name)
	if err != nil {
		return false, err
	}
	if exists {
		return false, fmt.Errorf("本地已有同名密钥文件，未覆盖；可稍后 Pull")
	}
	landings, err := secrets.PrepareSSHKeyLandings(decBundleName, []secrets.SSHKeyItem{key})
	if err != nil {
		return false, err
	}
	if err := secrets.WriteSSHKeyLandings(landings); err != nil {
		return false, err
	}
	return true, nil
}
