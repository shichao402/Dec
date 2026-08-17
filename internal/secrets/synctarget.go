package secrets

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// BundleFolderPrefix 是 Bitwarden 中 bundle 级 folder 的统一前缀，
// 用于与 project 级 folder（裸实体名）区分。
const BundleFolderPrefix = "bundle/"

// Declared 报告 target 是否由声明型构造函数产生。
// 包外结构体字面量无法设置该标记，只能作为浏览节点或构造函数的重建输入。
func (t SyncTarget) Declared() bool {
	return t.declared
}

// Clone 保留 SyncTarget 的声明标记，供调用方避免用结构体字面量拷贝时丢失归属。
func (t SyncTarget) Clone() SyncTarget {
	return t
}

// RequireDeclared 拒绝把浏览节点或手工拼装的 SyncTarget 用于写入。
func RequireDeclared(t SyncTarget) error {
	if t.Declared() {
		return nil
	}
	label := strings.TrimSpace(t.Folder)
	if label == "" {
		label = strings.TrimSpace(t.Name)
	}
	if label == "" {
		label = "<empty>"
	}
	return fmt.Errorf("SyncTarget %q 未声明：ADR 0013 要求写入目标通过声明型构造函数创建", label)
}

// NewBrowseFolder 构造只读远端 folder 节点。它故意保持未声明，
// 可用于 Remote 浏览与删除，但不能传给写入方法。
func NewBrowseFolder(folder string) (SyncTarget, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return SyncTarget{}, fmt.Errorf("浏览 folder 不能为空")
	}
	return SyncTarget{
		Kind:   SyncKindProject,
		Name:   folder,
		Folder: folder,
	}, nil
}

// MarshalJSON 显式携带声明标记，使 dec TUI 与 dec-server 之间的短生命周期
// SyncTarget 不会因未导出字段被 JSON 丢失。
func (t SyncTarget) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind      SyncKind
		Name      string
		Folder    string
		LocalRoot string
		Plane     SyncPlane
		Declared  bool
	}{
		Kind:      t.Kind,
		Name:      t.Name,
		Folder:    t.Folder,
		LocalRoot: t.LocalRoot,
		Plane:     t.Plane,
		Declared:  t.Declared(),
	})
}

// UnmarshalJSON 仅通过声明型构造函数恢复 declared=true，再复制原有展示字段。
func (t *SyncTarget) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind      SyncKind
		Name      string
		Folder    string
		LocalRoot string
		Plane     SyncPlane
		Declared  bool
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if !raw.Declared {
		*t = SyncTarget{
			Kind:      raw.Kind,
			Name:      raw.Name,
			Folder:    raw.Folder,
			LocalRoot: raw.LocalRoot,
			Plane:     raw.Plane,
		}
		return nil
	}

	var (
		rebuilt SyncTarget
		err     error
	)
	switch raw.Kind {
	case SyncKindProject:
		rebuilt, err = NewProjectSyncTarget(raw.Name, raw.Folder)
	case SyncKindBundle:
		if IsMachinePlane(raw.Plane) {
			rebuilt, err = NewMachineBundleSyncTarget(raw.Name, raw.Folder)
		} else {
			rebuilt, err = NewBundleSyncTarget(raw.Name, raw.Folder)
		}
	default:
		err = fmt.Errorf("未知 SyncKind %q", raw.Kind)
	}
	if err != nil {
		return fmt.Errorf("恢复已声明 SyncTarget 失败: %w", err)
	}
	*t = rebuilt
	return nil
}

// DefaultBundleFolder 返回 bundle 在 Bitwarden 上的默认 folder 名。
func DefaultBundleFolder(bundleName string) string {
	name := strings.TrimSpace(bundleName)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, BundleFolderPrefix) {
		return name
	}
	return BundleFolderPrefix + name
}

// MachineSecretsRoot 返回 ~/.dec/secrets（与 config.yaml 同树）。
func MachineSecretsRoot() (string, error) {
	return secretsDir()
}

