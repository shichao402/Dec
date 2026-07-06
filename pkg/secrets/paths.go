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

// NotePathForBundleFile 构造 Bitwarden Secure Note 名（folder 内相对路径，不含 `.secrets/<bundle>/` 前缀）。
func NotePathForBundleFile(secretsBundleName, relWithinBundle string) (string, error) {
	_ = secretsBundleName
	return normalizeBundleRelativePath(relWithinBundle)
}

// SecretsLocalPath 返回 bundle 内文件在项目根的落地相对路径。
func SecretsLocalPath(secretsBundleName, relWithinBundle string) (string, error) {
	rel, err := normalizeBundleRelativePath(relWithinBundle)
	if err != nil {
		return "", err
	}
	return normalizeProjectRelativePath(secretsBundlePrefix(secretsBundleName) + rel)
}

// IsLegacyNoteName 判断 Bitwarden Secure Note 名是否为需迁移的旧格式。
func IsLegacyNoteName(noteName string) bool {
	trimmed := strings.TrimSpace(noteName)
	if trimmed == "" {
		return false
	}
	return !isCanonicalNoteName(trimmed)
}

func isCanonicalNoteName(noteName string) bool {
	trimmed := strings.TrimSpace(noteName)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, SecretsRootDir+"/") {
		return false
	}
	if strings.HasPrefix(trimmed, ".config/") {
		return false
	}
	return true
}

// CanonicalNoteName 将 legacy 或中间态 note 名规范为 folder 内相对路径。
func CanonicalNoteName(secretsBundleName, noteName string) (string, error) {
	trimmed := strings.TrimSpace(noteName)
	if trimmed == "" {
		return "", fmt.Errorf("Secure Note 名称不能为空")
	}
	prefix := secretsBundlePrefix(secretsBundleName)
	if strings.HasPrefix(trimmed, prefix) {
		rel := strings.TrimPrefix(trimmed, prefix)
		return normalizeBundleRelativePath(canonicalWithinBundlePath(rel))
	}
	if strings.HasPrefix(trimmed, ".config/") {
		return normalizeBundleRelativePath(canonicalWithinBundlePath(trimmed))
	}
	if isCanonicalNoteName(trimmed) {
		return normalizeBundleRelativePath(canonicalWithinBundlePath(trimmed))
	}
	return "", fmt.Errorf("无法解析 Secure Note 名称: %q", noteName)
}

// CanonicalNotePath 将 note 名映射为本地落地相对路径（`.secrets/<bundle>/...`）。
func CanonicalNotePath(secretsBundleName, noteName string) (string, error) {
	return LandingPathForNote(secretsBundleName, noteName)
}

func canonicalWithinBundlePath(rel string) string {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(rel, "./")))
	if strings.HasPrefix(clean, ".config/") {
		return strings.TrimPrefix(clean, ".config/")
	}
	return clean
}

// LegacyNoteName 若 noteName 为长前缀旧格式，返回其 folder 内相对路径别名（供匹配存量 cipher）。
func LegacyNoteName(secretsBundleName, noteName string) string {
	canon, err := CanonicalNoteName(secretsBundleName, noteName)
	if err != nil || canon == "" {
		return ""
	}
	prefix := secretsBundlePrefix(secretsBundleName)
	longForm := prefix + canon
	if noteName == longForm {
		return canon
	}
	if noteName == canon {
		return longForm
	}
	return ""
}

// LandingPathForNote 将 Bitwarden Note 名映射为本地落地相对路径（`.secrets/<bundle>/...`）。
func LandingPathForNote(secretsBundleName, noteName string) (string, error) {
	rel, err := CanonicalNoteName(secretsBundleName, noteName)
	if err != nil {
		return "", err
	}
	return SecretsLocalPath(secretsBundleName, rel)
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
