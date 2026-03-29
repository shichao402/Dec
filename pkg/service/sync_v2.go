package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/shichao402/Dec/pkg/vault"
)

// SyncServiceV2 同步服务
type SyncServiceV2 struct {
	projectRoot string
	configMgr   *config.ProjectConfigManagerV2
}

// NewSyncServiceV2 创建同步服务
func NewSyncServiceV2(projectRoot string) (*SyncServiceV2, error) {
	return &SyncServiceV2{
		projectRoot: projectRoot,
		configMgr:   config.NewProjectConfigManagerV2(projectRoot),
	}, nil
}

// SyncResultV2 同步结果
type SyncResultV2 struct {
	ProjectName string
	IDEs        []string
	SkillsCount int
	RulesCount  int
	MCPsCount   int
	Warnings    []string
}

type resolvedVaultAssets struct {
	Vault  *vault.Vault
	Skills []vault.VaultItem
	Rules  []vault.VaultItem
	MCPs   []vault.VaultItem
}

// Sync 执行同步操作
func (s *SyncServiceV2) Sync() (*SyncResultV2, error) {
	if !s.configMgr.Exists() {
		return nil, fmt.Errorf("项目未初始化\n\n💡 运行 dec init 初始化项目")
	}

	idesConfig, err := s.configMgr.LoadIDEsConfig()
	if err != nil {
		return nil, fmt.Errorf("加载 IDE 配置失败: %w", err)
	}

	vaultConfig, err := s.configMgr.LoadVaultConfig()
	if err != nil {
		return nil, fmt.Errorf("加载 Vault 配置失败: %w", err)
	}

	assets, warnings, err := s.loadResolvedVaultAssets(vaultConfig)
	if err != nil {
		return nil, err
	}

	for _, ideName := range idesConfig.IDEs {
		ideImpl := ide.Get(ideName)
		if err := s.syncIDE(ideName, ideImpl, assets); err != nil {
			return nil, err
		}
	}

	if err := s.trackSyncedAssets(idesConfig.IDEs, assets); err != nil {
		return nil, fmt.Errorf("更新同步追踪失败: %w", err)
	}

	return &SyncResultV2{
		ProjectName: filepath.Base(s.projectRoot),
		IDEs:        idesConfig.IDEs,
		SkillsCount: len(assets.Skills),
		RulesCount:  len(assets.Rules),
		MCPsCount:   len(assets.MCPs),
		Warnings:    warnings,
	}, nil
}

type ideSyncBackup struct {
	tempDir          string
	mcpConfigExisted bool
}

func (b *ideSyncBackup) cleanup() {
	if b == nil || b.tempDir == "" {
		return
	}
	_ = os.RemoveAll(b.tempDir)
}

func (s *SyncServiceV2) syncIDE(ideName string, ideImpl ide.IDE, assets *resolvedVaultAssets) error {
	backup, err := s.createIDEBackup(ideImpl)
	if err != nil {
		return fmt.Errorf("备份 %s 当前托管资产失败: %w", ideName, err)
	}
	defer backup.cleanup()

	if err := s.cleanManagedRules(ideImpl); err != nil {
		return s.restoreIDEOnFailure(ideName, ideImpl, backup, fmt.Errorf("清理 %s 旧规则失败: %w", ideName, err))
	}
	if err := s.cleanManagedSkills(ideImpl); err != nil {
		return s.restoreIDEOnFailure(ideName, ideImpl, backup, fmt.Errorf("清理 %s 旧 Skills 失败: %w", ideName, err))
	}
	if err := s.syncVaultSkills(assets.Vault, ideImpl, assets.Skills); err != nil {
		return s.restoreIDEOnFailure(ideName, ideImpl, backup, fmt.Errorf("同步 %s Skills 失败: %w", ideName, err))
	}
	if err := s.syncVaultRules(assets.Vault, ideImpl, assets.Rules); err != nil {
		return s.restoreIDEOnFailure(ideName, ideImpl, backup, fmt.Errorf("同步 %s Rules 失败: %w", ideName, err))
	}
	if err := s.syncVaultMCPs(assets.Vault, ideImpl, assets.MCPs); err != nil {
		return s.restoreIDEOnFailure(ideName, ideImpl, backup, fmt.Errorf("同步 %s MCPs 失败: %w", ideName, err))
	}

	return nil
}

