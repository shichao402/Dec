package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const SecretsRootDir = ".secrets"

func secretsBundlePrefix(secretsBundleName string) string {
	return SecretsRootDir + "/" + strings.TrimSpace(secretsBundleName) + "/"
}

// SecretsBundleDir 返回项目内 `.secrets/<secrets_bundle>/` 绝对路径。
func SecretsBundleDir(projectRoot, secretsBundleName string) string {
	return filepath.Join(projectRoot, SecretsRootDir, strings.TrimSpace(secretsBundleName))
}

// ListSecretsBundleDirNames 返回项目 `.secrets/` 下已存在的 secrets bundle 子目录名。
func ListSecretsBundleDirNames(projectRoot string) ([]string, error) {
	secretsRoot := filepath.Join(projectRoot, SecretsRootDir)
	entries, err := os.ReadDir(secretsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// NotePathForBundleFile 构造 Bitwarden Secure Note 名（项目根相对路径）。
func NotePathForBundleFile(secretsBundleName, relWithinBundle string) (string, error) {
	rel, err := normalizeBundleRelativePath(relWithinBundle)
	if err != nil {
		return "", err
	}
	return secretsBundlePrefix(secretsBundleName) + rel, nil
}

// SecretsLocalPath 返回 bundle 内文件在项目根的落地相对路径。
func SecretsLocalPath(secretsBundleName, relWithinBundle string) (string, error) {
	notePath, err := NotePathForBundleFile(secretsBundleName, relWithinBundle)
	if err != nil {
		return "", err
	}
	return normalizeProjectRelativePath(notePath)
}

// IsLegacyNoteName 判断 Bitwarden Secure Note 名是否为旧格式（不以 `.secrets/` 开头）。
func IsLegacyNoteName(noteName string) bool {
	trimmed := strings.TrimSpace(noteName)
	return trimmed != "" && !strings.HasPrefix(trimmed, SecretsRootDir+"/")
}

// NeedsNoteRename 判断 note 名是否需迁移到规范 `.secrets/<bundle>/...` 路径。
func NeedsNoteRename(secretsBundleName, noteName string) bool {
	trimmed := strings.TrimSpace(noteName)
	if trimmed == "" {
		return false
	}
	prefix := secretsBundlePrefix(secretsBundleName)
	if !strings.HasPrefix(trimmed, prefix) {
		return IsLegacyNoteName(trimmed)
	}
	rel := strings.TrimPrefix(trimmed, prefix)
	return strings.HasPrefix(rel, ".config/") || strings.Contains(rel, "/.config/")
}

// CanonicalNotePath 将 legacy 或中间态 note 名规范为新格式。
func CanonicalNotePath(secretsBundleName, noteName string) (string, error) {
	trimmed := strings.TrimSpace(noteName)
	if trimmed == "" {
		return "", fmt.Errorf("Secure Note 名称不能为空")
	}
	prefix := secretsBundlePrefix(secretsBundleName)
	if strings.HasPrefix(trimmed, prefix) {
		rel, err := normalizeBundleRelativePath(canonicalWithinBundlePath(strings.TrimPrefix(trimmed, prefix)))
		if err != nil {
			return "", err
		}
		return NotePathForBundleFile(secretsBundleName, rel)
	}
	if IsLegacyNoteName(trimmed) {
		rel, err := normalizeBundleRelativePath(canonicalWithinBundlePath(trimmed))
		if err != nil {
			return "", err
		}
		return NotePathForBundleFile(secretsBundleName, rel)
	}
	return normalizeProjectRelativePath(trimmed)
}

func canonicalWithinBundlePath(rel string) string {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(rel, "./")))
	if strings.HasPrefix(clean, ".config/") {
		return strings.TrimPrefix(clean, ".config/")
	}
	return clean
}

// LegacyNoteName 若 noteName 为新格式（`.secrets/<bundle>/...`），返回 Bitwarden 旧格式名。
func LegacyNoteName(secretsBundleName, noteName string) string {
	prefix := secretsBundlePrefix(secretsBundleName)
	trimmed := strings.TrimSpace(noteName)
	if !strings.HasPrefix(trimmed, prefix) {
		return ""
	}
	remainder := strings.TrimPrefix(trimmed, prefix)
	if remainder == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(remainder))
}

// LandingPathForNote 将 Bitwarden Note 名映射为本地落地相对路径（规范 `.secrets/<bundle>/...`）。
func LandingPathForNote(secretsBundleName, noteName string) (string, error) {
	return CanonicalNotePath(secretsBundleName, noteName)
}

func normalizeBundleRelativePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("bundle 内相对路径不能为空")
	}
	clean := filepath.ToSlash(filepath.Clean(trimmed))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("非法 bundle 内相对路径: %q", raw)
	}
	return strings.TrimPrefix(clean, "./"), nil
}
