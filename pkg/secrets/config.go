package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/repo"
	"gopkg.in/yaml.v3"
)

// ProjectSecretsDecBundleName 是 project 级 secrets 在内部 API 中使用的占位 Dec bundle 名。
const ProjectSecretsDecBundleName = "_project"

// DefaultServerURL 为 Bitwarden 美国公有云 vault 地址。
const DefaultServerURL = "https://vault.bitwarden.com"

const secretsConfigHeader = `# Bitwarden secrets 连接配置
# server_url: Bitwarden 服务器地址
#   美国公有云（默认）: https://vault.bitwarden.com
#   欧盟公有云:         https://vault.bitwarden.eu
#   自托管示例:         https://vault.example.com
# email: 登录邮箱（web unlock 成功后自动写入）

`

// Config 对应 ~/.dec/secrets/config.yaml。
type Config struct {
	ServerURL      string          `yaml:"server_url"`
	Email          string          `yaml:"email"`
	ProjectSecrets string          `yaml:"project_secrets,omitempty"`
	Bundles        []BundleBinding `yaml:"bundles,omitempty"`
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
	applyConfigDefaults(cfg)
	return cfg, nil
}

// SaveConfig 写入 ~/.dec/secrets/config.yaml。
func SaveConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("secrets 配置不能为空")
	}
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
// 占位邮箱（如 user@example.com）不会写入，避免测试或 web unlock 误覆盖真实配置。
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

// defaultSecretsBundleName 返回未显式配置时的默认 Bitwarden folder 名。
// 少数 Dec bundle 与 secrets bundle 并不同名（如 vikunja ↔ vikunja_workflow）。
func defaultSecretsBundleName(decBundleName string) string {
	switch strings.TrimSpace(decBundleName) {
	case "vikunja":
		return "vikunja_workflow"
	default:
		return decBundleName
	}
}

// ResolveBinding 解析 Dec bundle 对应的 secrets 绑定；未显式配置时使用 defaultSecretsBundleName。
func (c *Config) ResolveBinding(decBundleName string) BundleBinding {
	for _, b := range c.Bundles {
		if b.DecBundleName == decBundleName {
			return normalizeBinding(decBundleName, b)
		}
	}
	return normalizeBinding(decBundleName, BundleBinding{DecBundleName: decBundleName})
}

// ProjectSecretsName 返回显式配置的 project secrets 名（Bitwarden folder / `.secrets/` 子目录）。
func (c *Config) ProjectSecretsName() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.ProjectSecrets)
}

// ResolveProjectSecrets 解析 project 级 secrets 同步目标。
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
// 不在 Pull/Push 流程中自动调用；需显式触发或用于一次性升级。
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
	if applyDefaultBindings(cfg) {
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

func applyDefaultBindings(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	changed := false
	for _, decBundle := range []string{"vikunja"} {
		secretsBundle := defaultSecretsBundleName(decBundle)
		if secretsBundle == decBundle {
			continue
		}
		found := false
		for _, b := range cfg.Bundles {
			if b.DecBundleName == decBundle {
				found = true
				break
			}
		}
		if found {
			continue
		}
		cfg.Bundles = append(cfg.Bundles, BundleBinding{
			DecBundleName:     decBundle,
			SecretsBundleName: secretsBundle,
		})
		changed = true
	}
	return changed
}
