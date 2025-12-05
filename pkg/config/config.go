package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// 默认值
const (
	// RepoOwner 仓库所有者
	RepoOwner = "shichao402"
	// RepoName 仓库名称
	RepoName = "CursorToolset"

	// RepoURL 仓库地址
	RepoURL = "https://github.com/" + RepoOwner + "/" + RepoName
	// RepoGitURL 仓库 Git 地址
	RepoGitURL = RepoURL + ".git"
	// DefaultRegistryURL 默认的 registry 下载地址
	DefaultRegistryURL = RepoURL + "/releases/download/registry/registry.json"
)

// Config 用户配置
type Config struct {
	// RegistryURL 自定义 registry 地址，支持镜像源
	RegistryURL string `json:"registry_url,omitempty"`
}

var (
	globalConfig *Config
	configOnce   sync.Once
	configPath   string
)

// GetConfigPath 获取配置文件路径
func GetConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".cursortoolsets", "config", "settings.json"), nil
}

// Load 加载配置文件
func Load() (*Config, error) {
	var loadErr error
	configOnce.Do(func() {
		globalConfig = &Config{}

		path, err := GetConfigPath()
		if err != nil {
			loadErr = err
			return
		}
		configPath = path

		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				// 配置文件不存在，使用默认值
				return
			}
			loadErr = err
			return
		}

		if err := json.Unmarshal(data, globalConfig); err != nil {
			loadErr = err
			return
		}
	})

	return globalConfig, loadErr
}

// Get 获取全局配置（如果未加载则使用默认值）
func Get() *Config {
	cfg, _ := Load()
	if cfg == nil {
		return &Config{}
	}
	return cfg
}

// Save 保存配置到文件
func Save(cfg *Config) error {
	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// GetRegistryURL 获取 Registry URL
// 优先级：环境变量 > 配置文件 > 默认值
func GetRegistryURL() string {
	// 1. 环境变量优先
	if url := os.Getenv("CURSOR_TOOLSET_REGISTRY"); url != "" {
		return url
	}

	// 2. 配置文件
	cfg := Get()
	if cfg.RegistryURL != "" {
		return cfg.RegistryURL
	}

	// 3. 默认值
	return DefaultRegistryURL
}

// SetRegistryURL 设置 Registry URL 并保存
func SetRegistryURL(url string) error {
	cfg := Get()
	cfg.RegistryURL = url
	return Save(cfg)
}

// ResetRegistryURL 重置为默认 Registry URL
func ResetRegistryURL() error {
	cfg := Get()
	cfg.RegistryURL = ""
	return Save(cfg)
}
