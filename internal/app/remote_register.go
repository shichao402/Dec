package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
)

// RemoteRegisterInput 是 Processor 驱动的远端登记输入。
// Scope 是结构化的远端归属（项目 + 平面）；调用方不拼 Bitwarden folder 名。
type RemoteRegisterInput struct {
	ProjectRoot  string
	Plane        WorkspacePlane
	Scope        secrets.RemoteScope
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
	Scope        secrets.RemoteScope
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
	workspace := NewWorkspace(in.Plane, in.ProjectRoot)
	target, err := resolveRemoteRegisterTarget(workspace, in.Scope, false, reporter)
	if err != nil {
		return nil, err
	}
	address := target.Address
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
		Scope:        in.Scope,
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
			fmt.Sprintf("已准备 SSH Key %s → %s（来源 %s）", name, address, mode), nil)
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
			header := fmt.Sprintf("# Dec Remote 登记 → %s / %s %s\n# 保存后写回 Bitwarden；不落本地同步根\n",
				address, proc.ID, name)
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
				fmt.Sprintf("已准备临时文件登记 %s → %s（不写本地同步根）", name, address), nil)
			return sess, nil
		case secrets.SourcePath:
			content, readErr := readLocalSecretFile(in.LocalPath)
			if readErr != nil {
				return nil, readErr
			}
			sess.NoteContent = content
			emit(reporter, EventInfo, "remote.register",
				fmt.Sprintf("已读取本地文件登记 %s → %s", name, address), nil)
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
	name := strings.TrimSpace(sess.Name)
	if name == "" {
		return nil, fmt.Errorf("登记会话无效")
	}
	workspace := NewWorkspace(sess.Plane, sess.ProjectRoot)
	target, err := resolveRemoteRegisterTarget(workspace, sess.Scope, true, reporter)
	if err != nil {
		return nil, err
	}
	if err := ensureBitwardenSession(ctx, reporter, "remote.register"); err != nil {
		return nil, err
	}
	client := secretsClientFactory()
	decName := target.Name
	address := target.Address

	switch {
	case proc.WritesSSHItem():
		key := sess.SSHKey
		if key == nil {
			return nil, fmt.Errorf("SSH Key 素材缺失")
		}
		key.Name = name
		if err := client.CreateSSHKey(ctx, secrets.CreateSSHKeyRequest{
			Target:                target,
			Key:                   *key,
			CreateFolderIfMissing: sess.CreateFolder,
		}); err != nil {
			return nil, err
		}
		landed, landErr := landRegisteredSSHKey(workspace, target, decName, *key)
		msg := fmt.Sprintf("已登记 SSH Key %s → %s", name, address)
		if landed {
			msg += "；已写入 ~/.ssh"
		} else if landErr != nil {
			msg += "；本机落地跳过: " + landErr.Error()
		}
		emit(reporter, EventInfo, "remote.register", msg, nil)
		return &AddSecretResult{
			TargetName:     target.Name,
			Address:        address,
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
			CreateFolderIfMissing: sess.CreateFolder,
		}, []secrets.SecureNote{{RelativePath: name, Content: content}})
		if err != nil {
			return nil, err
		}
		msg := fmt.Sprintf("已登记 %s → %s（不写本地同步根）", name, address)
		if result != nil {
			msg = fmt.Sprintf("已登记 %s → %s（created=%d updated=%d；不写本地同步根）",
				name, address, result.Created, result.Updated)
		}
		emit(reporter, EventInfo, "remote.register", msg, nil)
		return &AddSecretResult{
			TargetName:  target.Name,
			Address:     address,
			NoteRelPath: name,
		}, nil
	default:
		return nil, fmt.Errorf("类型 %s 未声明写入器", proc.Label)
	}
}

// ValidateRemoteRegisterScope 校验 Remote 登记归属是否有合法声明来源，但不写 vault。
func ValidateRemoteRegisterScope(workspace Workspace, scope secrets.RemoteScope) error {
	_, err := resolveRemoteRegisterTarget(workspace, scope, false, nil)
	return err
}

// resolveRemoteRegisterTarget 只允许已声明项目的固定平面归属。
// 不允许通过 Remote 临时创建项目，也不接受 public 或自定义别名。
func resolveRemoteRegisterTarget(workspace Workspace, scope secrets.RemoteScope, ensureBundle bool, reporter Reporter) (secrets.SyncTarget, error) {
	_ = ensureBundle
	_ = reporter
	if !scope.Valid() {
		return secrets.SyncTarget{}, fmt.Errorf("必须指定项目与平面")
	}
	var projects map[string]*pmodel.Loaded
	readErr := withAppReadRepo(func(tx *repo.Transaction) error {
		var err error
		projects, err = pmodel.Scan(tx.WorkDir())
		return err
	})
	if readErr != nil && !strings.Contains(readErr.Error(), "仓库未连接") {
		return secrets.SyncTarget{}, readErr
	}
	if workspace.EffectivePlane() == WorkspaceUser && !secrets.IsMachinePlane(scope.Plane) {
		return secrets.SyncTarget{}, fmt.Errorf("用户平面不能登记 project secrets: %s", scope)
	}
	if workspace.EffectivePlane() == WorkspaceProject && secrets.IsMachinePlane(scope.Plane) {
		return secrets.SyncTarget{}, fmt.Errorf("项目平面不能登记 user secrets: %s", scope)
	}
	if _, declared := projects[scope.P]; !declared {
		return secrets.SyncTarget{}, fmt.Errorf("项目 %q 未声明；禁止为包外地址手工创建 SyncTarget", scope.P)
	}
	return secrets.NewPSyncTarget(scope.P, scope.Plane)
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
func landRegisteredSSHKey(workspace Workspace, target secrets.SyncTarget, decBundleName string, key secrets.SSHKeyItem) (bool, error) {
	var exists bool
	var err error
	if workspace.EffectivePlane() == WorkspaceUser || secrets.IsMachinePlane(target.Plane) {
		exists, err = secrets.LocalSSHKeyExists(decBundleName, key.Name)
	} else {
		exists, err = secrets.InspectProjectSSHKeyLanding(workspace.Root, decBundleName, key.Name)
	}
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
	if workspace.EffectivePlane() == WorkspaceUser || secrets.IsMachinePlane(target.Plane) {
		err = secrets.WriteSSHKeyLandings(landings)
	} else {
		err = secrets.WriteProjectSSHKeyLandings(workspace.Root, landings)
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
