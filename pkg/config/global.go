package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

// ========================================
// 全局配置 (~/.dec/config.yaml)
// ========================================

// GetGlobalConfigPath 获取全局配置文件路径
func GetGlobalConfigPath() (string, error) {
	rootDir, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "config.yaml"), nil
}

// LoadGlobalConfig 加载全局配置
func LoadGlobalConfig() (*types.GlobalConfig, error) {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &types.GlobalConfig{}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取全局配置失败: %w", err)
	}

	var config types.GlobalConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析全局配置失败: %w", err)
	}

	return &config, nil
}

// SaveGlobalConfig 保存全局配置
func SaveGlobalConfig(config *types.GlobalConfig) error {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	header := "# Dec 全局配置\n\n"
	if err := os.WriteFile(configPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("写入全局配置失败: %w", err)
	}

	return nil
}

// SetRepoURL 设置仓库 URL
func SetRepoURL(url string) error {
	config, err := LoadGlobalConfig()
	if err != nil {
		return err
	}
	config.RepoURL = url
	return SaveGlobalConfig(config)
}

// ========================================
// 本机 IDE 配置 (~/.dec/local/config.yaml)
// ========================================

// GetLocalConfigPath 获取本机配置文件路径
func GetLocalConfigPath() (string, error) {
	rootDir, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "local", "config.yaml"), nil
}

// LoadLocalConfig 加载本机 IDE 配置
func LoadLocalConfig() (*types.LocalConfig, error) {
	configPath, err := GetLocalConfigPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &types.LocalConfig{}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取本机配置失败: %w", err)
	}

	var config types.LocalConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析本机配置失败: %w", err)
	}

	return &config, nil
}

// SaveLocalConfig 保存本机 IDE 配置
func SaveLocalConfig(config *types.LocalConfig) error {
	configPath, err := GetLocalConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	header := "# Dec 本机 IDE 配置\n# 全局默认 IDE 列表，项目级可覆盖\n\n"
	if err := os.WriteFile(configPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("写入本机配置失败: %w", err)
	}

	return nil
}

// GetEffectiveIDEs 获取有效的 IDE 列表（项目级覆盖全局）
func GetEffectiveIDEs(projectConfig *types.ProjectConfig) ([]string, error) {
	// 项目级有配置则优先
	if projectConfig != nil && len(projectConfig.IDEs) > 0 {
		return projectConfig.IDEs, nil
	}

	// 回退到全局配置
	localConfig, err := LoadLocalConfig()
	if err != nil {
		return nil, err
	}
	if len(localConfig.IDEs) > 0 {
		return localConfig.IDEs, nil
	}

	// 默认 cursor
	return []string{"cursor"}, nil
}

// ========================================
// 系统配置（用于版本更新）
// ========================================

// SystemConfig 系统配置
type SystemConfig struct {
	RepoOwner   string
	RepoName    string
	VersionURL  string
	UpdateBranch string
}

// GetSystemConfig 获取系统配置（返回默认值）
func GetSystemConfig() *SystemConfig {
	return &SystemConfig{
		RepoOwner:    "shichao402",
		RepoName:     "Dec",
		VersionURL:   "https://api.github.com/repos/shichao402/Dec/releases/latest",
		UpdateBranch: "main",
	}
}

// GetVersionURL 获取版本检查 URL
func GetVersionURL() string {
	return "https://api.github.com/repos/shichao402/Dec/releases/latest"
}
