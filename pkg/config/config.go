package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Config 用户配置（用户可自定义的设置）
type Config struct {
	// RegistryURL 自定义 registry 地址，支持镜像源
	RegistryURL string `json:"registry_url,omitempty"`
}

var (
	globalConfig *Config
	configOnce   sync.Once
)

// GetConfigPath 获取用户配置文件路径
func GetConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// 优先使用环境变量
	if rootDir := os.Getenv("DEC_HOME"); rootDir != "" {
		return filepath.Join(rootDir, "config", "settings.json"), nil
	}

	return filepath.Join(homeDir, ".decs", "config", "settings.json"), nil
}

// Load 加载用户配置文件
func Load() (*Config, error) {
	var loadErr error
	configOnce.Do(func() {
		globalConfig = &Config{}

		path, err := GetConfigPath()
		if err != nil {
			loadErr = err
			return
		}

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

// Get 获取用户配置（如果未加载则使用默认值）
func Get() *Config {
	cfg, _ := Load()
	if cfg == nil {
		return &Config{}
	}
	return cfg
}

// Save 保存用户配置到文件
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
// 优先级：环境变量 > 用户配置 > 系统配置
func GetRegistryURL() string {
	// 1. 环境变量优先
	if url := os.Getenv("DEC_REGISTRY"); url != "" {
		return url
	}

	// 2. 用户配置
	cfg := Get()
	if cfg.RegistryURL != "" {
		return cfg.RegistryURL
	}

	// 3. 系统配置（从 system.json 读取）
	return GetDefaultRegistryURL()
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
