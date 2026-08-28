package secrets

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

const (
	PPrivateFolderSegment = bwPrivateSegment
)

// Declared 报告 target 是否由声明型构造函数产生。
// 包外结构体字面量无法设置该标记，只能作为浏览节点或构造函数的重建输入。
func (t SyncTarget) Declared() bool {
	return t.declared
}

// Clone 保留 SyncTarget 的声明标记，供调用方避免用结构体字面量拷贝时丢失归属。
func (t SyncTarget) Clone() SyncTarget {
	return t
}

// Scope 返回远端寻址域。只有声明型 P target 能读写远端；浏览节点会失败。
func (t SyncTarget) Scope() (RemoteScope, error) {
	if scope, err := ParseRemoteScope(t.Address); err == nil {
		return scope, nil
	}
	return NewRemoteScope(t.Name, t.Plane)
}

// RequireDeclared 拒绝把浏览节点或手工拼装的 SyncTarget 用于写入。
func RequireDeclared(t SyncTarget) error {
	if t.Declared() {
		return nil
	}
	label := strings.TrimSpace(t.Address)
	if label == "" {
		label = strings.TrimSpace(t.Name)
	}
	if label == "" {
		label = "<empty>"
	}
	return fmt.Errorf("SyncTarget %q 未声明：写入目标必须通过声明型构造函数创建", label)
}

// NewBrowseAddress 构造只读远端节点。它故意保持未声明，可用于 Remote 浏览与
// 删除存量条目，但不能传给写入方法。
func NewBrowseAddress(address string) (SyncTarget, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return SyncTarget{}, fmt.Errorf("浏览地址不能为空")
	}
	target := SyncTarget{Name: address, Address: address}
	if scope, err := ParseRemoteScope(address); err == nil {
		target.Name = scope.P
		target.Plane = scope.Plane
	}
	return target, nil
}

// MarshalJSON 显式携带声明标记，使 dec TUI 与 dec-server 之间的短生命周期
// SyncTarget 不会因未导出字段被 JSON 丢失。
func (t SyncTarget) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name      string
		Address   string
		LocalRoot string
		Plane     SyncPlane
		Declared  bool
	}{
		Name:      t.Name,
		Address:   t.Address,
		LocalRoot: t.LocalRoot,
		Plane:     t.Plane,
		Declared:  t.Declared(),
	})
}

// UnmarshalJSON 仅通过声明型构造函数恢复 declared=true，再复制原有展示字段。
func (t *SyncTarget) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name      string
		Address   string
		LocalRoot string
		Plane     SyncPlane
		Declared  bool
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if !raw.Declared {
		*t = SyncTarget{
			Name:      raw.Name,
			Address:   raw.Address,
			LocalRoot: raw.LocalRoot,
			Plane:     raw.Plane,
		}
		return nil
	}

	rebuilt, err := NewPSyncTarget(raw.Name, raw.Plane)
	if err != nil {
		return fmt.Errorf("恢复已声明 SyncTarget 失败: %w", err)
	}
	*t = rebuilt
	return nil
}

// PFolder 返回 P + 平面的逻辑地址 <p>/private/<plane>。
//
// Deprecated: 用 RemoteScope.String()。保留给存量调用点，返回值不是 Bitwarden
// folder 名——BW 上的 folder 只有 P 名一级。
func PFolder(pName string, plane SyncPlane) string {
	scope, err := NewRemoteScope(pName, plane)
	if err != nil {
		return ""
	}
	return scope.String()
}

// ParsePFolder 解析逻辑地址 <p>/private/<plane>。
//
// Deprecated: 用 ParseRemoteScope。
func ParsePFolder(address string) (pName string, plane SyncPlane, ok bool) {
	scope, err := ParseRemoteScope(address)
	if err != nil {
		return "", "", false
	}
	return scope.P, scope.Plane, true
}

// NewPSyncTarget 构造 ADR 0016 P + plane 声明目标。地址与本地根均不可自定义：
// user: <p>/private/user ↔ ~/.dec/secrets/<p>/
// project: <p>/private/project ↔ <workspace>/.secrets/<p>/
func NewPSyncTarget(pName string, plane SyncPlane) (SyncTarget, error) {
	scope, err := NewRemoteScope(pName, plane)
	if err != nil {
		return SyncTarget{}, err
	}
	localRoot := path.Join(SecretsRootDir, scope.P)
	if IsMachinePlane(scope.Plane) {
		localRoot = scope.P
	}
	return SyncTarget{
		Name:      scope.P,
		Address:   scope.String(),
		LocalRoot: localRoot,
		Plane:     scope.Plane,
		declared:  true,
	}, nil
}

// MachineSecretsRoot 返回 ~/.dec/secrets（与 config.yaml 同树）。
func MachineSecretsRoot() (string, error) {
	return secretsDir()
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
// project 平面：相对项目根；machine 平面：以 .dec/secrets 为逻辑前缀。
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

// NormalizeNoteRel 规范化同步根相对路径。远端条目名的编码由 BW 实现负责，
// 包外只需要这一层校验（拒绝绝对路径、盘符、~ 与 .. 逃逸）。
func NormalizeNoteRel(raw string) (string, error) {
	return normalizeSyncRelPath(raw)
}

// normalizeSyncRelPath 规范化相对 SyncTarget.LocalRoot 的路径。
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