func (s *SyncServiceV2) createIDEBackup(ideImpl ide.IDE) (*ideSyncBackup, error) {
	tempDir, err := os.MkdirTemp("", "dec-sync-backup-")
	if err != nil {
		return nil, err
	}

	backup := &ideSyncBackup{tempDir: tempDir}

	skillsDir := ideImpl.SkillsDir(s.projectRoot)
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "dec-") {
				continue
			}
			srcPath := filepath.Join(skillsDir, entry.Name())
			dstPath := filepath.Join(tempDir, "skills", entry.Name())
			if err := vault.CopyDir(srcPath, dstPath); err != nil {
				return nil, err
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	rulesDir := ideImpl.RulesDir(s.projectRoot)
	if entries, err := os.ReadDir(rulesDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "dec-") {
				continue
			}
			srcPath := filepath.Join(rulesDir, entry.Name())
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return nil, err
			}
			dstPath := filepath.Join(tempDir, "rules", entry.Name())
			if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return nil, err
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	mcpConfigPath := ideImpl.MCPConfigPath(s.projectRoot)
	if data, err := os.ReadFile(mcpConfigPath); err == nil {
		backup.mcpConfigExisted = true
		dstPath := filepath.Join(tempDir, "mcp.json")
		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	return backup, nil
}

func (s *SyncServiceV2) restoreIDEOnFailure(ideName string, ideImpl ide.IDE, backup *ideSyncBackup, cause error) error {
	if backup == nil {
		return cause
	}
	if err := s.restoreIDEBackup(ideImpl, backup); err != nil {
		return fmt.Errorf("%v；恢复 %s 原有托管资产失败: %v", cause, ideName, err)
	}
	return cause
}

