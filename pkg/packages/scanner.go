package packages

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReservedCategories 保留的规则分类
var ReservedCategories = []string{
	"core",       // 核心规则，总是注入
	"languages",  // 编程语言
	"frameworks", // 框架
	"platforms",  // 目标平台
	"patterns",   // 设计模式
}

// RuleInfo 规则信息
type RuleInfo struct {
	Name        string // 规则名称（不含扩展名）
	Category    string // 分类（如 core, languages, frameworks）
	FilePath    string // 文件完整路径
	Description string // 描述（从文件头解析）
	IsCore      bool   // 是否是核心规则
}

// MCPInfo MCP 信息
type MCPInfo struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	FilePath    string            // 文件完整路径
}

// Scanner 包扫描器
type Scanner struct {
	packagesDir string
	customDir   string
}

// NewScannerWithDirs 使用指定目录创建扫描器
func NewScannerWithDirs(packagesDir, customDir string) *Scanner {
	return &Scanner{
		packagesDir: packagesDir,
		customDir:   customDir,
	}
}

// ScanRules 扫描所有规则
func (s *Scanner) ScanRules() ([]RuleInfo, error) {
	var rules []RuleInfo

	// 扫描包缓存中的规则
	packagesRulesDir := filepath.Join(s.packagesDir, "rules")
	if _, err := os.Stat(packagesRulesDir); err == nil {
		packageRules, err := s.scanRulesDir(packagesRulesDir)
		if err != nil {
			return nil, fmt.Errorf("扫描包规则失败: %w", err)
		}
		rules = append(rules, packageRules...)
	}

	// 扫描用户自定义规则
	if s.customDir != "" {
		customRulesDir := filepath.Join(s.customDir, "rules")
		if _, err := os.Stat(customRulesDir); err == nil {
			customRules, err := s.scanRulesDir(customRulesDir)
			if err != nil {
				return nil, fmt.Errorf("扫描自定义规则失败: %w", err)
			}
			rules = append(rules, customRules...)
		}
	}

	return rules, nil
}

// scanRulesDir 扫描规则目录
func (s *Scanner) scanRulesDir(rulesDir string) ([]RuleInfo, error) {
	var rules []RuleInfo

	// 遍历分类目录
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		category := entry.Name()
		categoryDir := filepath.Join(rulesDir, category)

		// 验证分类名称
		if !s.isValidCategory(category) {
			fmt.Printf("警告: 未知的规则分类 '%s'，建议使用 ext- 前缀\n", category)
		}

		// 扫描分类下的规则文件
		ruleFiles, err := os.ReadDir(categoryDir)
		if err != nil {
			continue
		}

		for _, ruleFile := range ruleFiles {
			if ruleFile.IsDir() || !strings.HasSuffix(ruleFile.Name(), ".mdc") {
				continue
			}

			ruleName := strings.TrimSuffix(ruleFile.Name(), ".mdc")
			filePath := filepath.Join(categoryDir, ruleFile.Name())

			rule := RuleInfo{
				Name:     ruleName,
				Category: category,
				FilePath: filePath,
				IsCore:   category == "core",
			}

			// 尝试解析描述
			if desc, err := s.parseRuleDescription(filePath); err == nil {
				rule.Description = desc
			}

			rules = append(rules, rule)
		}
	}

	return rules, nil
}

// isValidCategory 检查分类名称是否有效
func (s *Scanner) isValidCategory(category string) bool {
	// 检查是否是保留分类
	for _, reserved := range ReservedCategories {
		if category == reserved {
			return true
		}
	}

	// 检查是否是扩展分类
	if strings.HasPrefix(category, "ext-") {
		return true
	}

	return false
}

// parseRuleDescription 从规则文件解析描述
func (s *Scanner) parseRuleDescription(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	content := string(data)

	// 查找 YAML front matter
	if strings.HasPrefix(content, "---") {
		endIndex := strings.Index(content[3:], "---")
		if endIndex > 0 {
			frontMatter := content[3 : endIndex+3]
			// 简单解析 description 字段
			for _, line := range strings.Split(frontMatter, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "description:") {
					return strings.TrimSpace(strings.TrimPrefix(line, "description:")), nil
				}
			}
		}
	}

	// 查找第一个标题作为描述
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# "), nil
		}
	}

	return "", nil
}

// ScanMCPs 扫描所有 MCP
func (s *Scanner) ScanMCPs() ([]MCPInfo, error) {
	var mcps []MCPInfo

	// 扫描包缓存中的 MCP
	packagesMCPDir := filepath.Join(s.packagesDir, "mcp")
	if _, err := os.Stat(packagesMCPDir); err == nil {
		packageMCPs, err := s.scanMCPDir(packagesMCPDir)
		if err != nil {
			return nil, fmt.Errorf("扫描包 MCP 失败: %w", err)
		}
		mcps = append(mcps, packageMCPs...)
	}

	// 扫描用户自定义 MCP
	if s.customDir != "" {
		customMCPDir := filepath.Join(s.customDir, "mcp")
		if _, err := os.Stat(customMCPDir); err == nil {
			customMCPs, err := s.scanMCPDir(customMCPDir)
			if err != nil {
				return nil, fmt.Errorf("扫描自定义 MCP 失败: %w", err)
			}
			mcps = append(mcps, customMCPs...)
		}
	}

	return mcps, nil
}

// scanMCPDir 扫描 MCP 目录
func (s *Scanner) scanMCPDir(mcpDir string) ([]MCPInfo, error) {
	var mcps []MCPInfo

	entries, err := os.ReadDir(mcpDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(mcpDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var mcp MCPInfo
		if err := json.Unmarshal(data, &mcp); err != nil {
			continue
		}

		mcp.FilePath = filePath
		if mcp.Name == "" {
			mcp.Name = strings.TrimSuffix(entry.Name(), ".json")
		}

		mcps = append(mcps, mcp)
	}

	return mcps, nil
}

// GetCategories 获取所有规则分类
func (s *Scanner) GetCategories() ([]string, error) {
	categorySet := make(map[string]bool)

	// 扫描包缓存
	packagesRulesDir := filepath.Join(s.packagesDir, "rules")
	if entries, err := os.ReadDir(packagesRulesDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				categorySet[entry.Name()] = true
			}
		}
	}

	// 扫描用户自定义
	if s.customDir != "" {
		customRulesDir := filepath.Join(s.customDir, "rules")
		if entries, err := os.ReadDir(customRulesDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					categorySet[entry.Name()] = true
				}
			}
		}
	}

	var categories []string
	for cat := range categorySet {
		categories = append(categories, cat)
	}

	return categories, nil
}

// HasPackages 检查是否有可用的包
func (s *Scanner) HasPackages() bool {
	// 检查包缓存目录是否存在
	if _, err := os.Stat(s.packagesDir); os.IsNotExist(err) {
		return false
	}

	// 检查是否有规则或 MCP
	rulesDir := filepath.Join(s.packagesDir, "rules")
	mcpDir := filepath.Join(s.packagesDir, "mcp")

	hasRules := false
	hasMCP := false

	if entries, err := os.ReadDir(rulesDir); err == nil && len(entries) > 0 {
		hasRules = true
	}

	if entries, err := os.ReadDir(mcpDir); err == nil && len(entries) > 0 {
		hasMCP = true
	}

	return hasRules || hasMCP
}
