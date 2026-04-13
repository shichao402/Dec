// Package ide 提供 IDE 抽象层
// 使用策略模式封装不同 IDE 的目录结构差异
package ide

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/types"
)

// IDE 接口定义了不同 IDE 的目录结构和文件操作
type IDE interface {
	// Name 返回 IDE 名称
	Name() string

	// UserRootDir 返回用户级根目录
	UserRootDir(homeDir string) string

	// RulesDir 返回规则输出目录
	RulesDir(projectRoot string) string

	// SkillsDir 返回 Skills 输出目录
	SkillsDir(projectRoot string) string

	// MCPConfigPath 返回 MCP 配置文件路径
	MCPConfigPath(projectRoot string) string

	// WriteRules 写入规则文件到 IDE 目录
	WriteRules(projectRoot string, rules []RuleFile) error

	// WriteSkill 写入单个 Skill 目录到 IDE Skills 目录
	WriteSkill(projectRoot string, skillName string, files []SkillFile) error

	// WriteMCPConfig 写入 MCP 配置到 IDE 目录
	WriteMCPConfig(projectRoot string, config *types.MCPConfig) error

	// LoadMCPConfig 加载现有的 MCP 配置
	LoadMCPConfig(projectRoot string) (*types.MCPConfig, error)
}

// SkillFile 表示 Skill 中的一个文件
type SkillFile struct {
	RelPath string // 相对于 skill 目录的路径
	Content []byte // 文件内容
}

// RuleFile 表示一个规则文件
type RuleFile struct {
	Name    string // 文件名（不含路径）
	Content string // 文件内容
}

// baseIDE 提供 IDE 接口的基础实现
type baseIDE struct {
	name          string
	dirKey        string // 项目级目录名（如 .cursor, .codebuddy）
	userDirKey    string // 用户级目录名；为空时复用 dirKey
	mcpConfigPath string // MCP 配置文件路径（可选，为空则使用默认 {dirKey}/mcp.json）
}

func (b *baseIDE) Name() string {
	return b.name
}

func (b *baseIDE) UserRootDir(homeDir string) string {
	return filepath.Join(homeDir, b.userDirKeyOrDefault())
}

func (b *baseIDE) RulesDir(projectRoot string) string {
	return filepath.Join(projectRoot, b.dirKey, "rules")
}

func (b *baseIDE) SkillsDir(projectRoot string) string {
	return filepath.Join(projectRoot, b.dirKey, "skills")
}

func (b *baseIDE) MCPConfigPath(projectRoot string) string {
	if b.mcpConfigPath != "" {
		return filepath.Join(projectRoot, b.mcpConfigPath)
	}
	return filepath.Join(projectRoot, b.dirKey, "mcp.json")
}

func (b *baseIDE) userDirKeyOrDefault() string {
	if b.userDirKey != "" {
		return b.userDirKey
	}
	return b.dirKey
}

func (b *baseIDE) WriteRules(projectRoot string, rules []RuleFile) error {
	rulesDir := b.RulesDir(projectRoot)

	// 确保目录存在
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return err
	}

	// 写入每个规则文件
	for _, rule := range rules {
		rulePath := filepath.Join(rulesDir, rule.Name)
		if err := os.WriteFile(rulePath, []byte(rule.Content), 0644); err != nil {
			return err
		}
	}

	return nil
}

func (b *baseIDE) WriteSkill(projectRoot string, skillName string, files []SkillFile) error {
	skillDir := filepath.Join(b.SkillsDir(projectRoot), skillName)

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}

	for _, f := range files {
		fullPath := filepath.Join(skillDir, f.RelPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, f.Content, 0644); err != nil {
			return err
		}
	}

	return nil
}

func (b *baseIDE) WriteMCPConfig(projectRoot string, config *types.MCPConfig) error {
	configPath := b.MCPConfigPath(projectRoot)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func (b *baseIDE) LoadMCPConfig(projectRoot string) (*types.MCPConfig, error) {
	configPath := b.MCPConfigPath(projectRoot)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.MCPConfig{MCPServers: make(map[string]types.MCPServer)}, nil
		}
		return nil, err
	}

	var config types.MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if config.MCPServers == nil {
		config.MCPServers = make(map[string]types.MCPServer)
	}

	return &config, nil
}
