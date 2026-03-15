package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/paths"
	"gopkg.in/yaml.v3"
)

// GlobalConfig 全局配置结构
type GlobalConfig struct {
	// VaultSource 个人知识仓库地址（GitHub 仓库 URL）
	VaultSource string `yaml:"vault_source,omitempty"`
}

// GetGlobalConfigPath 获取全局配置文件路径
func GetGlobalConfigPath() (string, error) {
	rootDir, err := paths.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "config.yaml"), nil
}

// LoadGlobalConfig 加载全局配置
func LoadGlobalConfig() (*GlobalConfig, error) {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return nil, fmt.Errorf("获取配置路径失败: %w", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &GlobalConfig{}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config GlobalConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &config, nil
}

// SaveGlobalConfig 保存全局配置
func SaveGlobalConfig(config *GlobalConfig) error {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return fmt.Errorf("获取配置路径失败: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	header := `# Dec 全局配置
# vault_source: 个人知识仓库地址（GitHub 仓库 URL）

`
	content := header + string(data)

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// SetVaultSource 设置 vault 仓库地址
func SetVaultSource(source string) error {
	config, err := LoadGlobalConfig()
	if err != nil {
		return err
	}
	config.VaultSource = source
	return SaveGlobalConfig(config)
}

// GetVaultSource 获取 vault 仓库地址
func GetVaultSource() (string, error) {
	config, err := LoadGlobalConfig()
	if err != nil {
		return "", err
	}
	return config.VaultSource, nil
}
