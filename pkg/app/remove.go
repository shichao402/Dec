package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/repo"
)

// RemoveAssetInput 描述一次 remove 操作的输入。
type RemoveAssetInput struct {
	ProjectRoot string
	Type        string
	Name        string
	Vault       string
	Confirmed   bool
}

// RemoveAssetResult 汇总一次 remove 操作的结果。
type RemoveAssetResult struct {
	ProjectRoot      string
	Type             string
	Name             string
	Vault            string
	RemovedFromIDEs  []string
	RemovedFromCache bool
	ConfigUpdated    bool
	VersionCommit    string
}

// ErrRemoveNotConfirmed 调用方没有完成二次确认。
var ErrRemoveNotConfirmed = fmt.Errorf("remove 未确认")

// RemoveAsset 执行一次资产删除：远端 commit、清理 IDE / cache / 项目配置。
//
// 输入约定：
//   - Confirmed 必须为 true，否则立即返回 ErrRemoveNotConfirmed。
//   - Vault 为空时由远端查找唯一匹配；查找失败不做任何改动。
//   - IDE 侧的清理按 GetEffectiveIDEs 解析，失败不阻断远端删除，但会记录 warn。
func RemoveAsset(input RemoveAssetInput, reporter Reporter) (*RemoveAssetResult, error) {
	reporter = defaultReporter(reporter)

	projectRoot := strings.TrimSpace(input.ProjectRoot)
	itemType := strings.TrimSpace(input.Type)
	assetName := strings.TrimSpace(input.Name)
	vaultHint := strings.TrimSpace(input.Vault)

	emit(reporter, EventInfo, "remove.prepare", fmt.Sprintf("🗑  准备删除 [%s] %s", itemType, assetName), nil)

	if projectRoot == "" {
		return nil, fmt.Errorf("项目根目录不能为空")
	}
	if assetName == "" {
		return nil, fmt.Errorf("资产名称不能为空")
	}
	if !isRemovableAssetType(itemType) {
		return nil, fmt.Errorf("不支持的资产类型: %s (支持: skill, rule, mcp)", itemType)
	}
	if !input.Confirmed {
		return nil, ErrRemoveNotConfirmed
	}

	result := &RemoveAssetResult{
		ProjectRoot: projectRoot,
		Type:        itemType,
		Name:        assetName,
		Vault:       vaultHint,
	}

	// Stage 1: 远端删除（最关键，失败直接返回错误）。
	emit(reporter, EventInfo, "remove.repo", "连接资产仓库...", nil)
	if err := withAppWriteRepo(func(tx *repo.Transaction) error {
		repoDir := tx.WorkDir()

		foundVault, fullPath, err := locateAssetInRepo(repoDir, itemType, assetName, vaultHint)
		if err != nil {
			return err
		}
		result.Vault = foundVault

		if err := os.RemoveAll(fullPath); err != nil {
			return fmt.Errorf("删除远端资产失败: %w", err)
		}

		commitMsg := fmt.Sprintf("remove: %s/%s", foundVault, assetName)
		if err := tx.CommitAndPush(commitMsg); err != nil {
			return fmt.Errorf("提交失败: %w", err)
		}

		result.VersionCommit = tx.CommitHash()
		emit(reporter, EventInfo, "remove.repo", fmt.Sprintf("✅ 已从远端删除 (vault: %s)", foundVault), nil)
		return nil
	}); err != nil {
		emit(reporter, EventError, "remove.repo", err.Error(), nil)
		return nil, err
	}

	// Stage 2: IDE 清理（尽力而为）。
	projectIDEs := resolveProjectIDEs(projectRoot, reporter)
	for _, ideImpl := range projectIDEs {
		removed, err := removeAssetFromIDE(itemType, assetName, projectRoot, ideImpl)
		if err != nil {
			emit(reporter, EventWarn, "remove.ide", fmt.Sprintf("IDE %s 清理失败: %v", ideImpl.Name(), err), nil)
			continue
		}
		if removed {
			result.RemovedFromIDEs = append(result.RemovedFromIDEs, ideImpl.Name())
		}
	}
	if len(result.RemovedFromIDEs) > 0 {
		emit(reporter, EventInfo, "remove.ide", fmt.Sprintf("🧹 已清理 IDE: %s", strings.Join(result.RemovedFromIDEs, ", ")), nil)
	}

	// Stage 3: 本地缓存清理。
	cachePath := getCachePath(projectRoot, result.Vault, itemType, assetName)
	if cachePath != "" {
		if _, err := os.Stat(cachePath); err == nil {
			if err := os.RemoveAll(cachePath); err != nil {
				emit(reporter, EventWarn, "remove.cache", fmt.Sprintf("缓存清理失败: %v", err), nil)
			} else {
				result.RemovedFromCache = true
				emit(reporter, EventInfo, "remove.cache", "🧹 已清理本地缓存", nil)
			}
		}
	}

	// Stage 4: 项目配置更新。
	mgr := config.NewProjectConfigManager(projectRoot)
	if projectConfig, err := mgr.LoadProjectConfig(); err == nil {
		changed := false
		if projectConfig.Enabled != nil && projectConfig.Enabled.RemoveAsset(itemType, assetName, result.Vault) {
			changed = true
		}
		if projectConfig.Available != nil && projectConfig.Available.RemoveAsset(itemType, assetName, result.Vault) {
			changed = true
		}
		if changed {
			if err := mgr.SaveProjectConfig(projectConfig); err != nil {
				emit(reporter, EventWarn, "remove.config", fmt.Sprintf("项目配置更新失败: %v", err), nil)
			} else {
				result.ConfigUpdated = true
				emit(reporter, EventInfo, "remove.config", "📝 已更新项目配置", nil)
			}
		}
	}

	summary := fmt.Sprintf("✅ 已删除 [%s] %s (vault: %s)", itemType, assetName, result.Vault)
	emit(reporter, EventInfo, "remove.finish", summary, nil)
	return result, nil
}

