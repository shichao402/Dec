package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
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

// RemoveBundleInput 描述一次 bundle 级 remove 操作的输入。
type RemoveBundleInput struct {
	ProjectRoot string
	BundleName  string
	// Members 为 bundle 成员（用于 IDE/cache 清理）；为空时在远端删除前从 repo 扫描。
	Members   []AssetSelectionItem
	Confirmed bool
}

// RemoveBundleResult 汇总一次 bundle 级 remove 操作的结果。
type RemoveBundleResult struct {
	ProjectRoot      string
	BundleName       string
	MemberCount      int
	RemovedFromIDEs  []string
	RemovedFromCache bool
	ConfigUpdated    bool
	VersionCommit    string
}

// RemoveBundle 执行 bundle 级删除：远端删除 bundles/<name>/、清理 IDE / cache / 项目配置。
//
// 不会自动删除 Bitwarden secrets bundle；敏感文件需用户自行处理。
func RemoveBundle(input RemoveBundleInput, reporter Reporter) (*RemoveBundleResult, error) {
	reporter = defaultReporter(reporter)

	projectRoot := strings.TrimSpace(input.ProjectRoot)
	bundleName := strings.TrimSpace(input.BundleName)

	emit(reporter, EventInfo, "remove.prepare", fmt.Sprintf("🗑  准备删除 bundle %s", bundleName), nil)

	if projectRoot == "" {
		return nil, fmt.Errorf("项目根目录不能为空")
	}
	if bundleName == "" {
		return nil, fmt.Errorf("bundle 名称不能为空")
	}
	if !input.Confirmed {
		return nil, ErrRemoveNotConfirmed
	}

	result := &RemoveBundleResult{
		ProjectRoot: projectRoot,
		BundleName:  bundleName,
	}

	members := append([]AssetSelectionItem(nil), input.Members...)

	// Stage 1: 远端删除整包（最关键，失败直接返回错误）。
	emit(reporter, EventInfo, "remove.repo", "连接资产仓库...", nil)
	if err := withAppWriteRepo(func(tx *repo.Transaction) error {
		repoDir := tx.WorkDir()
		bundlePath := filepath.Join(repoDir, types.VaultBundlesDir, bundleName)
		if _, err := os.Stat(bundlePath); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("未找到 bundle %q", bundleName)
			}
			return fmt.Errorf("检查 bundle 目录失败: %w", err)
		}
		if len(members) == 0 {
			members = bundleSelectionItemsFromRepo(repoDir, bundleName)
		}
		if err := os.RemoveAll(bundlePath); err != nil {
			return fmt.Errorf("删除远端 bundle 失败: %w", err)
		}
		commitMsg := fmt.Sprintf("remove bundle: %s", bundleName)
		if err := tx.CommitAndPush(commitMsg); err != nil {
			return fmt.Errorf("提交失败: %w", err)
		}
		result.VersionCommit = tx.CommitHash()
		emit(reporter, EventInfo, "remove.repo", fmt.Sprintf("✅ 已从远端删除 bundle %s", bundleName), nil)
		return nil
	}); err != nil {
		emit(reporter, EventError, "remove.repo", err.Error(), nil)
		return nil, err
	}

	result.MemberCount = len(members)

	// Stage 2: IDE 清理（尽力而为）。
	projectIDEs := resolveProjectIDEs(projectRoot, reporter)
	removedIDEs := make(map[string]struct{})
	for _, member := range members {
		for _, ideImpl := range projectIDEs {
			removed, err := removeAssetFromIDE(member.Type, member.Name, projectRoot, ideImpl)
			if err != nil {
				emit(reporter, EventWarn, "remove.ide", fmt.Sprintf("IDE %s 清理 %s 失败: %v", ideImpl.Name(), member.Name, err), nil)
				continue
			}
			if removed {
				removedIDEs[ideImpl.Name()] = struct{}{}
			}
		}
	}
	if len(removedIDEs) > 0 {
		ideNames := make([]string, 0, len(removedIDEs))
		for name := range removedIDEs {
			ideNames = append(ideNames, name)
		}
		sort.Strings(ideNames)
		result.RemovedFromIDEs = ideNames
		emit(reporter, EventInfo, "remove.ide", fmt.Sprintf("🧹 已清理 IDE: %s", strings.Join(ideNames, ", ")), nil)
	}

	// Stage 3: 本地 bundle cache 清理。
	cacheBundleDir := filepath.Join(projectRoot, ".dec", "cache", bundleName)
	if _, err := os.Stat(cacheBundleDir); err == nil {
		if err := os.RemoveAll(cacheBundleDir); err != nil {
			emit(reporter, EventWarn, "remove.cache", fmt.Sprintf("缓存清理失败: %v", err), nil)
		} else {
			result.RemovedFromCache = true
			emit(reporter, EventInfo, "remove.cache", "🧹 已清理本地 bundle 缓存", nil)
		}
	}

	// Stage 4: 项目配置更新。
	mgr := config.NewProjectConfigManager(projectRoot)
	if projectConfig, err := mgr.LoadProjectConfig(); err == nil {
		changed := false
		if updated, ok := removeEnabledBundle(projectConfig.EnabledBundles, bundleName); ok {
			projectConfig.EnabledBundles = updated
			changed = true
		}
		for _, member := range members {
			if projectConfig.Enabled != nil && projectConfig.Enabled.RemoveAsset(member.Type, member.Name, member.Vault) {
				changed = true
			}
			if projectConfig.Available != nil && projectConfig.Available.RemoveAsset(member.Type, member.Name, member.Vault) {
				changed = true
			}
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

	summary := fmt.Sprintf("✅ 已删除 bundle %s（%d 个成员）", bundleName, result.MemberCount)
	emit(reporter, EventInfo, "remove.finish", summary, nil)
	return result, nil
}

func removeEnabledBundle(bundles []string, name string) ([]string, bool) {
	changed := false
	out := make([]string, 0, len(bundles))
	for _, b := range bundles {
		if b == name {
			changed = true
			continue
		}
		out = append(out, b)
	}
	return out, changed
}

func bundleSelectionItemsFromRepo(repoDir, bundleName string) []AssetSelectionItem {
	refs := listBundleAssetMembers(repoDir, bundleName)
	items := make([]AssetSelectionItem, 0, len(refs))
	for _, ref := range refs {
		parts := strings.SplitN(ref, "/", 2)
		if len(parts) != 2 {
			continue
		}
		itemType := memberPrefixToAssetType(parts[0])
		if itemType == "" {
			continue
		}
		items = append(items, AssetSelectionItem{
			Type:  itemType,
			Name:  parts[1],
			Vault: bundleName,
		})
	}
	return items
}

func memberPrefixToAssetType(prefix string) string {
	switch prefix {
	case "skills":
		return "skill"
	case "commands":
		return "command"
	case "rules":
		return "rule"
	case "mcp":
		return "mcp"
	default:
		return ""
	}
}

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
		return nil, fmt.Errorf("不支持的资产类型: %s (支持: skill, command, rule, mcp)", itemType)
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
	return t == "skill" || t == "command" || t == "rule" || t == "mcp"
}

// locateAssetInRepo 在 repo 中定位资产文件。vaultHint 非空时优先走该 bundle；为空时遍历 bundles/ 子目录查找唯一匹配。
func locateAssetInRepo(repoDir, itemType, assetName, vaultHint string) (string, string, error) {
	if vaultHint != "" {
		fullPath := resolveAssetFile(repoDir, vaultHint, itemType, assetName)
		if fullPath == "" {
			return "", "", fmt.Errorf("不支持的资产类型: %s", itemType)
		}
		if _, err := os.Stat(fullPath); err != nil {
			return "", "", fmt.Errorf("未找到 %s '%s' (bundle: %s)", itemType, assetName, vaultHint)
		}
		return vaultHint, fullPath, nil
	}

	bundlesDir := filepath.Join(repoDir, types.VaultBundlesDir)
	entries, err := os.ReadDir(bundlesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("未找到 %s '%s'", itemType, assetName)
		}
		return "", "", fmt.Errorf("读取仓库 bundle 目录失败: %w", err)
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
