// bundle_resolver.go 负责把 ProjectConfig.enabled_bundles 解析为本轮 pull 的目标资产集合，
// 并记录每个资产的来源（bundle/<name>）。
//
// 本文件只做「想装哪些资产」的解析；真正的装卸仍由 operations.go 内的 installAssetToIDEs
// 与 cleanupRemovedAssets 负责。同一资产被多个 bundle 引用时来源会叠加，
// 只要任何 bundle 仍引用它，它就会出现在目标集里，不会被清理掉。
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/types")

// BundleOverview 描述一次解析中涉及的 bundle 状态，供 TUI / CLI 呈现。
type BundleOverview struct {
	// Name 是 bundle 短名。
	Name string
	// Description 来自 bundle YAML。
	Description string
	// VaultName 指出 bundle 来自哪个 vault。
	VaultName string
	// Members 是 bundle 声明的成员引用列表（按 YAML 顺序），含 <type>/<name> 原文。
	Members []string
	// Enabled 表示该 bundle 是否出现在 ProjectConfig.EnabledBundles 中。
	Enabled bool
	// UserEnabled 表示该 bundle 是否出现在本机 user_enabled_bundles 中。
	// 与 Enabled 独立；pull 目标集取二者并集（ADR 0003）。
	UserEnabled bool
}

// ResolvedAssets 是解析后的目标资产集合及来源追踪信息。
type ResolvedAssets struct {
	// Assets 是按 (type, vault, name) 去重后的目标资产清单。
	Assets []types.TypedAssetRef
	// Sources 以 "type:vault:name" 为 key，值是 ["bundle/<name>"] 这类来源列表；
	// 同一资产被多个 bundle 引用时会出现多项。
	Sources map[string][]string
	// Bundles 是本轮扫描发现的 bundle 全集，包含启用与未启用的。
	Bundles []BundleOverview
}

// resolveDesiredAssets 把 ProjectConfig.EnabledBundles 展开成目标资产集。
//
// 参数：
//   - projectConfig：项目配置；nil 或空时退化为空结果（调用方负责外部的 skip 判断）。
//   - repoDir：仓库工作目录（通常来自 repo.Transaction.WorkDir），用于扫描 vault 下的 bundles。
//   - reporter：用于记录非致命告警（bundle 成员不存在、bundle 引用无法匹配等）。
//
// 返回：
//   - *ResolvedAssets：目标资产列表 + 来源映射 + bundle 概览。
//   - error：致命错误（bundle YAML 非法、命名冲突等）。成员不存在等非致命问题只打 warning，不报错。
//
// 算法：
//  1. 扫描 repoDir/bundles/ 下各 bundle 目录，加载 bundles/<name>/bundle.yaml。
//  2. 对 EnabledBundles 中的每个 bundle 名，在所有 vault 中搜索匹配项：
//     - 找不到：reporter warning + Bundles 不新增条目（因为我们没找到其声明）。
//     - 找到唯一匹配：展开 members，对每个成员检查资产文件是否存在；存在就并入目标集，
//       来源记 "bundle/<name>"；不存在则 reporter warning 跳过该成员。
//     - 命中多个 vault：目前视为 warning 并使用第一个（按 vault 字典序），因为跨 vault
//       bundle 短名冲突是父卡里 #17 明确标为「未验证需求」的场景。
//  3. Bundles 列表同时包含启用和未启用（用于 TUI 的 overview 渲染）。
func resolveDesiredAssets(projectConfig *types.ProjectConfig, repoDir string, reporter Reporter) (*ResolvedAssets, error) {
	reporter = defaultReporter(reporter)
	result := &ResolvedAssets{
		Sources: make(map[string][]string),
	}

	// 1. 扫描 vault 目录并加载所有 bundles（含隐式 vault bundle）。
	// 即使尚无项目配置也要扫描，供 Assets TUI / config init 展示 bundle 列表。
	vaultBundles, bundleOverviews, err := scanVaultBundles(repoDir, reporter)
	if err != nil {
		return nil, err
	}
	result.Bundles = bundleOverviews

	if projectConfig == nil {
		return result, nil
	}

	seen := make(map[string]int) // key -> index in result.Assets
	addAsset := func(asset types.TypedAssetRef, source string) {
		key := assetKey(asset)
		if idx, ok := seen[key]; ok {
			// 已存在，只追加 source
			result.Assets[idx] = asset
			result.Sources[key] = appendUniqueSource(result.Sources[key], source)
			return
		}
		seen[key] = len(result.Assets)
		result.Assets = append(result.Assets, asset)
		result.Sources[key] = []string{source}
	}

	// 2. 展开 bundle 成员。
	for _, bundleName := range projectConfig.EnabledBundles {
		matches := vaultBundles[bundleName]
		if len(matches) == 0 {
			emit(reporter, EventWarn, "pull.bundle",
				fmt.Sprintf("enabled_bundles 引用的 bundle %q 在任何 vault 里都找不到声明，已忽略", bundleName), nil)
			continue
		}
		// 标记启用
		for i := range result.Bundles {
			if result.Bundles[i].Name == bundleName && containsVault(matches, result.Bundles[i].VaultName) {
				result.Bundles[i].Enabled = true
			}
		}

		chosen := matches[0]
		if len(matches) > 1 {
			emit(reporter, EventWarn, "pull.bundle",
				fmt.Sprintf("bundle %q 在多个 vault 中都有声明（%s），将使用 %q；跨 vault bundle 冲突需要手动消歧",
					bundleName, joinVaultNames(matches), chosen.vaultName), nil)
		}

		for _, raw := range chosen.bundle.Members {
			member, parseErr := bundle.ParseMember(raw)
			if parseErr != nil {
				// 正常情况下 LoadBundles 已经校验过，这里不应该发生；稳妥起见仍然 warning。
				emit(reporter, EventWarn, "pull.bundle",
					fmt.Sprintf("bundle %q 成员 %q 解析失败，已跳过：%v", bundleName, raw, parseErr), nil)
				continue
			}
			if !assetFileExists(repoDir, chosen.vaultName, member.Type, member.Name) {
				emit(reporter, EventWarn, "pull.bundle",
					fmt.Sprintf("bundle %q 成员 %s/%s 在 vault %q 内不存在，已跳过",
						bundleName, member.Type, member.Name, chosen.vaultName), nil)
				continue
			}
			asset := types.TypedAssetRef{
				Type:     member.Type,
				AssetRef: types.AssetRef{Name: member.Name, Vault: chosen.vaultName},
			}
			addAsset(asset, "bundle/"+bundleName)
		}
	}

	return result, nil
}

