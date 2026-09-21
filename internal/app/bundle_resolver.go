// bundle_resolver.go 负责把消费声明 requires 里 pin 为 vault 的项目（ADR 0029）解析为本轮
// pull 的目标资产集合，并记录每个资产的来源（p/<name> 或 depends_on <name>）。
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
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/types"
)

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
	// Enabled 表示该项目在当前平面被订阅（requires）或是作者身份家项目。
	Enabled bool
	// Model 在 ADR 0016 仓库中为 "p"；空值表示 legacy bundle。
	Model string
	// Home 是作者身份家项目；Required 表示已在 requires 中订阅。
	Home     bool
	Required bool
	// Quadrants 是四象限资产计数，key 为 public/user 等稳定路径。
	Quadrants map[string]int
	// Tags 来自 <p>/dec.yaml，用于推荐与筛选；当前 Console 可配置的只有 global。
	Tags []string
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
	// MissingProjects records direct project references that could not be resolved.
	MissingProjects []string
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
//     来源记 "bundle/<name>"；不存在则 reporter warning 跳过该成员。
//     - 命中多个 vault：目前视为 warning 并使用第一个（按 vault 字典序），因为跨 vault
//     bundle 短名冲突是父卡里 #17 明确标为「未验证需求」的场景。
//  3. Bundles 列表同时包含启用和未启用（用于 TUI 的 overview 渲染）。
func resolveDesiredAssets(projectConfig *types.ProjectConfig, repoDir string, reporter Reporter) (*ResolvedAssets, error) {
	return resolveDesiredAssetsForPlane(projectConfig, repoDir, WorkspaceProject, reporter)
}

// resolveDesiredAssetsForPlane 只暴露并解析当前工作空间平面的 bundle。
// scope 为空在 bundle.LoadBundle 中已规范化为 project。
func resolveDesiredAssetsForPlane(projectConfig *types.ProjectConfig, repoDir string, plane WorkspacePlane, reporter Reporter) (*ResolvedAssets, error) {
	reporter = defaultReporter(reporter)
	projects, err := pmodel.Scan(repoDir)
	if err != nil {
		return nil, err
	}
	return resolvePAssets(projectConfig, projects, plane, reporter)
}

