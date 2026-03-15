package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

// ProjectConfigManagerV2 项目配置管理器
type ProjectConfigManagerV2 struct {
	projectRoot string
}

// NewProjectConfigManagerV2 创建项目配置管理器
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

// LoadVaultConfig 加载 Vault 配置
func (m *ProjectConfigManagerV2) LoadVaultConfig() (*types.VaultConfigV2, error) {
	configPath := paths.GetVaultConfigPath(m.projectRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.VaultConfigV2{}, nil
		}
		return nil, fmt.Errorf("读取 Vault 配置失败: %w", err)
	}

	var config types.VaultConfigV2
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析 Vault 配置失败: %w", err)
	}

	return &config, nil
}

// SaveVaultConfig 保存 Vault 配置
func (m *ProjectConfigManagerV2) SaveVaultConfig(config *types.VaultConfigV2) error {
	configPath := paths.GetVaultConfigPath(m.projectRoot)
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	normalized := normalizeVaultConfig(config)
	content := renderVaultConfig(normalized)
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入 Vault 配置失败: %w", err)
	}

	return nil
}

// EnsureVaultItem 确保资产已声明在 vault.yaml 中
func (m *ProjectConfigManagerV2) EnsureVaultItem(itemType, name string) error {
	config, err := m.LoadVaultConfig()
	if err != nil {
		return err
	}

	switch itemType {
	case "skill":
		config.VaultSkills = appendUnique(config.VaultSkills, name)
	case "rule":
		config.VaultRules = appendUnique(config.VaultRules, name)
	case "mcp":
		config.VaultMCPs = appendUnique(config.VaultMCPs, name)
	default:
		return fmt.Errorf("不支持的资产类型: %s", itemType)
	}

	return m.SaveVaultConfig(config)
}

// ========================================
// 初始化项目
// ========================================

// InitProject 初始化项目配置
func (m *ProjectConfigManagerV2) InitProject(ides []string) error {
	if err := m.createIDEsConfig(ides); err != nil {
		return err
	}

	if err := m.createVaultConfig(); err != nil {
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

// createVaultConfig 创建 Vault 配置
func (m *ProjectConfigManagerV2) createVaultConfig() error {
	return m.SaveVaultConfig(&types.VaultConfigV2{})
}

func normalizeVaultConfig(config *types.VaultConfigV2) *types.VaultConfigV2 {
	if config == nil {
		return &types.VaultConfigV2{}
	}

	return &types.VaultConfigV2{
		VaultSkills: normalizeStringList(config.VaultSkills),
		VaultRules:  normalizeStringList(config.VaultRules),
		VaultMCPs:   normalizeStringList(config.VaultMCPs),
	}
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return normalizeStringList(values)
	}

	values = append(values, value)
	return normalizeStringList(values)
}

func renderVaultConfig(config *types.VaultConfigV2) string {
	var sb strings.Builder
	sb.WriteString("# Vault 配置\n")
	sb.WriteString("# 声明本项目需要从个人知识仓库同步的资产\n")
	sb.WriteString("# 运行 dec sync 时会自动从 Vault 拉取这里列出的内容\n")
	sb.WriteString("#\n")
	sb.WriteString("# 使用 dec vault list 查看所有可用资产\n\n")

	writeVaultSection(&sb, "Skills（Agent 能力包）", "vault_skills", config.VaultSkills, []string{
		"create-api-test",
		"fix-cors-issue",
	})
	writeVaultSection(&sb, "Rules（Agent 行为规则）", "vault_rules", config.VaultRules, []string{
		"my-security-rule",
		"my-code-style",
	})
	writeVaultSection(&sb, "MCPs（外部工具配置）", "vault_mcps", config.VaultMCPs, []string{
		"my-database-mcp",
	})

	return sb.String()
}

func writeVaultSection(sb *strings.Builder, title, key string, values, samples []string) {
	sb.WriteString(fmt.Sprintf("# %s\n", title))
	sb.WriteString(fmt.Sprintf("%s:\n", key))

	if len(values) == 0 {
		for _, sample := range samples {
			sb.WriteString(fmt.Sprintf("  # - %s\n", sample))
		}
		sb.WriteString("\n")
		return
	}

	for _, value := range values {
		sb.WriteString(fmt.Sprintf("  - %s\n", value))
	}
	sb.WriteString("\n")
}