// vaultBundle 跟踪 bundle 所在的 vault。
type vaultBundle struct {
	vaultName string
	bundle    types.Bundle
}

// scanVaultBundles 扫描 repoDir/bundles/ 下各 bundle 目录，加载 bundle.yaml。
// 非致命告警（成员不存在等）通过 reporter 发出。
func scanVaultBundles(repoDir string, reporter Reporter) (map[string][]vaultBundle, []BundleOverview, error) {
	if repoDir == "" {
		return nil, nil, nil
	}
	bundlesDir := filepath.Join(repoDir, types.VaultBundlesDir)
	entries, err := os.ReadDir(bundlesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("读取仓库 bundle 目录失败: %w", err)
	}

	byName := make(map[string][]vaultBundle)
	var overviews []BundleOverview

	bundleNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "" || entry.Name()[0] == '.' {
			continue
		}
		bundleNames = append(bundleNames, entry.Name())
	}
	sort.Strings(bundleNames)

	for _, bundleName := range bundleNames {
		bundlePath := filepath.Join(bundlesDir, bundleName)
		memberExists := func(m types.BundleMember) bool {
			return assetFileExists(repoDir, bundleName, m.Type, m.Name)
		}
		b, warnings, loadErr := bundle.LoadBundle(bundlePath, memberExists)
		if loadErr != nil {
			emit(reporter, EventWarn, "pull.bundle", fmt.Sprintf("加载 bundle %q 失败，跳过: %v", bundleName, loadErr), nil)
			continue
		}
		for _, w := range warnings {
			msg := w.Message
			if w.BundleName != "" {
				msg = fmt.Sprintf("[bundle %s] %s", bundleName, msg)
			} else {
				msg = fmt.Sprintf("[bundle %s] %s", bundleName, msg)
			}
			emit(reporter, EventWarn, "pull.bundle", msg, nil)
		}
		if b.Name == "" {
			continue
		}
		byName[b.Name] = append(byName[b.Name], vaultBundle{vaultName: bundleName, bundle: b})
		overviews = append(overviews, BundleOverview{
			Name:        b.Name,
			Description: b.Description,
			VaultName:   bundleName,
			Members:     append([]string(nil), b.Members...),
			Enabled:     false,
		})
	}

	overviews = synthesizeVaultBundles(repoDir, byName, overviews)
	return byName, overviews, nil
}

// assetFileExists 判定 vault 内指定资产的源文件是否存在（skill 是目录，rule/mcp 是文件）。
func assetFileExists(repoDir, vault, itemType, name string) bool {
	path := resolveAssetFile(repoDir, vault, itemType, name)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func assetKey(asset types.TypedAssetRef) string {
	return asset.Type + ":" + asset.Vault + ":" + asset.Name
}

func appendUniqueSource(sources []string, candidate string) []string {
	for _, s := range sources {
		if s == candidate {
			return sources
		}
	}
	return append(sources, candidate)
}

func containsVault(matches []vaultBundle, vault string) bool {
	for _, m := range matches {
		if m.vaultName == vault {
			return true
		}
	}
	return false
}

func joinVaultNames(matches []vaultBundle) string {
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m.vaultName)
	}
	sort.Strings(names)
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
