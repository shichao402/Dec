package secrets

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// BundleFolderPrefix 是 Bitwarden 中 bundle 级 folder 的统一前缀，
// 用于与 project 级 folder（裸实体名）区分。
const BundleFolderPrefix = "bundle/"

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

// NewBundleSyncTarget 构造 bundle 级 SyncTarget；folder 默认 bundle/<name>。
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
	}, nil
}

// NewProjectSyncTarget 构造 project 级 SyncTarget；folder 默认同 project 名。
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
	}, nil
}

// ResolveTarget 优先返回 req.Target；否则从旧字段推导。
func ResolveTarget(kind SyncKind, name string, binding BundleBinding, explicit SyncTarget) (SyncTarget, error) {
	if explicit.LocalRoot != "" && explicit.Folder != "" {
		return explicit, nil
	}
	folder := strings.TrimSpace(binding.SecretsBundleName)
	if folder == "" {
		folder = strings.TrimSpace(binding.Folder)
	}
	switch kind {
	case SyncKindProject:
		return NewProjectSyncTarget(name, folder)
	case SyncKindBundle:
		decName := strings.TrimSpace(binding.DecBundleName)
		if decName == "" {
			decName = name
		}
		if folder == "" {
			folder = strings.TrimSpace(binding.SecretsBundleName)
		}
		return NewBundleSyncTarget(decName, folder)
	default:
		return SyncTarget{}, fmt.Errorf("未知 SyncKind %q", kind)
	}
}

// ProjectRelPath 把同步根相对路径转为项目根相对路径。
func ProjectRelPath(target SyncTarget, noteRel string) (string, error) {
	rel, err := normalizeSyncRelPath(noteRel)
	if err != nil {
		return "", err
	}
	root := strings.Trim(filepath.ToSlash(target.LocalRoot), "/")
	if root == "" {
		return "", fmt.Errorf("SyncTarget.LocalRoot 不能为空")
	}
	return path.Join(root, rel), nil
}

// AbsolutePath 返回 note 在磁盘上的绝对路径。
func AbsolutePath(projectRoot string, target SyncTarget, noteRel string) (string, error) {
	projectRel, err := ProjectRelPath(target, noteRel)
	if err != nil {
		return "", err
	}
	return filepath.Join(projectRoot, filepath.FromSlash(projectRel)), nil
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

// IsEnvNote 判断 note 是否属于 env 注入源（env/*.env）。
func IsEnvNote(noteRel string) bool {
	rel, err := normalizeSyncRelPath(noteRel)
	if err != nil {
		return false
	}
	dir, base := path.Split(rel)
	dir = strings.Trim(dir, "/")
	return dir == "env" && strings.HasSuffix(strings.ToLower(base), ".env")
}
