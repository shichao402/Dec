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

const projectVarsTemplate = `# Dec 项目变量定义
# 资产模板中的 {{VAR_NAME}} 会在 dec pull 时替换
# 优先级：assets.<type>.<name>.vars > vars > 机器级变量 (~/.dec/local/vars.yaml)

vars:
  # API_BASE_URL: "https://api.example.com"
  # API_TOKEN: "<TOKEN>"

assets:
  skill:
    # my-skill:
    #   vars:
    #     API_TOKEN: "<TOKEN>"
  rule:
    # my-rule:
    #   vars:
    #     API_BASE_URL: "https://api.example.com"
  mcp:
    # my-mcp:
    #   vars:
    #     API_TOKEN: "<TOKEN>"
`

// NewProjectConfigManager 创建项目配置管理器
func NewProjectConfigManager(projectRoot string) *ProjectConfigManager {
	return &ProjectConfigManager{projectRoot: projectRoot}
}

// GetDecDir 获取项目 .dec/ 目录
func (m *ProjectConfigManager) GetDecDir() string {
	return filepath.Join(m.projectRoot, ".dec")
}

// GetVarsPath 获取项目变量定义文件路径
func (m *ProjectConfigManager) GetVarsPath() string {
	return filepath.Join(m.GetDecDir(), "vars.yaml")
}

// Exists 检查项目配置是否已存在
func (m *ProjectConfigManager) Exists() bool {
	_, err := os.Stat(filepath.Join(m.GetDecDir(), "config.yaml"))
	return err == nil
}

// ========================================
// 项目配置 (.dec/config.yaml)
// ========================================

// LoadProjectConfig 加载项目配置，自动去重
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
		return nil, fmt.Errorf("解析项目配置失败: %w\n\n请检查 %s 的 YAML 格式是否正确", err, configPath)
	}

	// 自动去重（同名以靠后的为准）
	if config.Available != nil {
		config.Available.Dedup()
	}
	if config.Enabled != nil {
		config.Enabled.Dedup()
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

	header := "# Dec 项目配置\n# ides: 项目级 IDE 覆盖（可选），例如：\n#   ides:\n#     - cursor\n#     - codex\n# editor: 项目级交互式编辑器，覆盖全局配置（可选），例如：\n#   editor: code --wait\n#   editor: vim\n# available: 仓库中所有可用资产（dec config init 自动生成）\n# enabled: 已启用资产（从 available 复制到这里即为启用）\n\n"
	configPath := filepath.Join(decDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("写入项目配置失败: %w", err)
	}

	return nil
}

// EnsureVarsConfigTemplate 确保项目变量定义模板存在，不覆盖已有文件。
func (m *ProjectConfigManager) EnsureVarsConfigTemplate() (bool, error) {
	decDir := m.GetDecDir()
	if err := os.MkdirAll(decDir, 0755); err != nil {
		return false, fmt.Errorf("创建 .dec 目录失败: %w", err)
	}

	varsPath := m.GetVarsPath()
	if _, err := os.Stat(varsPath); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("检查变量定义文件失败: %w", err)
	}

	if err := os.WriteFile(varsPath, []byte(projectVarsTemplate), 0644); err != nil {
		return false, fmt.Errorf("写入变量定义模板失败: %w", err)
	}

	return true, nil
}

// ========================================
// 项目变量定义 (.dec/vars.yaml)
// ========================================

// LoadVarsConfig 加载项目变量定义
func (m *ProjectConfigManager) LoadVarsConfig() (*types.VarsConfig, error) {
	varsPath := m.GetVarsPath()

	data, err := os.ReadFile(varsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.VarsConfig{}, nil
		}
		return nil, fmt.Errorf("读取变量定义失败: %w", err)
	}

	var cfg types.VarsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析变量定义失败: %w", err)
	}

	return &cfg, nil
}