func isRemovableAssetType(t string) bool {
	return t == "skill" || t == "rule" || t == "mcp"
}

// locateAssetInRepo 在 repo 中定位资产文件。vaultHint 非空时优先走该 vault；为空时遍历顶层目录查找唯一匹配。
func locateAssetInRepo(repoDir, itemType, assetName, vaultHint string) (string, string, error) {
	if vaultHint != "" {
		fullPath := resolveAssetFile(repoDir, vaultHint, itemType, assetName)
		if fullPath == "" {
			return "", "", fmt.Errorf("不支持的资产类型: %s", itemType)
		}
		if _, err := os.Stat(fullPath); err != nil {
			return "", "", fmt.Errorf("未找到 %s '%s' (vault: %s)", itemType, assetName, vaultHint)
		}
		return vaultHint, fullPath, nil
	}

	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return "", "", fmt.Errorf("读取仓库目录失败: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		fullPath := resolveAssetFile(repoDir, entry.Name(), itemType, assetName)
		if fullPath == "" {
			continue
		}
		if _, err := os.Stat(fullPath); err == nil {
			return entry.Name(), fullPath, nil
		}
	}
	return "", "", fmt.Errorf("未找到 %s '%s'", itemType, assetName)
}

// withAppWriteRepo 等价于 cmd/vault.go 中 withWriteRepo 的实现，但位于 pkg/app 包内，
// 避免用例层反向依赖 cmd 包。
func withAppWriteRepo(fn func(*repo.Transaction) error) error {
	if globalConfig, err := config.LoadGlobalConfig(); err == nil {
		if err := repo.EnsureConnectedRepoMatches(globalConfig.RepoURL); err != nil {
			return err
		}
	}

	tx, err := repo.NewWriteTransaction()
	if err != nil {
		return err
	}
	defer tx.Close()
	return fn(tx)
}

// resolveProjectIDEs 解析当前项目可用的 IDE 列表用于资产清理。
func resolveProjectIDEs(projectRoot string, reporter Reporter) []ide.IDE {
	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		emit(reporter, EventWarn, "remove.ide", fmt.Sprintf("加载项目配置失败: %v", err), nil)
		return nil
	}

	selection, err := config.ResolveEffectiveIDEs(projectConfig)
	if err != nil {
		emit(reporter, EventWarn, "remove.ide", fmt.Sprintf("解析 IDE 失败: %v", err), nil)
		return nil
	}
	for _, warning := range selection.Warnings {
		emit(reporter, EventWarn, "remove.ide", warning, nil)
	}

	return uniqueProjectIDEs(projectRoot, selection.IDEs)
}
