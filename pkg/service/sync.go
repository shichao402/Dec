// Package service 提供业务逻辑服务层
// 服务层负责协调各个模块，实现核心业务流程
package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/generator"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/registry"
	"github.com/shichao402/Dec/pkg/types"
)

// SyncService 同步服务，协调规则和 MCP 配置的生成
type SyncService struct {
	projectRoot string
	configMgr   *config.ProjectConfigManager
	registryMgr *registry.MultiRegistryManager
	rulesGen    *generator.RulesGenerator
	mcpGen      *generator.MCPGenerator
}

// NewSyncService 创建同步服务
func NewSyncService(projectRoot string) *SyncService {
	return &SyncService{
		projectRoot: projectRoot,
		configMgr:   config.NewProjectConfigManager(projectRoot),
		registryMgr: registry.NewMultiRegistryManager(),
		rulesGen:    generator.NewRulesGenerator(),
		mcpGen:      generator.NewMCPGenerator(),
	}
}

// SyncResult 同步结果
type SyncResult struct {
	ProjectName string            // 项目名称
	IDEs        []string          // 目标 IDE 列表
	IDEResults  map[string]IDEResult // 每个 IDE 的同步结果
}

// IDEResult 单个 IDE 的同步结果
type IDEResult struct {
	RulesDir        string   // 规则目录
	MCPConfigPath   string   // MCP 配置路径
	CoreRulesCount  int      // 核心规则数量
	BuiltinPacks    []string // 启用的内置包
	ExternalPacks   []string // 外部包
	MCPPacks        []string // MCP 包
}

// Sync 执行同步操作
func (s *SyncService) Sync() (*SyncResult, error) {
	// 检查项目是否已初始化
	if !s.configMgr.Exists() {
		return nil, fmt.Errorf("项目未初始化\n\n💡 运行 dec init 初始化项目")
	}

	// 加载项目配置
	projectConfig, err := s.configMgr.LoadProjectConfig()
	if err != nil {
		return nil, fmt.Errorf("加载项目配置失败: %w", err)
	}

	// 加载包配置
	packsConfig, err := s.configMgr.LoadPacksConfig()
	if err != nil {
		return nil, fmt.Errorf("加载包配置失败: %w", err)
	}

	// 加载注册表（仅使用本地缓存，不自动更新）
	// 注册表加载失败时继续使用内置规则，不自动更新避免网络问题导致卡住
	_ = s.registryMgr.Load()

	// 解析包
	rulePacks, mcpPacks, enabledBuiltinPacks := s.resolvePacks(packsConfig)

	// 获取目标 IDE 列表
	ides := projectConfig.IDEs
	if len(ides) == 0 {
		ides = []string{"cursor"}
	}

	// 生成规则内容（与 IDE 无关）
	ruleFiles, err := s.rulesGen.GenerateAll(rulePacks, enabledBuiltinPacks)
	if err != nil {
		return nil, fmt.Errorf("生成规则失败: %w", err)
	}

	// 生成 MCP 配置（与 IDE 无关）
	mcpConfig, managedNames, err := s.mcpGen.GenerateAll(mcpPacks)
	if err != nil {
		return nil, fmt.Errorf("生成 MCP 配置失败: %w", err)
	}

	// 构建结果
	result := &SyncResult{
		ProjectName: projectConfig.Name,
		IDEs:        ides,
		IDEResults:  make(map[string]IDEResult),
	}

	// 为每个 IDE 写入文件
	for _, ideName := range ides {
		ideImpl := ide.Get(ideName)

		// 清理旧的托管规则
		if err := s.cleanManagedRules(ideImpl); err != nil {
			return nil, fmt.Errorf("清理 %s 旧规则失败: %w", ideName, err)
		}

		// 写入规则文件
		if err := ideImpl.WriteRules(s.projectRoot, ruleFiles); err != nil {
			return nil, fmt.Errorf("写入 %s 规则失败: %w", ideName, err)
		}

		// 加载现有 MCP 配置并合并
		existingConfig, _ := ideImpl.LoadMCPConfig(s.projectRoot)
		finalConfig := s.mcpGen.MergeConfig(existingConfig, mcpConfig, managedNames)

		// 写入 MCP 配置
		if err := ideImpl.WriteMCPConfig(s.projectRoot, finalConfig); err != nil {
			return nil, fmt.Errorf("写入 %s MCP 配置失败: %w", ideName, err)
		}

		// 收集结果
		var builtinPackNames []string
		for name := range enabledBuiltinPacks {
			builtinPackNames = append(builtinPackNames, name)
		}

		var externalPackNames []string
		for _, pack := range rulePacks {
			externalPackNames = append(externalPackNames, pack.Name)
		}

		var mcpPackNames []string
		for _, pack := range mcpPacks {
			mcpPackNames = append(mcpPackNames, pack.Name)
		}

		result.IDEResults[ideName] = IDEResult{
			RulesDir:        ideImpl.RulesDir(s.projectRoot),
			MCPConfigPath:   ideImpl.MCPConfigPath(s.projectRoot),
			CoreRulesCount:  5, // 核心规则数量
			BuiltinPacks:    builtinPackNames,
			ExternalPacks:   externalPackNames,
			MCPPacks:        mcpPackNames,
		}
	}

	return result, nil
}

