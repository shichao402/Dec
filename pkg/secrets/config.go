package secrets

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/repo"
	"gopkg.in/yaml.v3"
)

// Config 对应 ~/.dec/secrets/config.yaml。
type Config struct {
	ServerURL string          `yaml:"server_url"`
	Email     string          `yaml:"email"`
	Bundles   []BundleBinding `yaml:"bundles,omitempty"`
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

// LoadConfig 读取 secrets 配置；文件不存在时返回零值 Config。
func LoadConfig() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("读取 secrets 配置失败: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析 secrets 配置失败: %w", err)
	}
	return cfg, nil
}

// IsConfigured 判定是否已配置 Bitwarden 连接（server_url 非空）。
func IsConfigured() (bool, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return false, err
	}
	return cfg.ServerURL != "", nil
}

// CanAuthenticate 判定是否具备 web unlock / API 登录所需字段。
func (c *Config) CanAuthenticate() bool {
	return c != nil && c.ServerURL != "" && c.Email != ""
}

// ResolveBinding 解析 Dec bundle 对应的 secrets 绑定；未显式配置时默认同名。
func (c *Config) ResolveBinding(decBundleName string) BundleBinding {
	for _, b := range c.Bundles {
		if b.DecBundleName == decBundleName {
			return normalizeBinding(decBundleName, b)
		}
	}
	return normalizeBinding(decBundleName, BundleBinding{DecBundleName: decBundleName})
}

func normalizeBinding(decBundleName string, b BundleBinding) BundleBinding {
	if b.DecBundleName == "" {
		b.DecBundleName = decBundleName
	}
	if b.SecretsBundleName == "" {
		b.SecretsBundleName = b.DecBundleName
	}
	if b.BitwardenFolder == "" {
		b.BitwardenFolder = b.SecretsBundleName
	}
	return b
}
