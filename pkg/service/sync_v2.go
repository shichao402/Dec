package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/packages"
	"github.com/shichao402/Dec/pkg/types"
)

// SyncServiceV2 新版同步服务
type SyncServiceV2 struct {
	projectRoot string
	configMgr   *config.ProjectConfigManagerV2
	scanner     *packages.Scanner
	parser      *packages.PlaceholderParser
}

// NewSyncServiceV2 创建新版同步服务
func NewSyncServiceV2(projectRoot string) (*SyncServiceV2, error) {
	scanner, err := config.NewScanner()
	if err != nil {
		return nil, err
	}

	return &SyncServiceV2{
		projectRoot: projectRoot,
		configMgr:   config.NewProjectConfigManagerV2(projectRoot),
		scanner:     scanner,
		parser:      packages.NewPlaceholderParser(),
	}, nil
}

// SyncResultV2 同步结果
type SyncResultV2 struct {
	ProjectName    string
	IDEs           []string
	CoreRulesCount int
	TechRulesCount int
	MCPCount       int
}

// Sync 执行同步操作
func (s *SyncServiceV2) Sync() (*SyncResultV2, error) {
	// 检查项目是否已初始化
	if !s.configMgr.Exists() {
		return nil, fmt.Errorf("项目未初始化\n\n💡 运行 dec init 初始化项目")
	}

	// 检查是否有可用的包
	if !s.scanner.HasPackages() {
		return nil, fmt.Errorf("没有可用的包缓存\n\n💡 运行 dec update 更新包缓存")
	}

	// 加载配置
	idesConfig, err := s.configMgr.LoadIDEsConfig()
	if err != nil {
		return nil, fmt.Errorf("加载 IDE 配置失败: %w", err)
	}

	techConfig, err := s.configMgr.LoadTechnologyConfig()
	if err != nil {
		return nil, fmt.Errorf("加载技术栈配置失败: %w", err)
	}

	mcpConfig, err := s.configMgr.LoadMCPConfig()
	if err != nil {
		return nil, fmt.Errorf("加载 MCP 配置失败: %w", err)
	}

	// 扫描所有规则
	allRules, err := s.scanner.ScanRules()
	if err != nil {
		return nil, fmt.Errorf("扫描规则失败: %w", err)
	}

	// 扫描所有 MCP
	allMCPs, err := s.scanner.ScanMCPs()
	if err != nil {
		return nil, fmt.Errorf("扫描 MCP 失败: %w", err)
	}

	// 筛选要注入的规则
	var rulesToInject []packages.RuleInfo
	coreCount := 0
	techCount := 0

	for _, rule := range allRules {
		if rule.IsCore {
			// 核心规则总是注入
			rulesToInject = append(rulesToInject, rule)
			coreCount++
		} else if s.isRuleEnabled(rule, techConfig) {
			// 检查是否在配置中启用
			rulesToInject = append(rulesToInject, rule)
			techCount++
		}
	}

	// 筛选要启用的 MCP
	var mcpsToEnable []packages.MCPInfo
	for _, mcp := range allMCPs {
		if s.isMCPEnabled(mcp.Name, mcpConfig) {
			mcpsToEnable = append(mcpsToEnable, mcp)
		}
	}

	// 为每个 IDE 生成配置
	for _, ideName := range idesConfig.IDEs {
		ideImpl := ide.Get(ideName)

		// 清理旧的托管规则
		if err := s.cleanManagedRules(ideImpl); err != nil {
			return nil, fmt.Errorf("清理 %s 旧规则失败: %w", ideName, err)
		}

		// 生成规则文件
		if err := s.generateRules(ideImpl, rulesToInject, techConfig); err != nil {
			return nil, fmt.Errorf("生成 %s 规则失败: %w", ideName, err)
		}

		// 生成 MCP 配置
		if err := s.generateMCPConfig(ideImpl, mcpsToEnable, mcpConfig); err != nil {
			return nil, fmt.Errorf("生成 %s MCP 配置失败: %w", ideName, err)
		}
	}

	return &SyncResultV2{
		ProjectName:    filepath.Base(s.projectRoot),
		IDEs:           idesConfig.IDEs,
		CoreRulesCount: coreCount,
		TechRulesCount: techCount,
		MCPCount:       len(mcpsToEnable),
	}, nil
}

