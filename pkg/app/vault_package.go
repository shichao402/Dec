package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/types"
)

// synthesizeVaultPackages 为缺少同名 bundle 声明的 vault 合成隐式 package。
//
// 用户视角的 package（vikunja、cli、default 等）通常与 vault 同名。
// 若 vault/bundles/<vault>.yaml 已存在则尊重显式声明，不再合成。
func synthesizeVaultPackages(repoDir string, byName map[string][]vaultBundle, overviews []BundleOverview) []BundleOverview {
	if repoDir == "" {
		return overviews
	}
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return overviews
	}

	explicitVaultPackage := make(map[string]struct{})
	for name, matches := range byName {
		for _, m := range matches {
			if m.vaultName == name {
				explicitVaultPackage[name] = struct{}{}
				break
			}
		}
	}

	vaultNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "" || entry.Name()[0] == '.' {
			continue
		}
		vaultNames = append(vaultNames, entry.Name())
	}
	sort.Strings(vaultNames)

	for _, vaultName := range vaultNames {
		if _, ok := explicitVaultPackage[vaultName]; ok {
			continue
		}
		members := listVaultAssetMembers(repoDir, vaultName)
		if len(members) == 0 {
			continue
		}
		b := types.Bundle{
			Name:        vaultName,
			Description: fmt.Sprintf("%s 资产包（vault 内全部资产）", vaultName),
			Members:     members,
		}
		byName[vaultName] = append(byName[vaultName], vaultBundle{vaultName: vaultName, bundle: b})
		overviews = append(overviews, BundleOverview{
			Name:        b.Name,
			Description: b.Description,
			VaultName:   vaultName,
			Members:     append([]string(nil), b.Members...),
			Enabled:     false,
		})
	}
	return overviews
}

// listVaultAssetMembers 列出 vault 内全部资产，返回 bundle members 引用（skills/rules/mcp 前缀）。
func listVaultAssetMembers(repoDir, vaultName string) []string {
	vaultPath := filepath.Join(repoDir, vaultName)
	type memberRef struct {
		prefix string
		name   string
	}
	var refs []memberRef

	for _, spec := range []struct {
		dir    string
		prefix string
		trim   func(string) string
	}{
		{"skills", "skills", func(s string) string { return s }},
		{"rules", "rules", func(s string) string { return strings.TrimSuffix(s, ".mdc") }},
		{"mcp", "mcp", func(s string) string { return strings.TrimSuffix(s, ".json") }},
	} {
		dir := filepath.Join(vaultPath, spec.dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.Name() == ".gitkeep" {
				continue
			}
			if spec.dir == "skills" {
				if entry.IsDir() {
					refs = append(refs, memberRef{prefix: spec.prefix, name: entry.Name()})
				}
				continue
			}
			if entry.IsDir() {
				continue
			}
			refs = append(refs, memberRef{prefix: spec.prefix, name: spec.trim(entry.Name())})
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

// inferBundleEnabledFromStandalone 当 bundle 成员在 enabled 中全部以 standalone 启用时，
// 视为该 package 已启用（便于从旧配置迁移到包级呈现）。
func inferBundleEnabledFromStandalone(members []AssetSelectionItem, enabled *types.AssetList) bool {
	if enabled == nil || enabled.IsEmpty() || len(members) == 0 {
		return false
	}
	for _, mb := range members {
		if enabled.FindAsset(mb.Type, mb.Name, mb.Vault) == nil {
			return false
		}
	}
	return true
}
