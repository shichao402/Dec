package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/types"
)

// synthesizeVaultBundles 为缺少 bundle.yaml 的 bundle 目录合成隐式 bundle。
//
// 用户视角的 bundle（vikunja、cli、default 等）对应 bundles/<name>/ 目录。
// 若 bundles/<name>/bundle.yaml 已存在则尊重显式声明，不再合成。
func synthesizeVaultBundles(repoDir string, byName map[string][]vaultBundle, overviews []BundleOverview) []BundleOverview {
	if repoDir == "" {
		return overviews
	}
	bundlesDir := filepath.Join(repoDir, types.VaultBundlesDir)
	entries, err := os.ReadDir(bundlesDir)
	if err != nil {
		return overviews
	}

	explicitBundle := make(map[string]struct{})
	for name, matches := range byName {
		for _, m := range matches {
			if m.vaultName == name {
				explicitBundle[name] = struct{}{}
				break
			}
		}
	}

	bundleNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "" || entry.Name()[0] == '.' {
			continue
		}
		bundleNames = append(bundleNames, entry.Name())
	}
	sort.Strings(bundleNames)

	for _, bundleName := range bundleNames {
		if _, ok := explicitBundle[bundleName]; ok {
			continue
		}
		members := listBundleAssetMembers(repoDir, bundleName)
		if len(members) == 0 {
			continue
		}
		b := types.Bundle{
			Name:        bundleName,
			Scope:       types.BundleScopeProject,
			Description: fmt.Sprintf("%s 资产包（bundle 内全部资产）", bundleName),
			Members:     members,
		}
		byName[bundleName] = append(byName[bundleName], vaultBundle{vaultName: bundleName, bundle: b})
		overviews = append(overviews, BundleOverview{
			Name:        b.Name,
			Description: b.Description,
			VaultName:   bundleName,
			Members:     append([]string(nil), b.Members...),
			Enabled:     false,
		})
	}
	return overviews
}

// listBundleAssetMembers 列出 bundles/<name>/ 内全部资产，返回 bundle members 引用。
// 目录清单来自 bundle.VaultAssetKinds，避免与 Bundles/Remote/cache 扫描分裂。
func listBundleAssetMembers(repoDir, bundleName string) []string {
	bundlePath := filepath.Join(repoDir, types.VaultBundlesDir, bundleName)
	type memberRef struct {
		prefix string
		name   string
	}
	var refs []memberRef

	for _, kind := range bundle.VaultAssetKinds {
		dir := filepath.Join(bundlePath, kind.Dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.Name() == ".gitkeep" {
				continue
			}
			if kind.DirEntries {
				if entry.IsDir() {
					refs = append(refs, memberRef{prefix: kind.Dir, name: entry.Name()})
				}
				continue
			}
			if entry.IsDir() {
				continue
			}
			refs = append(refs, memberRef{
				prefix: kind.Dir,
				name:   bundle.AssetEntryName(kind, entry.Name()),
			})
		}
	}

	sort.Slice(refs, func(i, j int) bool {
		if refs[i].prefix != refs[j].prefix {
			return refs[i].prefix < refs[j].prefix
		}
		return refs[i].name < refs[j].name
	})

	members := make([]string, 0, len(refs))
	for _, ref := range refs {
		members = append(members, ref.prefix+"/"+ref.name)
	}
	return members
}