// isRuleEnabled 检查规则是否在配置中启用
func (s *SyncServiceV2) isRuleEnabled(rule packages.RuleInfo, techConfig *types.NewTechnologyConfigV2) bool {
	enabledNames := techConfig.GetEnabledNames(rule.Category)
	for _, name := range enabledNames {
		if name == rule.Name {
			return true
		}
	}
	return false
}

// isMCPEnabled 检查 MCP 是否在配置中启用
func (s *SyncServiceV2) isMCPEnabled(name string, mcpConfig *types.NewMCPConfigV2) bool {
	for _, item := range mcpConfig.MCPs {
		if item.Name == name {
			return true
		}
	}
	return false
}

// getMCPVars 获取 MCP 的变量配置
func (s *SyncServiceV2) getMCPVars(name string, mcpConfig *types.NewMCPConfigV2) map[string]interface{} {
	for _, item := range mcpConfig.MCPs {
		if item.Name == name {
			return item.Vars
		}
	}
	return nil
}

// generateRules 生成规则文件
func (s *SyncServiceV2) generateRules(ideImpl ide.IDE, rules []packages.RuleInfo, techConfig *types.NewTechnologyConfigV2) error {
	rulesDir := ideImpl.RulesDir(s.projectRoot)
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return err
	}

	for _, rule := range rules {
		// 读取规则内容
		content, err := os.ReadFile(rule.FilePath)
		if err != nil {
			return fmt.Errorf("读取规则 %s 失败: %w", rule.Name, err)
		}

		// 获取变量配置
		vars := techConfig.GetItemVars(rule.Category, rule.Name)

		// 替换占位符
		processedContent := s.parser.Replace(string(content), vars)

		// 生成输出文件名
		outputName := fmt.Sprintf("dec-%s-%s.mdc", rule.Category, rule.Name)
		outputPath := filepath.Join(rulesDir, outputName)

		// 写入文件
		if err := os.WriteFile(outputPath, []byte(processedContent), 0644); err != nil {
			return fmt.Errorf("写入规则 %s 失败: %w", rule.Name, err)
		}
	}

	return nil
}

// generateMCPConfig 生成 MCP 配置
func (s *SyncServiceV2) generateMCPConfig(ideImpl ide.IDE, mcps []packages.MCPInfo, mcpConfig *types.NewMCPConfigV2) error {
	mcpServers := make(map[string]types.MCPServer)

	// 添加 dec 自身
	mcpServers["dec"] = types.MCPServer{
		Command: "dec",
		Args:    []string{"serve"},
	}

	// 添加启用的 MCP
	for _, mcp := range mcps {
		vars := s.getMCPVars(mcp.Name, mcpConfig)

		// 处理命令
		command := s.parser.Replace(mcp.Command, vars)

		// 处理参数
		var args []string
		for _, arg := range mcp.Args {
			args = append(args, s.parser.Replace(arg, vars))
		}

		// 处理环境变量
		env := make(map[string]string)
		for k, v := range mcp.Env {
			env[k] = s.parser.Replace(v, vars)
		}

		mcpServers[mcp.Name] = types.MCPServer{
			Command: command,
			Args:    args,
			Env:     env,
		}
	}

	// 加载现有配置并合并（保留用户手动添加的）
	existingConfig, _ := ideImpl.LoadMCPConfig(s.projectRoot)
	finalConfig := s.mergeConfig(existingConfig, mcpServers)

	// 写入配置
	return ideImpl.WriteMCPConfig(s.projectRoot, finalConfig)
}

// mergeConfig 合并 MCP 配置
func (s *SyncServiceV2) mergeConfig(existing *types.MCPConfig, managed map[string]types.MCPServer) *types.MCPConfig {
	result := &types.MCPConfig{
		MCPServers: make(map[string]types.MCPServer),
	}

	// 添加托管的配置
	for name, server := range managed {
		result.MCPServers[name] = server
	}

	// 保留用户手动添加的配置
	if existing != nil {
		for name, server := range existing.MCPServers {
			if _, isManaged := managed[name]; !isManaged {
				result.MCPServers[name] = server
			}
		}
	}

	return result
}

// cleanManagedRules 清理托管的规则文件
func (s *SyncServiceV2) cleanManagedRules(ideImpl ide.IDE) error {
	rulesDir := ideImpl.RulesDir(s.projectRoot)

	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// 清理 dec- 前缀的文件
		if strings.HasPrefix(entry.Name(), "dec-") {
			path := filepath.Join(rulesDir, entry.Name())
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}

	return nil
}
