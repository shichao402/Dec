package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/packages"
	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

// ========================================
// 新版项目配置管理（支持占位符变量）
// ========================================

// ProjectConfigManagerV2 新版项目配置管理器
type ProjectConfigManagerV2 struct {
	projectRoot string
}

// NewProjectConfigManagerV2 创建新版项目配置管理器
func NewProjectConfigManagerV2(projectRoot string) *ProjectConfigManagerV2 {
	return &ProjectConfigManagerV2{projectRoot: projectRoot}
}

// GetConfigDir 获取项目配置目录
func (m *ProjectConfigManagerV2) GetConfigDir() string {
	return paths.GetProjectConfigDir(m.projectRoot)
}

// Exists 检查项目配置是否存在
func (m *ProjectConfigManagerV2) Exists() bool {
	configPath := paths.GetIDEsConfigPath(m.projectRoot)
	_, err := os.Stat(configPath)
	return err == nil
}

// ========================================
// 加载配置
// ========================================

// LoadTechnologyConfig 加载技术栈配置
func (m *ProjectConfigManagerV2) LoadTechnologyConfig() (*types.NewTechnologyConfigV2, error) {
	configPath := paths.GetTechnologyConfigPath(m.projectRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.NewTechnologyConfigV2{}, nil
		}
		return nil, fmt.Errorf("读取技术栈配置失败: %w", err)
	}

	var config types.NewTechnologyConfigV2
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析技术栈配置失败: %w", err)
	}

	return &config, nil
}

// LoadMCPConfig 加载 MCP 配置
func (m *ProjectConfigManagerV2) LoadMCPConfig() (*types.NewMCPConfigV2, error) {
	configPath := paths.GetMCPConfigPath(m.projectRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.NewMCPConfigV2{}, nil
		}
		return nil, fmt.Errorf("读取 MCP 配置失败: %w", err)
	}

	var config types.NewMCPConfigV2
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析 MCP 配置失败: %w", err)
	}

	return &config, nil
}

// LoadIDEsConfig 加载 IDE 配置
func (m *ProjectConfigManagerV2) LoadIDEsConfig() (*types.IDEsConfig, error) {
	configPath := paths.GetIDEsConfigPath(m.projectRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.IDEsConfig{IDEs: []string{"cursor"}}, nil
		}
		return nil, fmt.Errorf("读取 IDE 配置失败: %w", err)
	}

	var config types.IDEsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析 IDE 配置失败: %w", err)
	}

	if len(config.IDEs) == 0 {
		config.IDEs = []string{"cursor"}
	}

	return &config, nil
}

// ========================================
// 初始化项目
// ========================================

// InitProject 初始化项目配置
func (m *ProjectConfigManagerV2) InitProject(ides []string) error {
	// 创建 IDE 配置
	if err := m.createIDEsConfig(ides); err != nil {
		return err
	}

	// 创建技术栈配置（根据扫描的包生成）
	if err := m.createTechnologyConfig(); err != nil {
		return err
	}

	// 创建 MCP 配置（根据扫描的包生成）
	if err := m.createMCPConfig(); err != nil {
		return err
	}

	return nil
}

// createIDEsConfig 创建 IDE 配置
func (m *ProjectConfigManagerV2) createIDEsConfig(ides []string) error {
	configPath := paths.GetIDEsConfigPath(m.projectRoot)

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	enabledSet := make(map[string]bool)
	for _, ide := range ides {
		enabledSet[ide] = true
	}
	if len(enabledSet) == 0 {
		enabledSet["cursor"] = true
	}

	var sb strings.Builder
	sb.WriteString("# IDE 配置\n")
	sb.WriteString("# 取消注释启用对应 IDE\n\n")
	sb.WriteString("ides:\n")

	availableIDEs := []string{"cursor", "codebuddy", "windsurf", "trae"}
	for _, ide := range availableIDEs {
		if enabledSet[ide] {
			sb.WriteString(fmt.Sprintf("  - %s\n", ide))
		} else {
			sb.WriteString(fmt.Sprintf("  # - %s\n", ide))
		}
	}

	return os.WriteFile(configPath, []byte(sb.String()), 0644)
}

// createScanner 创建包扫描器
func (m *ProjectConfigManagerV2) createScanner() (*packages.Scanner, error) {
	packagesDir, err := GetPackagesCacheDir()
	if err != nil {
		return nil, err
	}

	customDir, err := GetCustomDir()
	if err != nil {
		return nil, err
	}

	return packages.NewScannerWithDirs(packagesDir, customDir), nil
}