// resolvePacks 解析包配置，分离规则包和 MCP 包
func (s *SyncService) resolvePacks(packsConfig map[string]types.PackEntry) (
	[]generator.RulePackInfo,
	[]generator.MCPPackInfo,
	map[string]bool,
) {
	var rulePacks []generator.RulePackInfo
	var mcpPacks []generator.MCPPackInfo
	enabledBuiltinPacks := make(map[string]bool)

	for name, entry := range packsConfig {
		// 跳过注释字段
		if len(name) > 0 && name[0] == '_' {
			continue
		}
		if !entry.Enabled {
			continue
		}
		// 跳过 dec 自身（会被自动添加）
		if name == "dec" {
			continue
		}

		// 解析包
		resolved := s.registryMgr.ResolvePack(name)
		if resolved == nil {
			// 可能是内置包
			if entry.Type == types.PackTypeRule {
				enabledBuiltinPacks[name] = true
			}
			continue
		}

		// 加载包的 package.json
		pack, err := s.loadPackFromPath(resolved)
		if err != nil {
			continue
		}

		switch entry.Type {
		case types.PackTypeRule:
			rulePacks = append(rulePacks, generator.RulePackInfo{
				Name:        name,
				InstallPath: resolved.InstallPath,
				LocalPath:   resolved.LocalPath,
				Pack:        pack,
				UserConfig:  entry.Config,
			})
		case types.PackTypeMCP:
			mcpPacks = append(mcpPacks, generator.MCPPackInfo{
				Name:        name,
				InstallPath: resolved.InstallPath,
				LocalPath:   resolved.LocalPath,
				Pack:        pack,
				UserConfig:  entry.Config,
			})
		}
	}

	return rulePacks, mcpPacks, enabledBuiltinPacks
}

// loadPackFromPath 从路径加载包的 package.json
func (s *SyncService) loadPackFromPath(resolved *types.ResolvedPack) (*types.Pack, error) {
	packPath := resolved.InstallPath
	if resolved.LocalPath != "" {
		packPath = resolved.LocalPath
	}

	if packPath == "" {
		return nil, fmt.Errorf("包路径为空")
	}

	packageJSONPath := filepath.Join(packPath, "package.json")
	data, err := os.ReadFile(packageJSONPath)
	if err != nil {
		return nil, err
	}

	var pack types.Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		return nil, err
	}

	return &pack, nil
}

// cleanManagedRules 清理托管的规则文件
func (s *SyncService) cleanManagedRules(ideImpl ide.IDE) error {
	rulesDir := ideImpl.RulesDir(s.projectRoot)

	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	prefix := generator.GetManagedRulePrefix()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), prefix) {
			path := filepath.Join(rulesDir, entry.Name())
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}

	return nil
}