// NewBundleSyncTarget 构造项目平面 bundle SyncTarget；folder 默认 bundle/<name>。
func NewBundleSyncTarget(bundleName, folder string) (SyncTarget, error) {
	name := strings.TrimSpace(bundleName)
	if name == "" {
		return SyncTarget{}, fmt.Errorf("bundle 名不能为空")
	}
	if name == ProjectSecretsDecBundleName {
		return SyncTarget{}, fmt.Errorf("保留名 %q 不能用作 bundle", ProjectSecretsDecBundleName)
	}
	folder = strings.TrimSpace(folder)
	if folder == "" {
		folder = DefaultBundleFolder(name)
	}
	return SyncTarget{
		Kind:      SyncKindBundle,
		Name:      name,
		Folder:    folder,
		LocalRoot: path.Join(BundleSecretsLocalRelPrefix, name),
		Plane:     SyncPlaneProject,
		declared:  true,
	}, nil
}

// NewMachineBundleSyncTarget 构造机器平面 bundle SyncTarget（~/.dec/secrets/bundles/<name>）。
func NewMachineBundleSyncTarget(bundleName, folder string) (SyncTarget, error) {
	name := strings.TrimSpace(bundleName)
	if name == "" {
		return SyncTarget{}, fmt.Errorf("bundle 名不能为空")
	}
	if name == ProjectSecretsDecBundleName {
		return SyncTarget{}, fmt.Errorf("保留名 %q 不能用作 bundle", ProjectSecretsDecBundleName)
	}
	folder = strings.TrimSpace(folder)
	if folder == "" {
		folder = DefaultBundleFolder(name)
	}
	return SyncTarget{
		Kind:      SyncKindBundle,
		Name:      name,
		Folder:    folder,
		LocalRoot: path.Join(MachineBundleSecretsRelPrefix, name),
		Plane:     SyncPlaneMachine,
		declared:  true,
	}, nil
}

// NewProjectSyncTarget 构造历史 project 级 SyncTarget（落地 `.secrets/project`）。
//
// Deprecated: ADR 0014 取消 project 级可写归属。写入路径禁止使用；仅保留给存量迁移/
// 只读兼容与旧测试。新代码请用 NewBundleSyncTarget / NewMachineBundleSyncTarget。
func NewProjectSyncTarget(projectName, folder string) (SyncTarget, error) {
	name := strings.TrimSpace(projectName)
	if name == "" || name == "unknown" {
		return SyncTarget{}, fmt.Errorf("project 名不能为空")
	}
	folder = strings.TrimSpace(folder)
	if folder == "" {
		folder = name
	}
	return SyncTarget{
		Kind:      SyncKindProject,
		Name:      name,
		Folder:    folder,
		LocalRoot: ProjectSecretsLocalRel,
		Plane:     SyncPlaneProject,
		declared:  true,
	}, nil
}

// ResolveTarget 优先返回 req.Target；否则从旧字段推导。
func ResolveTarget(kind SyncKind, name string, binding BundleBinding, explicit SyncTarget) (SyncTarget, error) {
	if explicit.Declared() {
		return explicit.Clone(), nil
	}

	folder := strings.TrimSpace(binding.SecretsBundleName)
	if folder == "" {
		folder = strings.TrimSpace(binding.Folder)
	}
	if explicitFolder := strings.TrimSpace(explicit.Folder); explicitFolder != "" {
		folder = explicitFolder
	}
	resolvedKind := kind
	if explicit.Kind == SyncKindProject || explicit.Kind == SyncKindBundle {
		resolvedKind = explicit.Kind
	}
	switch resolvedKind {
	case SyncKindProject:
		projectName := strings.TrimSpace(explicit.Name)
		if projectName == "" {
			projectName = strings.TrimSpace(name)
		}
		if projectName == ProjectSecretsDecBundleName {
			projectName = ""
		}
		if projectName == "" {
			projectName = folder
		}
		return NewProjectSyncTarget(projectName, folder)
	case SyncKindBundle:
		decName := strings.TrimSpace(explicit.Name)
		if decName == "" {
			decName = strings.TrimSpace(binding.DecBundleName)
		}
		if decName == "" {
			decName = strings.TrimSpace(name)
		}
		if IsMachinePlane(explicit.Plane) {
			return NewMachineBundleSyncTarget(decName, folder)
		}
		return NewBundleSyncTarget(decName, folder)
	default:
		return SyncTarget{}, fmt.Errorf("未知 SyncKind %q", resolvedKind)
	}
}