func (s *SyncServiceV2) restoreIDEBackup(ideImpl ide.IDE, backup *ideSyncBackup) error {
	if err := s.cleanManagedRules(ideImpl); err != nil {
		return err
	}
	if err := s.cleanManagedSkills(ideImpl); err != nil {
		return err
	}

	skillsBackupDir := filepath.Join(backup.tempDir, "skills")
	if entries, err := os.ReadDir(skillsBackupDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dstPath := filepath.Join(ideImpl.SkillsDir(s.projectRoot), entry.Name())
			if err := vault.CopyDir(filepath.Join(skillsBackupDir, entry.Name()), dstPath); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	rulesBackupDir := filepath.Join(backup.tempDir, "rules")
	if entries, err := os.ReadDir(rulesBackupDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(rulesBackupDir, entry.Name()))
			if err != nil {
				return err
			}
			dstPath := filepath.Join(ideImpl.RulesDir(s.projectRoot), entry.Name())
			if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	mcpConfigPath := ideImpl.MCPConfigPath(s.projectRoot)
	if backup.mcpConfigExisted {
		data, err := os.ReadFile(filepath.Join(backup.tempDir, "mcp.json"))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(mcpConfigPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(mcpConfigPath, data, 0644); err != nil {
			return err
		}
	} else if err := os.Remove(mcpConfigPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (s *SyncServiceV2) loadResolvedVaultAssets(config *types.VaultConfigV2) (*resolvedVaultAssets, []string, error) {
	assets := &resolvedVaultAssets{}
	if !hasVaultDeclarations(config) {
		return assets, nil, nil
	}

	v, err := vault.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("打开 Vault 失败: %w", err)
	}

	var warnings []string
	if err := v.Refresh(); err != nil {
		warnings = append(warnings, fmt.Sprintf("同步 Vault 远程状态失败，已回退到本地缓存: %v", err))
	}

	skills, err := resolveDeclaredItems(v, config.VaultSkills, "skill")
	if err != nil {
		return nil, warnings, err
	}
	rules, err := resolveDeclaredItems(v, config.VaultRules, "rule")
	if err != nil {
		return nil, warnings, err
	}
	mcps, err := resolveDeclaredItems(v, config.VaultMCPs, "mcp")
	if err != nil {
		return nil, warnings, err
	}

	assets.Vault = v
	assets.Skills = skills
	assets.Rules = rules
	assets.MCPs = mcps

	return assets, warnings, nil
}

func hasVaultDeclarations(config *types.VaultConfigV2) bool {
	if config == nil {
		return false
	}
	return len(config.VaultSkills) > 0 || len(config.VaultRules) > 0 || len(config.VaultMCPs) > 0
}

func resolveDeclaredItems(v *vault.Vault, names []string, itemType string) ([]vault.VaultItem, error) {
	if len(names) == 0 {
		return nil, nil
	}

	items := make([]vault.VaultItem, 0, len(names))
	for _, name := range names {
		item := v.Index.Get(itemType, name)
		if item == nil {
			return nil, fmt.Errorf("Vault 中未找到声明的 %s: %s", itemType, name)
		}
		items = append(items, *item)
	}

	return items, nil
}

// syncVaultSkills 将 vault 中的 skills 同步到 IDE 目录
func (s *SyncServiceV2) syncVaultSkills(v *vault.Vault, ideImpl ide.IDE, items []vault.VaultItem) error {
	if len(items) == 0 {
		return nil
	}
	if v == nil {
		return fmt.Errorf("Vault 未就绪")
	}

	skillsDir := ideImpl.SkillsDir(s.projectRoot)
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return err
	}

	for _, item := range items {
		srcPath := filepath.Join(v.Dir, filepath.FromSlash(item.Path))
		if _, err := os.Stat(srcPath); err != nil {
			return fmt.Errorf("Vault Skill 不存在: %s", item.Name)
		}
		destDir := filepath.Join(skillsDir, vault.ManagedName(item.Name))
		if err := vault.CopyDir(srcPath, destDir); err != nil {
			return fmt.Errorf("同步 Vault Skill %s 失败: %w", item.Name, err)
		}
	}

	return nil
}

// syncVaultRules 将 vault 中的 rules 同步到 IDE 目录
func (s *SyncServiceV2) syncVaultRules(v *vault.Vault, ideImpl ide.IDE, items []vault.VaultItem) error {
	if len(items) == 0 {
		return nil
	}
	if v == nil {
		return fmt.Errorf("Vault 未就绪")
	}

	rulesDir := ideImpl.RulesDir(s.projectRoot)
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return err
	}

	for _, item := range items {
		srcPath := filepath.Join(v.Dir, filepath.FromSlash(item.Path))
		content, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("读取 Vault Rule %s 失败: %w", item.Name, err)
		}
		destPath := filepath.Join(rulesDir, vault.ManagedName(item.Name)+".mdc")
		if err := os.WriteFile(destPath, content, 0644); err != nil {
			return fmt.Errorf("同步 Vault Rule %s 失败: %w", item.Name, err)
		}
	}

	return nil
}

// syncVaultMCPs 将 vault 中的 MCP 配置合并到 IDE 的 mcp.json
func (s *SyncServiceV2) syncVaultMCPs(v *vault.Vault, ideImpl ide.IDE, items []vault.VaultItem) error {
	managedServers := make(map[string]types.MCPServer)

	if len(items) > 0 {
		if v == nil {
			return fmt.Errorf("Vault 未就绪")
		}

		for _, item := range items {
			srcPath := filepath.Join(v.Dir, filepath.FromSlash(item.Path))
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return fmt.Errorf("读取 Vault MCP %s 失败: %w", item.Name, err)
			}

			var server types.MCPServer
			if err := json.Unmarshal(data, &server); err != nil {
				return fmt.Errorf("解析 Vault MCP %s 失败: %w", item.Name, err)
			}
			if server.Command == "" {
				return fmt.Errorf("Vault MCP %s 缺少 command 字段", item.Name)
			}
			managedServers[vault.ManagedName(item.Name)] = server
		}
	}

	existingConfig, err := ideImpl.LoadMCPConfig(s.projectRoot)
	if err != nil {
		return fmt.Errorf("加载现有 MCP 配置失败: %w", err)
	}
	finalConfig := mergeConfig(existingConfig, managedServers)

	return ideImpl.WriteMCPConfig(s.projectRoot, finalConfig)
}

