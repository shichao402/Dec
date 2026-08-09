package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/repo"
	"gopkg.in/yaml.v3"
)

// DefaultServerURL 为 Bitwarden 美国公有云 vault 地址。
const DefaultServerURL = "https://vault.bitwarden.com"

const secretsConfigHeader = `# Bitwarden secrets 连接配置
# server_url: Bitwarden 服务器地址
#   美国公有云（默认）: https://vault.bitwarden.com
#   欧盟公有云:         https://vault.bitwarden.eu
#   自托管示例:         https://vault.example.com
# email: 登录邮箱（web unlock 成功后自动写入）
# project_secrets: 可选；project 级 Bitwarden folder 名，默认 = project_name
# user_enabled_bundles: 本机始终同步的 secrets bundle（与各 project enabled_bundles 并集）
# known_secret_bundles: 本机已知的 secrets bundle 名（枚举/pull 后写入，供 Settings 候选，非启用）
# bundles: 可选显式别名绑定；默认同名，一般不需要

`

// Config 对应 ~/.dec/secrets/config.yaml。
type Config struct {
	ServerURL           string          `yaml:"server_url"`
	Email               string          `yaml:"email"`
	ProjectSecrets      string          `yaml:"project_secrets,omitempty"`
	UserEnabledBundles  []string        `yaml:"user_enabled_bundles,omitempty"`
	KnownSecretBundles  []string        `yaml:"known_secret_bundles,omitempty"`
	Bundles             []BundleBinding `yaml:"bundles,omitempty"`
}

func secretsDir() (string, error) {
	root, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "secrets"), nil
}

// ConfigPath 返回 ~/.dec/secrets/config.yaml 路径。
func ConfigPath() (string, error) {
	dir, err := secretsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// LoadConfig 读取 secrets 配置；文件不存在时返回带默认值的 Config。
func LoadConfig() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyConfigDefaults(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("读取 secrets 配置失败: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析 secrets 配置失败: %w", err)
	}
	for i := range cfg.Bundles {
		cfg.Bundles[i] = normalizeBinding(cfg.Bundles[i].DecBundleName, cfg.Bundles[i])
	}
	cfg.UserEnabledBundles = NormalizeBundleNames(cfg.UserEnabledBundles)
	cfg.KnownSecretBundles = NormalizeBundleNames(cfg.KnownSecretBundles)
	applyConfigDefaults(cfg)
	return cfg, nil
}

// SaveConfig 写入 ~/.dec/secrets/config.yaml。
func SaveConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("secrets 配置不能为空")
	}
	cfg.UserEnabledBundles = NormalizeBundleNames(cfg.UserEnabledBundles)
	cfg.KnownSecretBundles = NormalizeBundleNames(cfg.KnownSecretBundles)
	applyConfigDefaults(cfg)
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化 secrets 配置失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(secretsConfigHeader+string(data)), 0600); err != nil {
		return fmt.Errorf("写入 secrets 配置失败: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("设置 secrets 配置权限失败: %w", err)
	}
	return nil
}

// IsConfigured 判定是否已配置 Bitwarden 连接（含默认 server_url）。
func IsConfigured() (bool, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(cfg.ServerURL) != "", nil
}

// CanAuthenticate 判定是否具备 web unlock / API 登录所需字段。
func (c *Config) CanAuthenticate() bool {
	return c != nil && c.ServerURL != "" && c.Email != ""
}

// SaveEmail 将 email 写回 ~/.dec/secrets/config.yaml，保留其他字段。
func SaveEmail(email string) error {
	email = strings.TrimSpace(email)
	if isPlaceholderEmail(email) {
		return nil
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	cfg.Email = email
	return SaveConfig(cfg)
}

// defaultSecretsBundleName 返回未显式配置时的默认 Bitwarden folder 名（严格同名）。
func defaultSecretsBundleName(decBundleName string) string {
	return strings.TrimSpace(decBundleName)
}

// ResolveBinding 解析 Dec bundle 对应的 secrets 绑定；未显式配置时同名。
func (c *Config) ResolveBinding(decBundleName string) BundleBinding {
	for _, b := range c.Bundles {
		if b.DecBundleName == decBundleName {
			return normalizeBinding(decBundleName, b)
		}
	}
	return normalizeBinding(decBundleName, BundleBinding{DecBundleName: decBundleName})
}

// ProjectSecretsName 返回显式配置的 project secrets folder 名。
func (c *Config) ProjectSecretsName() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.ProjectSecrets)
}

// ResolveProjectSecrets 解析 project 级 secrets folder。
// project_secrets 未设时回退为 projectName；两者皆空时 enabled=false。
func (c *Config) ResolveProjectSecrets(projectName string) (name string, enabled bool) {
	if explicit := c.ProjectSecretsName(); explicit != "" {
		return explicit, true
	}
	name = strings.TrimSpace(projectName)
	if name == "" || name == "unknown" {
		return "", false
	}
	return name, true
}

