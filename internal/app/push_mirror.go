package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/internal/types"
)

// syncDecVaultFromCache 将 enabled 范围内的 vault 资产与本地 cache 对齐：复制存在项、删除 cache 已缺失项。
func syncDecVaultFromCache(projectRoot, repoDir string, projectConfig *types.ProjectConfig, resolved *ResolvedAssets, reporter Reporter) (synced, pruned int, err error) {
	assets := resolved.Assets
	if len(assets) == 0 && len(projectConfig.EnabledBundles) == 0 {
		return 0, 0, nil
	}

	bundlesToScan := collectEnabledBundleNames(projectConfig, assets)

	for idx, asset := range assets {
		progress := &Progress{Phase: "dec", Current: idx + 1, Total: len(assets)}
		cachePath := getCachePath(projectRoot, asset.Vault, asset.Type, asset.Name)
		destPath := resolveAssetFile(repoDir, asset.Vault, asset.Type, asset.Name)
		if destPath == "" {
			continue
		}

		if _, statErr := os.Stat(cachePath); os.IsNotExist(statErr) {
			if _, vaultErr := os.Stat(destPath); vaultErr == nil {
				if rmErr := os.RemoveAll(destPath); rmErr != nil {
					emit(reporter, EventWarn, "push.dec", fmt.Sprintf("⚠️  [%s] %s 远端删除失败: %v", asset.Type, asset.Name, rmErr), progress)
					continue
				}
				pruned++
				emit(reporter, EventInfo, "push.dec", fmt.Sprintf("  − [%s] %s（cache 已删）", asset.Type, asset.Name), progress)
			}
			continue
		}

		switch asset.Type {
		case "skill", "command":
			if copyErr := copyDir(cachePath, destPath); copyErr != nil {
				emit(reporter, EventWarn, "push.dec", fmt.Sprintf("⚠️  [%s] %s 推送失败: %v", asset.Type, asset.Name, copyErr), progress)
				continue
			}
		case "rule", "mcp":
			if copyErr := copyFile(cachePath, destPath); copyErr != nil {
				emit(reporter, EventWarn, "push.dec", fmt.Sprintf("⚠️  [%s] %s 推送失败: %v", asset.Type, asset.Name, copyErr), progress)
				continue
			}
		default:
			continue
		}
		synced++
		emit(reporter, EventInfo, "push.dec", fmt.Sprintf("  [%s] %s → %s", asset.Type, asset.Name, asset.Vault), progress)
	}

	for bundleName := range bundlesToScan {
		members := listBundleAssetMembers(repoDir, bundleName)
		for _, member := range members {
			parts := strings.SplitN(member, "/", 2)
			if len(parts) != 2 {
				continue
			}
			itemType := memberPrefixToAssetType(parts[0])
			if itemType == "" {
				continue
			}
			assetName := parts[1]
			if assetInResolved(assets, bundleName, itemType, assetName) {
				continue
			}
			cachePath := getCachePath(projectRoot, bundleName, itemType, assetName)
			vaultPath := resolveAssetFile(repoDir, bundleName, itemType, assetName)
			if vaultPath == "" {
				continue
			}
			if _, cacheErr := os.Stat(cachePath); !os.IsNotExist(cacheErr) {
				continue
			}
			if _, vaultErr := os.Stat(vaultPath); os.IsNotExist(vaultErr) {
				continue
			}
			if rmErr := os.RemoveAll(vaultPath); rmErr != nil {
				emit(reporter, EventWarn, "push.dec", fmt.Sprintf("⚠️  [%s] %s 远端删除失败: %v", itemType, assetName, rmErr), nil)
				continue
			}
			pruned++
			emit(reporter, EventInfo, "push.dec", fmt.Sprintf("  − [%s] %s / %s（cache 已删）", itemType, assetName, bundleName), nil)
		}
	}
	return synced, pruned, nil
}

func collectEnabledBundleNames(projectConfig *types.ProjectConfig, assets []types.TypedAssetRef) map[string]struct{} {
	out := make(map[string]struct{})
	for _, name := range projectConfig.EnabledBundles {
		out[name] = struct{}{}
	}
	for _, asset := range assets {
		vault := strings.TrimSpace(asset.Vault)
		if vault != "" {
			out[vault] = struct{}{}
		}
	}
	return out
}

func assetInResolved(assets []types.TypedAssetRef, vault, itemType, name string) bool {
	for _, asset := range assets {
		if strings.TrimSpace(asset.Vault) == vault && asset.Type == itemType && asset.Name == name {
			return true
		}
	}
	return false
}