// mergeConfig 合并 MCP 配置（托管配置优先，保留用户手动添加的）
func mergeConfig(existing *types.MCPConfig, managed map[string]types.MCPServer) *types.MCPConfig {
	result := &types.MCPConfig{
		MCPServers: make(map[string]types.MCPServer),
	}

	// 第 1 步：添加所有托管服务
	for name, server := range managed {
		result.MCPServers[name] = server
	}

	// 第 2 步：保留用户手动添加的配置，去重冲突项
	if existing != nil {
		for name, server := range existing.MCPServers {
			// 跳过已有的 dec- 前缀配置（由 Dec 托管）
			if strings.HasPrefix(name, "dec-") {
				continue
			}
			// 跳过与托管版本冲突的用户配置
			// 例如：用户手动添加了 "dec"，Vault 托管了 "dec-dec"，优先保留托管版本
			if _, isConflict := managed["dec-"+name]; isConflict {
				continue
			}
			if _, isManaged := managed[name]; !isManaged {
				result.MCPServers[name] = server
			}
		}
	}

	return result
}

func (s *SyncServiceV2) trackSyncedAssets(ideNames []string, assets *resolvedVaultAssets) error {
	if assets == nil {
		return nil
	}

	td, err := vault.LoadTracking(s.projectRoot)
	if err != nil {
		return err
	}

	for _, item := range assets.Skills {
		if err := s.trackAssetPaths(td, ideNames, item, false); err != nil {
			return err
		}
	}
	for _, item := range assets.Rules {
		if err := s.trackAssetPaths(td, ideNames, item, false); err != nil {
			return err
		}
	}
	for _, item := range assets.MCPs {
		if err := s.trackAssetPaths(td, ideNames, item, true); err != nil {
			return err
		}
	}

	return td.Save(s.projectRoot)
}

func (s *SyncServiceV2) trackAssetPaths(td *vault.TrackingData, ideNames []string, item vault.VaultItem, singlePath bool) error {
	localPaths, err := s.managedLocalPaths(ideNames, item.Type, item.Name)
	if err != nil {
		return err
	}
	if singlePath && len(localPaths) > 1 {
		localPaths = localPaths[:1]
	}

	hash := ""
	if len(localPaths) > 0 {
		hash, err = vault.HashPath(filepath.Join(s.projectRoot, localPaths[0]))
		if err != nil {
			return err
		}
	}

	td.TrackPaths(item.Name, item.Type, localPaths, hash)
	return nil
}

func (s *SyncServiceV2) managedLocalPaths(ideNames []string, itemType, name string) ([]string, error) {
	localPaths := make([]string, 0, len(ideNames))
	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)

		var fullPath string
		switch itemType {
		case "skill":
			fullPath = filepath.Join(ideImpl.SkillsDir(s.projectRoot), vault.ManagedName(name))
		case "rule":
			fullPath = filepath.Join(ideImpl.RulesDir(s.projectRoot), vault.ManagedName(name)+".mdc")
		case "mcp":
			fullPath = ideImpl.MCPConfigPath(s.projectRoot)
		default:
			return nil, fmt.Errorf("不支持的资产类型: %s", itemType)
		}

		relPath, err := filepath.Rel(s.projectRoot, fullPath)
		if err != nil {
			return nil, err
		}
		localPaths = append(localPaths, relPath)
	}

	return localPaths, nil
}

// cleanManagedSkills 清理托管的 Skill 目录
func (s *SyncServiceV2) cleanManagedSkills(ideImpl ide.IDE) error {
	skillsDir := ideImpl.SkillsDir(s.projectRoot)

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), "dec-") {
			path := filepath.Join(skillsDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}

	return nil
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
		if strings.HasPrefix(entry.Name(), "dec-") {
			path := filepath.Join(rulesDir, entry.Name())
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}

	return nil
}
