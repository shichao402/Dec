package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SystemConfig 系统配置（安装时下载，包含更新源等信息）
type SystemConfig struct {
	// RepoOwner 仓库所有者
	RepoOwner string `json:"repo_owner"`
	// RepoName 仓库名称
	RepoName string `json:"repo_name"`
	// RegistryURL 包索引下载地址
	RegistryURL string `json:"registry_url"`
	// UpdateBranch 更新检查分支
	UpdateBranch string `json:"update_branch"`
}

var (
	systemConfig     *SystemConfig
	systemConfigOnce sync.Once
	systemConfigErr  error
)

// GetSystemConfigPath 获取系统配置文件路径
func GetSystemConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// 优先使用环境变量
	if rootDir := os.Getenv("DEC_HOME"); rootDir != "" {
		return filepath.Join(rootDir, "config", "system.json"), nil
	}

	return filepath.Join(homeDir, ".dec", "config", "system.json"), nil
}

// LoadSystemConfig 加载系统配置
func LoadSystemConfig() (*SystemConfig, error) {
	systemConfigOnce.Do(func() {
		path, err := GetSystemConfigPath()
		if err != nil {
			systemConfigErr = fmt.Errorf("获取系统配置路径失败: %w", err)
			return
		}

		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				// 配置文件不存在，使用内置默认值
				systemConfig = getDefaultSystemConfig()
				return
			}
			systemConfigErr = fmt.Errorf("读取系统配置失败: %w", err)
			return
		}

		systemConfig = &SystemConfig{}
		if err := json.Unmarshal(data, systemConfig); err != nil {
			systemConfigErr = fmt.Errorf("解析系统配置失败: %w", err)
			return
		}

		// 填充缺失的字段
		fillDefaultSystemConfig(systemConfig)
	})

	return systemConfig, systemConfigErr
}

// GetSystemConfig 获取系统配置（如果加载失败则使用默认值）
func GetSystemConfig() *SystemConfig {
	cfg, err := LoadSystemConfig()
	if err != nil || cfg == nil {
		return getDefaultSystemConfig()
	}
	return cfg
}

// getDefaultSystemConfig 返回内置默认配置
func getDefaultSystemConfig() *SystemConfig {
	return &SystemConfig{
		RepoOwner:    "shichao402",
		RepoName:     "Dec",
		RegistryURL:  "https://github.com/shichao402/Dec/releases/download/registry/registry.json",
		UpdateBranch: "ReleaseLatest",
	}
}

// fillDefaultSystemConfig 填充缺失的配置字段
func fillDefaultSystemConfig(cfg *SystemConfig) {
	defaults := getDefaultSystemConfig()
	if cfg.RepoOwner == "" {
		cfg.RepoOwner = defaults.RepoOwner
	}
	if cfg.RepoName == "" {
		cfg.RepoName = defaults.RepoName
	}
	if cfg.RegistryURL == "" {
		cfg.RegistryURL = defaults.RegistryURL
	}
	if cfg.UpdateBranch == "" {
		cfg.UpdateBranch = defaults.UpdateBranch
	}
}

// SaveSystemConfig 保存系统配置
func SaveSystemConfig(cfg *SystemConfig) error {
	path, err := GetSystemConfigPath()
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

// ========================================
// 便捷方法：从系统配置获取各种 URL
// ========================================

// GetRepoURL 获取仓库地址
func GetRepoURL() string {
	cfg := GetSystemConfig()
	return "https://github.com/" + cfg.RepoOwner + "/" + cfg.RepoName
}

// GetRepoGitURL 获取仓库 Git 地址
func GetRepoGitURL() string {
	return GetRepoURL() + ".git"
}

// GetDefaultRegistryURL 获取默认的 Registry URL
func GetDefaultRegistryURL() string {
	cfg := GetSystemConfig()
	return cfg.RegistryURL
}

// GetUpdateBranch 获取更新分支
func GetUpdateBranch() string {
	cfg := GetSystemConfig()
	return cfg.UpdateBranch
}

// GetVersionURL 获取版本信息 URL
func GetVersionURL() string {
	cfg := GetSystemConfig()
	return "https://raw.githubusercontent.com/" + cfg.RepoOwner + "/" + cfg.RepoName + "/" + cfg.UpdateBranch + "/version.json"
}

// GetInstallScriptURL 获取安装脚本 URL
func GetInstallScriptURL() string {
	cfg := GetSystemConfig()
	return "https://raw.githubusercontent.com/" + cfg.RepoOwner + "/" + cfg.RepoName + "/" + cfg.UpdateBranch + "/scripts/install.sh"
}