// resolvePAssets 按 ADR 0029 解析个人私仓订阅：
// requires 里 pin 为 vault 的项目（项目平面另加作者身份 home）装本平面全部资产，
// 它们 depends_on 的传递闭包只装 public。
func resolvePAssets(projectConfig *types.ProjectConfig, projects map[string]*pmodel.Loaded, plane WorkspacePlane, reporter Reporter) (*ResolvedAssets, error) {
	result := &ResolvedAssets{Sources: make(map[string][]string)}
	enabled := make(map[string]struct{})
	selected := make([]types.TypedAssetRef, 0)

	addProjectAssets := func(name string, visibility *types.AssetVisibility, source string) bool {
		p, ok := projects[name]
		if !ok {
			emit(reporter, EventWarn, "pull.project", fmt.Sprintf("订阅的项目 %q 不在私仓，已忽略", name), nil)
			result.MissingProjects = appendUniqueSource(result.MissingProjects, name)
			return false
		}
		enabled[name] = struct{}{}
		for _, asset := range p.Assets {
			wantPlane := types.AssetPlaneLocal
			if plane == WorkspaceUser || plane == WorkspaceGlobal {
				wantPlane = types.AssetPlaneGlobal
			}
			if types.CanonicalAssetPlane(asset.Plane) != wantPlane || (visibility != nil && asset.Visibility != *visibility) {
				continue
			}
			selected = append(selected, asset)
			result.Sources[assetKey(asset)] = appendUniqueSource(result.Sources[assetKey(asset)], source)
		}
		return true
	}

	if projectConfig != nil {
		seeds := workspaceVaultSeeds(projectConfig, plane)
		public := types.AssetVisibilityPublic
		visited := make(map[string]struct{}, len(seeds))
		queue := make([]string, 0, len(seeds))
		for _, name := range seeds {
			if !addProjectAssets(name, nil, "p/"+name) {
				continue
			}
			visited[name] = struct{}{}
			if p, ok := projects[name]; ok {
				queue = append(queue, p.Manifest.DependsOn...)
			}
		}
		for len(queue) > 0 {
			name := queue[0]
			queue = queue[1:]
			if _, seen := visited[name]; seen {
				continue
			}
			visited[name] = struct{}{}
			// 同名项目已由官方 requires 消费时，私仓 depends_on 不能再落地它。
			// 否则 pull 会先安装官方快照，随后用私仓旧副本覆盖同一 cache / IDE
			// 路径，形成「版本戳已更新、资产正文仍旧」的假成功。
			if pin, declared := projectConfig.Requires[name]; declared && !types.IsVaultPin(pin) {
				continue
			}
			if !addProjectAssets(name, &public, "depends_on "+name) {
				continue
			}
			if p, ok := projects[name]; ok {
				queue = append(queue, p.Manifest.DependsOn...)
			}
		}
	}

	seenTarget := make(map[string]types.TypedAssetRef)
	for _, asset := range selected {
		target := asset.Type + ":" + asset.Name
		if previous, ok := seenTarget[target]; ok {
			if previous.Vault != asset.Vault {
				return nil, fmt.Errorf("项目 %q 与 %q 在 %s 平面竞争同一安装目标 %s/%s",
					previous.Vault, asset.Vault, asset.Plane, asset.Type, asset.Name)
			}
			return nil, fmt.Errorf("项目 %q 的 %s/%s 与 %s/%s 包含同名资产 %s/%s，安装目标冲突",
				asset.Vault, previous.Visibility, previous.Plane, asset.Visibility, asset.Plane, asset.Type, asset.Name)
		}
		seenTarget[target] = asset
		result.Assets = append(result.Assets, asset)
	}

	names := make([]string, 0, len(projects))
	for name := range projects {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := projects[name]
		members := make([]string, 0, len(p.Assets))
		for _, asset := range p.Assets {
			members = append(members, fmt.Sprintf("%s/%s/%s/%s", asset.Visibility, asset.Plane, typeSubDir(asset.Type), asset.Name))
		}
		_, isEnabled := enabled[name]
		result.Bundles = append(result.Bundles, BundleOverview{
			Name: name, Description: p.Manifest.Description, VaultName: name, Members: members, Enabled: isEnabled,
			Model: "p", Quadrants: countPQuadrants(p.Assets), Tags: append([]string(nil), p.Manifest.Tags...),
		})
	}
	if projectConfig != nil {
		home := strings.TrimSpace(projectConfig.ProjectName)
		for i := range result.Bundles {
			result.Bundles[i].Home = plane == WorkspaceProject && result.Bundles[i].Name == home
			result.Bundles[i].Required = projectConfig.Requires.IsVault(result.Bundles[i].Name)
		}
	}
	return result, nil
}

// workspaceVaultSeeds 返回本工作区直接订阅的私仓项目：requires 里的 vault pin，
// 项目平面另加作者身份 home（它从工作树创作，但安装目标与订阅一致）。
func workspaceVaultSeeds(projectConfig *types.ProjectConfig, plane WorkspacePlane) []string {
	if projectConfig == nil {
		return nil
	}
	seeds := make([]string, 0, len(projectConfig.Requires)+1)
	seen := make(map[string]struct{})
	if plane == WorkspaceProject {
		if home := strings.TrimSpace(projectConfig.ProjectName); home != "" {
			seeds = append(seeds, home)
			seen[home] = struct{}{}
		}
	}
	for _, name := range projectConfig.Requires.VaultProjects() {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		seeds = append(seeds, name)
	}
	return seeds
}

func countPQuadrants(assets []types.TypedAssetRef) map[string]int {
	out := map[string]int{
		"public/global": 0, "private/global": 0,
		"public/local": 0, "private/local": 0,
	}
	for _, asset := range assets {
		out[string(asset.Visibility)+"/"+string(types.CanonicalAssetPlane(asset.Plane))]++
	}
	return out
}

func containsName(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
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
	return asset.Type + ":" + asset.Vault + ":" + string(asset.Visibility) + ":" + string(asset.Plane) + ":" + asset.Name
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