// createTechnologyConfig 创建技术栈配置
func (m *ProjectConfigManagerV2) createTechnologyConfig() error {
	configPath := paths.GetTechnologyConfigPath(m.projectRoot)

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	// 尝试扫描可用的规则
	scanner, err := m.createScanner()
	if err != nil {
		// 扫描失败，生成空模板
		return m.createEmptyTechnologyConfig(configPath)
	}

	rules, err := scanner.ScanRules()
	if err != nil {
		return m.createEmptyTechnologyConfig(configPath)
	}

	// 按分类组织规则
	categoryRules := make(map[string][]packages.RuleInfo)
	for _, rule := range rules {
		if rule.Category != "core" { // core 规则不需要用户选择
			categoryRules[rule.Category] = append(categoryRules[rule.Category], rule)
		}
	}

	var sb strings.Builder
	sb.WriteString("# 技术栈配置\n")
	sb.WriteString("# 取消注释启用对应选项\n")
	sb.WriteString("# 支持两种格式:\n")
	sb.WriteString("#   - name           # 简单格式\n")
	sb.WriteString("#   - name:          # 带变量格式\n")
	sb.WriteString("#       var1: value1\n\n")

	// 按顺序生成各分类
	categories := []string{"languages", "frameworks", "platforms", "patterns"}
	labels := map[string]string{
		"languages":  "编程语言",
		"frameworks": "框架",
		"platforms":  "目标平台",
		"patterns":   "设计模式",
	}

	for _, cat := range categories {
		sb.WriteString(fmt.Sprintf("# %s\n", labels[cat]))
		sb.WriteString(fmt.Sprintf("%s:\n", cat))

		if rules, ok := categoryRules[cat]; ok && len(rules) > 0 {
			for _, rule := range rules {
				sb.WriteString(fmt.Sprintf("  # - %s\n", rule.Name))
			}
		} else {
			sb.WriteString("  # （暂无可用选项）\n")
		}
		sb.WriteString("\n")
	}

	// 扩展分类
	var extCategories []string
	for cat := range categoryRules {
		isReserved := false
		for _, reserved := range categories {
			if cat == reserved {
				isReserved = true
				break
			}
		}
		if !isReserved {
			extCategories = append(extCategories, cat)
		}
	}

	if len(extCategories) > 0 {
		sort.Strings(extCategories)
		for _, cat := range extCategories {
			sb.WriteString(fmt.Sprintf("# 扩展: %s\n", cat))
			sb.WriteString(fmt.Sprintf("%s:\n", cat))
			for _, rule := range categoryRules[cat] {
				sb.WriteString(fmt.Sprintf("  # - %s\n", rule.Name))
			}
			sb.WriteString("\n")
		}
	}

	return os.WriteFile(configPath, []byte(sb.String()), 0644)
}

// createEmptyTechnologyConfig 创建空的技术栈配置
func (m *ProjectConfigManagerV2) createEmptyTechnologyConfig(configPath string) error {
	template := `# 技术栈配置
# 取消注释启用对应选项
# 请先运行 'dec update' 更新包缓存

# 编程语言
languages:
  # （请先运行 dec update）

# 框架
frameworks:
  # （请先运行 dec update）

# 目标平台
platforms:
  # （请先运行 dec update）

# 设计模式
patterns:
  # （请先运行 dec update）
`
	return os.WriteFile(configPath, []byte(template), 0644)
}

// createMCPConfig 创建 MCP 配置
func (m *ProjectConfigManagerV2) createMCPConfig() error {
	configPath := paths.GetMCPConfigPath(m.projectRoot)

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	// 尝试扫描可用的 MCP
	scanner, err := m.createScanner()
	if err != nil {
		return m.createEmptyMCPConfig(configPath)
	}

	mcps, err := scanner.ScanMCPs()
	if err != nil {
		return m.createEmptyMCPConfig(configPath)
	}

	var sb strings.Builder
	sb.WriteString("# MCP 配置\n")
	sb.WriteString("# 取消注释启用对应 MCP\n")
	sb.WriteString("# 支持两种格式:\n")
	sb.WriteString("#   - name           # 简单格式\n")
	sb.WriteString("#   - name:          # 带变量格式\n")
	sb.WriteString("#       var1: value1\n\n")
	sb.WriteString("mcps:\n")

	if len(mcps) > 0 {
		for _, mcp := range mcps {
			// dec 默认启用
			if mcp.Name == "dec" {
				sb.WriteString(fmt.Sprintf("  - %s\n", mcp.Name))
			} else {
				sb.WriteString(fmt.Sprintf("  # - %s", mcp.Name))
				if mcp.Description != "" {
					sb.WriteString(fmt.Sprintf("  # %s", mcp.Description))
				}
				sb.WriteString("\n")
			}
		}
	} else {
		sb.WriteString("  # （请先运行 dec update）\n")
	}

	return os.WriteFile(configPath, []byte(sb.String()), 0644)
}

// createEmptyMCPConfig 创建空的 MCP 配置
func (m *ProjectConfigManagerV2) createEmptyMCPConfig(configPath string) error {
	template := `# MCP 配置
# 取消注释启用对应 MCP
# 请先运行 'dec update' 更新包缓存

mcps:
  # （请先运行 dec update）
`
	return os.WriteFile(configPath, []byte(template), 0644)
}

// NewScanner 创建包扫描器（供外部使用）
func NewScanner() (*packages.Scanner, error) {
	packagesDir, err := GetPackagesCacheDir()
	if err != nil {
		return nil, err
	}

	customDir, err := GetCustomDir()
	if err != nil {
		return nil, err
	}

	return packages.NewScannerWithDirs(packagesDir, customDir), nil
}
