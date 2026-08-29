package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/repo"
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
# known_secret_bundles: 本机已知的 secrets-related bundle 名（枚举/pull 后写入，供 Settings 候选；启用缺失 vault 包时再创建）
# known_secret_bundle_members: bundle 短名 → secrets 条目路径（Note / SSH Key 名，无正文）。有 session 刷新时覆盖写入，无权限时供 Bundles 页回填成员数
# bundles: 可选显式别名绑定；默认同名，一般不需要
# 用户平面启用列表已迁至 ~/.dec/config.yaml 的 enabled_bundles（ADR 0009）

`

// Config 对应 ~/.dec/secrets/config.yaml。
type Config struct {
	ServerURL                string              `yaml:"server_url"`
	Email                    string              `yaml:"email"`
	ProjectSecrets           string              `yaml:"project_secrets,omitempty"`
	UserEnabledBundles       []string            `yaml:"user_enabled_bundles,omitempty"` // 遗留：仅 Load 读取；Save 前清空（ADR 0009）
	KnownSecretBundles       []string            `yaml:"known_secret_bundles,omitempty"`
	KnownSecretBundleMembers map[string][]string `yaml:"known_secret_bundle_members,omitempty"`
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
	cfg.UserEnabledBundles = NormalizeBundleNames(cfg.UserEnabledBundles)
	cfg.KnownSecretBundles = NormalizeBundleNames(cfg.KnownSecretBundles)
	cfg.KnownSecretBundleMembers = normalizeKnownSecretBundleMembers(cfg.KnownSecretBundleMembers)
	applyConfigDefaults(cfg)
	return cfg, nil
}

// SaveConfig 写入 ~/.dec/secrets/config.yaml。
// 写入前清空 UserEnabledBundles（已迁至 GlobalConfig.EnabledBundles，不再持久化）。
func SaveConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("secrets 配置不能为空")
	}
	cfg.UserEnabledBundles = nil
	cfg.KnownSecretBundles = NormalizeBundleNames(cfg.KnownSecretBundles)
	cfg.KnownSecretBundleMembers = normalizeKnownSecretBundleMembers(cfg.KnownSecretBundleMembers)
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

// ProjectSecretsName 返回显式配置的 project secrets folder 名。
func (c *Config) ProjectSecretsName() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.ProjectSecrets)
}

// ResolveProjectSecrets 解析历史 project 级 secrets folder 名（仅迁移/只读兼容）。
//
// Deprecated: ADR 0014 取消 project 级可写归属；pull/push plan 不再使用本函数。
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

// legacyBundleFolderPrefix 是已删除的 bundle 写入路径留下的 folder 前缀。
const legacyBundleFolderPrefix = "bundle/"

// NormalizeBundleNames 去空白、去重，保序。
// 存量配置里可能残留 `bundle/<名>` 写法（写入路径已删除），读入时剥掉前缀。
func NormalizeBundleNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimPrefix(strings.TrimSpace(raw), legacyBundleFolderPrefix)
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

// LegacyUserEnabledBundleNames 返回已加载 Config 中遗留 user_enabled_bundles（规范化）。
// 供迁移到 GlobalConfig.EnabledBundles；新代码勿再依赖。
func (c *Config) LegacyUserEnabledBundleNames() []string {
	if c == nil {
		return nil
	}
	return NormalizeBundleNames(c.UserEnabledBundles)
}

// PeekLegacyUserEnabledBundles 读取磁盘上旧字段供 GlobalConfig 迁移（不经 Save 清空）。
func PeekLegacyUserEnabledBundles() ([]string, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	return cfg.LegacyUserEnabledBundleNames(), nil
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

// ForgetSecretBundles 从 known_secret_bundles 移除指定短名（幂等）。
// 用于 bundle 删除后避免 Settings 候选 / 再启用路径把已删包「记回来」。
func ForgetSecretBundles(names []string) error {
	names = NormalizeBundleNames(names)
	if len(names) == 0 {
		return nil
	}
	drop := make(map[string]struct{}, len(names))
	for _, name := range names {
		drop[name] = struct{}{}
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	before := cfg.KnownSecretBundleNames()
	after := make([]string, 0, len(before))
	for _, name := range before {
		if _, skip := drop[name]; skip {
			continue
		}
		after = append(after, name)
	}
	after = NormalizeBundleNames(after)
	membersChanged := dropKnownSecretBundleMembers(cfg, drop)
	if equalStringSlices(before, after) && !membersChanged {
		return nil
	}
	cfg.KnownSecretBundles = after
	return SaveConfig(cfg)
}

// SecretBundleMembers 返回本机缓存的 secrets 条目路径（Note / SSH Key 名，无正文）。
func SecretBundleMembers(bundleName string) []string {
	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		return nil
	}
	return cfg.SecretBundleMembers(bundleName)
}

// SecretBundleMembers 返回指定 bundle 的缓存路径副本。
func (c *Config) SecretBundleMembers(bundleName string) []string {
	names := NormalizeBundleNames([]string{bundleName})
	if c == nil || len(names) == 0 || len(c.KnownSecretBundleMembers) == 0 {
		return nil
	}
	paths := c.KnownSecretBundleMembers[names[0]]
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, len(paths))
	copy(out, paths)
	return out
}

// RememberSecretBundleMembers 覆盖写入一个 bundle 的 secrets 成员路径缓存。
// paths 为空时写成空列表，避免下次无 session 时读到陈旧路径。
func RememberSecretBundleMembers(bundleName string, paths []string) error {
	names := NormalizeBundleNames([]string{bundleName})
	if len(names) == 0 {
		return nil
	}
	return RememberAllSecretBundleMembers(map[string][]string{names[0]: paths})
}

// RememberAllSecretBundleMembers 覆盖写入多个 bundle 的 secrets 成员路径（一次读改写）。
// 未出现在 members 里的既有缓存保留；出现且路径为空的写成空列表。
func RememberAllSecretBundleMembers(members map[string][]string) error {
	if len(members) == 0 {
		return nil
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	if cfg.KnownSecretBundleMembers == nil {
		cfg.KnownSecretBundleMembers = make(map[string][]string, len(members))
	}
	changed := false
	for rawName, paths := range members {
		names := NormalizeBundleNames([]string{rawName})
		if len(names) == 0 {
			continue
		}
		normalized := normalizeSecretMemberPaths(paths)
		if normalized == nil {
			normalized = []string{}
		}
		prev := cfg.KnownSecretBundleMembers[names[0]]
		if prev != nil && equalStringSlices(prev, normalized) {
			continue
		}
		cfg.KnownSecretBundleMembers[names[0]] = normalized
		changed = true
	}
	if !changed {
		return nil
	}
	return SaveConfig(cfg)
}

func dropKnownSecretBundleMembers(cfg *Config, drop map[string]struct{}) bool {
	if cfg == nil || len(cfg.KnownSecretBundleMembers) == 0 || len(drop) == 0 {
		return false
	}
	changed := false
	for name := range drop {
		if _, ok := cfg.KnownSecretBundleMembers[name]; !ok {
			continue
		}
		delete(cfg.KnownSecretBundleMembers, name)
		changed = true
	}
	if len(cfg.KnownSecretBundleMembers) == 0 {
		cfg.KnownSecretBundleMembers = nil
	}
	return changed
}

func normalizeKnownSecretBundleMembers(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for rawName, paths := range in {
		names := NormalizeBundleNames([]string{rawName})
		if len(names) == 0 {
			continue
		}
		normalized := normalizeSecretMemberPaths(paths)
		if normalized == nil {
			normalized = []string{}
		}
		out[names[0]] = normalized
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeSecretMemberPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
		path = strings.TrimPrefix(path, "./")
		if path == "" || path == "." {
			continue
		}
		if _, dup := seen[path]; dup {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
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

// ResolvePSyncTargets 按 ADR 0016 生成固定项目 + plane 目标。
// project 平面调用方只能传家项目；requires 不得加入。user 平面传本机显式启用的项目。
func ResolvePSyncTargets(plane SyncPlane, pNames []string) ([]SyncTarget, error) {
	pNames = NormalizeBundleNames(pNames)
	targets := make([]SyncTarget, 0, len(pNames))
	for _, name := range pNames {
		target, err := NewPSyncTarget(name, plane)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
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

