package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

// ========================================
// 项目配置管理 (<project>/.dec/)
// ========================================

// ProjectConfigManager 项目配置管理器
type ProjectConfigManager struct {
	projectRoot string
}

// NewProjectConfigManager 创建项目配置管理器
func NewProjectConfigManager(projectRoot string) *ProjectConfigManager {
	return &ProjectConfigManager{projectRoot: projectRoot}
}

// GetDecDir 获取项目 .dec/ 目录
func (m *ProjectConfigManager) GetDecDir() string {
	return filepath.Join(m.projectRoot, ".dec")
}

// Exists 检查项目配置是否已存在
func (m *ProjectConfigManager) Exists() bool {
	_, err := os.Stat(filepath.Join(m.GetDecDir(), "config.yaml"))
	return err == nil
}

// ========================================
// 项目配置 (.dec/config.yaml)
// ========================================

// LoadProjectConfig 加载项目配置
func (m *ProjectConfigManager) LoadProjectConfig() (*types.ProjectConfig, error) {
	configPath := filepath.Join(m.GetDecDir(), "config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.ProjectConfig{}, nil
		}
		return nil, fmt.Errorf("读取项目配置失败: %w", err)
	}

	var config types.ProjectConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析项目配置失败: %w", err)
	}

	return &config, nil
}

// SaveProjectConfig 保存项目配置
func (m *ProjectConfigManager) SaveProjectConfig(config *types.ProjectConfig) error {
	decDir := m.GetDecDir()
	if err := os.MkdirAll(decDir, 0755); err != nil {
		return fmt.Errorf("创建 .dec 目录失败: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化项目配置失败: %w", err)
	}

	header := "# Dec 项目配置\n# vaults: 关联的 vault 列表\n# ides: IDE 覆盖配置（留空则使用全局默认）\n\n"
	configPath := filepath.Join(decDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("写入项目配置失败: %w", err)
	}

	return nil
}

// AddVault 添加 vault 到项目配置
func (m *ProjectConfigManager) AddVault(vaultName string) error {
	config, err := m.LoadProjectConfig()
	if err != nil {
		return err
	}

	// 去重
	for _, v := range config.Vaults {
		if v == vaultName {
			return nil // 已存在
		}
	}
	config.Vaults = append(config.Vaults, vaultName)

	return m.SaveProjectConfig(config)
}

// ========================================
// 资产追踪 (.dec/assets.yaml)
// ========================================

// LoadAssetsConfig 加载资产追踪配置
func (m *ProjectConfigManager) LoadAssetsConfig() (*types.AssetsConfig, error) {
	assetsPath := filepath.Join(m.GetDecDir(), "assets.yaml")

	data, err := os.ReadFile(assetsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.AssetsConfig{}, nil
		}
		return nil, fmt.Errorf("读取资产配置失败: %w", err)
	}

	var config types.AssetsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析资产配置失败: %w", err)
	}

	return &config, nil
}

// SaveAssetsConfig 保存资产追踪配置
func (m *ProjectConfigManager) SaveAssetsConfig(config *types.AssetsConfig) error {
	decDir := m.GetDecDir()
	if err := os.MkdirAll(decDir, 0755); err != nil {
		return fmt.Errorf("创建 .dec 目录失败: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化资产配置失败: %w", err)
	}

	header := "# Dec 已安装资产\n# 由 dec vault pull/remove 自动管理，请勿手动编辑\n\n"
	assetsPath := filepath.Join(decDir, "assets.yaml")
	if err := os.WriteFile(assetsPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("写入资产配置失败: %w", err)
	}

	return nil
}

// ========================================
// 初始化项目
// ========================================

// InitProject 初始化项目 Dec 配置
func (m *ProjectConfigManager) InitProject(vaultName string, ides []string) error {
	decDir := m.GetDecDir()
	if err := os.MkdirAll(decDir, 0755); err != nil {
		return fmt.Errorf("创建 .dec 目录失败: %w", err)
	}

	// 创建 config.yaml
	config := &types.ProjectConfig{
		Vaults: []string{vaultName},
		IDEs:   ides,
	}
	if err := m.SaveProjectConfig(config); err != nil {
		return err
	}

	// 创建空的 assets.yaml
	if err := m.SaveAssetsConfig(&types.AssetsConfig{}); err != nil {
		return err
	}

	// 确保 .dec 被添加到 .gitignore
	if err := m.ensureGitignore(); err != nil {
		return err
	}

	return nil
}

// ensureGitignore 确保 .dec/ 在项目 .gitignore 中
func (m *ProjectConfigManager) ensureGitignore() error {
	gitignorePath := filepath.Join(m.projectRoot, ".gitignore")

	content := ""
	if data, err := os.ReadFile(gitignorePath); err == nil {
		content = string(data)
	}

	// 检查是否已有 .dec/ 规则
	for _, line := range filepath.SplitList(content) {
		if line == ".dec/" || line == ".dec" {
			return nil
		}
	}

	// 追加 .dec/ 到 .gitignore
	entry := "\n# Dec 项目配置\n.dec/\n"
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("写入 .gitignore 失败: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("写入 .gitignore 失败: %w", err)
	}

	return nil
}