// ProjectSecretsBinding 构造 project 级 secrets 的 BundleBinding。
func ProjectSecretsBinding(secretsName string) BundleBinding {
	return BundleBinding{
		DecBundleName:     ProjectSecretsDecBundleName,
		SecretsBundleName: strings.TrimSpace(secretsName),
	}
}

// NormalizeBundleNames 去空白、去重，保序。
func NormalizeBundleNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		name = strings.TrimPrefix(name, BundleFolderPrefix)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// MergeEnabledBundles 合并 project 与 user 级启用列表（并集、保序：project 在前）。
func MergeEnabledBundles(projectEnabled, userEnabled []string) []string {
	return NormalizeBundleNames(append(append([]string{}, projectEnabled...), userEnabled...))
}

// UserEnabledBundleNames 返回规范化后的用户级启用列表。
func (c *Config) UserEnabledBundleNames() []string {
	if c == nil {
		return nil
	}
	return NormalizeBundleNames(c.UserEnabledBundles)
}

// KnownSecretBundleNames 返回本机已知的 secrets bundle 逻辑名（非启用）。
func (c *Config) KnownSecretBundleNames() []string {
	if c == nil {
		return nil
	}
	return NormalizeBundleNames(c.KnownSecretBundles)
}

// RememberSecretBundles 把发现的 secrets bundle 名合并写入 known_secret_bundles（幂等）。
// 用于 Settings 候选与刷新可见性；不写入 Dec Git vault。
func RememberSecretBundles(names []string) error {
	names = NormalizeBundleNames(names)
	if len(names) == 0 {
		return nil
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	before := cfg.KnownSecretBundleNames()
	merged := NormalizeBundleNames(append(append([]string{}, before...), names...))
	if equalStringSlices(before, merged) {
		return nil
	}
	cfg.KnownSecretBundles = merged
	return SaveConfig(cfg)
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// ResolveSyncTargets 解析一次 pull/push 的全部 SyncTarget。
// 同一同步集合内 Bitwarden folder 名冲突时直接失败。
// enabledBundles 应为已合并的 project∪user 列表（见 MergeEnabledBundles）。
func (c *Config) ResolveSyncTargets(enabledBundles []string, projectName string) ([]SyncTarget, error) {
	enabledBundles = NormalizeBundleNames(enabledBundles)
	targets := make([]SyncTarget, 0, len(enabledBundles)+1)
	seenFolder := make(map[string]string) // folder -> label

	add := func(t SyncTarget, label string) error {
		prev, ok := seenFolder[t.Folder]
		if ok && prev != label {
			return fmt.Errorf("Bitwarden folder %q 同时绑定 %s 与 %s", t.Folder, prev, label)
		}
		seenFolder[t.Folder] = label
		targets = append(targets, t)
		return nil
	}

	for _, bundleName := range enabledBundles {
		binding := c.ResolveBinding(bundleName)
		target, err := NewBundleSyncTarget(bundleName, binding.SecretsBundleName)
		if err != nil {
			return nil, err
		}
		if err := add(target, fmt.Sprintf("bundle %q", bundleName)); err != nil {
			return nil, err
		}
	}

	if folder, ok := c.ResolveProjectSecrets(projectName); ok {
		target, err := NewProjectSyncTarget(projectName, folder)
		if err != nil {
			return nil, err
		}
		// project folder 显式覆盖名可能与 projectName 不同；用 ResolveProjectSecrets 的 folder。
		if folder != target.Name {
			target.Folder = folder
		}
		if err := add(target, fmt.Sprintf("project %q", target.Name)); err != nil {
			return nil, err
		}
	}

	return targets, nil
}

func applyConfigDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.ServerURL) == "" {
		cfg.ServerURL = DefaultServerURL
	}
}

func normalizeBinding(decBundleName string, b BundleBinding) BundleBinding {
	if b.DecBundleName == "" {
		b.DecBundleName = decBundleName
	}
	if b.SecretsBundleName == "" && strings.TrimSpace(b.Folder) != "" {
		b.SecretsBundleName = strings.TrimSpace(b.Folder)
	}
	if b.SecretsBundleName == "" {
		b.SecretsBundleName = defaultSecretsBundleName(b.DecBundleName)
	}
	return b
}

// MigrateConfigIfNeeded 将废弃的 folder 字段迁移为 secrets_bundle 并回写配置（幂等）。
func MigrateConfigIfNeeded() (bool, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return false, err
	}
	changed := false
	for i := range cfg.Bundles {
		b := &cfg.Bundles[i]
		if strings.TrimSpace(b.Folder) == "" {
			continue
		}
		if b.SecretsBundleName == "" {
			b.SecretsBundleName = strings.TrimSpace(b.Folder)
		}
		b.Folder = ""
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := SaveConfig(cfg); err != nil {
		return false, err
	}
	return true, nil
}