// planeOf 返回有效平面（空视为 project）。
func planeOf(t SyncTarget) SyncPlane {
	if IsMachinePlane(t.Plane) {
		return SyncPlaneMachine
	}
	return SyncPlaneProject
}

// ResolveAbsDir 返回 SyncTarget.LocalRoot 的绝对目录。
func ResolveAbsDir(projectRoot string, target SyncTarget) (string, error) {
	root := strings.Trim(filepath.ToSlash(target.LocalRoot), "/")
	if root == "" {
		return "", fmt.Errorf("SyncTarget.LocalRoot 不能为空")
	}
	if planeOf(target) == SyncPlaneMachine {
		base, err := MachineSecretsRoot()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, filepath.FromSlash(root)), nil
	}
	if strings.TrimSpace(projectRoot) == "" {
		return "", fmt.Errorf("project 平面需要 projectRoot")
	}
	return filepath.Join(projectRoot, filepath.FromSlash(root)), nil
}

// RootRelPath 把同步根相对路径转为「展示/校验用」相对路径。
// project 平面：相对项目根；machine 平面：~/.dec/secrets/bundles/... 风格（以 .dec/secrets 为逻辑前缀）。
func RootRelPath(target SyncTarget, noteRel string) (string, error) {
	rel, err := normalizeSyncRelPath(noteRel)
	if err != nil {
		return "", err
	}
	root := strings.Trim(filepath.ToSlash(target.LocalRoot), "/")
	if root == "" {
		return "", fmt.Errorf("SyncTarget.LocalRoot 不能为空")
	}
	joined := path.Join(root, rel)
	if planeOf(target) == SyncPlaneMachine {
		return path.Join(".dec/secrets", joined), nil
	}
	return joined, nil
}

// ProjectRelPath 把同步根相对路径转为项目根相对路径（仅 project 平面）。
// 兼容旧调用；machine 平面请用 RootRelPath / AbsolutePath。
func ProjectRelPath(target SyncTarget, noteRel string) (string, error) {
	return RootRelPath(target, noteRel)
}

// AbsolutePath 返回 note 在磁盘上的绝对路径。
func AbsolutePath(projectRoot string, target SyncTarget, noteRel string) (string, error) {
	rel, err := normalizeSyncRelPath(noteRel)
	if err != nil {
		return "", err
	}
	dir, err := ResolveAbsDir(projectRoot, target)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.FromSlash(rel)), nil
}

// RemoteNoteName 返回写入 Bitwarden 的 Note 名（= 相对 LocalRoot 的路径）。
func RemoteNoteName(target SyncTarget, noteRel string) (string, error) {
	_ = target
	return normalizeSyncRelPath(noteRel)
}

// LocalNoteRelFromRemote 把 BW Note 名转为相对 LocalRoot 的路径。
func LocalNoteRelFromRemote(target SyncTarget, remoteName string) (rel string, ok bool, err error) {
	_ = target
	rel, err = normalizeSyncRelPath(remoteName)
	if err != nil {
		return "", false, err
	}
	return rel, true, nil
}

// normalizeSyncRelPath 规范化相对 SyncTarget.LocalRoot 的 Note 名。
func normalizeSyncRelPath(raw string) (string, error) {
	trimmed := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	if trimmed == "" {
		return "", fmt.Errorf("secrets note 路径不能为空")
	}
	if strings.HasPrefix(trimmed, "/") || filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("secrets note 路径不能是绝对路径: %q", raw)
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		return "", fmt.Errorf("secrets note 路径不能以 ~ 开头: %q", raw)
	}
	if strings.Contains(trimmed, ":") {
		return "", fmt.Errorf("secrets note 路径不能包含盘符: %q", raw)
	}
	clean := path.Clean(trimmed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("非法 secrets note 路径: %q", raw)
	}
	return clean, nil
}

// IsEnvNote 判断 note 是否属于 env 注入源（.env/*.env）。
func IsEnvNote(noteRel string) bool {
	rel, err := normalizeSyncRelPath(noteRel)
	if err != nil {
		return false
	}
	dir, base := path.Split(rel)
	dir = strings.Trim(dir, "/")
	return dir == TypeDirEnv && strings.HasSuffix(strings.ToLower(base), ".env")
}
