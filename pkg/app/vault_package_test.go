package app

import (
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func TestSynthesizeVaultPackages_CreatesImplicitPackagePerVault(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"vikunja/rules/vikunja-integration.mdc":    "---\ndescription: test\n---\n",
		"cli/skills/cli-release-workflow/SKILL.md": "---\nname: cli-release-workflow\n---\n",
	})

	byName := make(map[string][]vaultBundle)
	overviews := synthesizeVaultPackages(repoDir, byName, nil)

	if len(overviews) != 2 {
		t.Fatalf("期望 2 个隐式 package, got %d", len(overviews))
	}
	if len(byName["vikunja"]) != 1 {
		t.Fatalf("vikunja package 应存在, got %#v", byName["vikunja"])
	}
	vikunja := byName["vikunja"][0].bundle
	if len(vikunja.Members) != 2 {
		t.Fatalf("vikunja package 应有 2 个成员, got %v", vikunja.Members)
	}
}

func TestSynthesizeVaultPackages_SkipsWhenExplicitBundleExists(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"vikunja/bundles/vikunja.yaml": `name: vikunja
description: explicit
members:
  - skills/vikunja-workflow
`,
	})

	byName := map[string][]vaultBundle{
		"vikunja": {{vaultName: "vikunja", bundle: types.Bundle{Name: "vikunja", Members: []string{"skills/vikunja-workflow"}}}},
	}
	overviews := synthesizeVaultPackages(repoDir, byName, []BundleOverview{{Name: "vikunja", VaultName: "vikunja"}})

	if len(overviews) != 1 {
		t.Fatalf("已有显式 bundle 时不应再合成, got %d overviews", len(overviews))
	}
	if len(byName["vikunja"]) != 1 {
		t.Fatalf("byName 中 vikunja 条目应保持 1 条, got %d", len(byName["vikunja"]))
	}
}

func TestInferBundleEnabledFromStandalone(t *testing.T) {
	members := []AssetSelectionItem{
		{Type: "skill", Vault: "vikunja", Name: "vikunja-workflow"},
		{Type: "rule", Vault: "vikunja", Name: "vikunja-integration"},
	}
	enabled := &types.AssetList{
		Skills: []types.AssetRef{{Name: "vikunja-workflow", Vault: "vikunja"}},
		Rules:  []types.AssetRef{{Name: "vikunja-integration", Vault: "vikunja"}},
	}
	if !inferBundleEnabledFromStandalone(members, enabled) {
		t.Fatal("全部成员 standalone 启用时应推断 package 已启用")
	}
	if inferBundleEnabledFromStandalone(members, &types.AssetList{
		Skills: []types.AssetRef{{Name: "vikunja-workflow", Vault: "vikunja"}},
	}) {
		t.Fatal("部分成员启用时不应推断 package 已启用")
	}
}

func TestResolveDesiredAssets_VaultPackageViaEnabledBundles(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"vikunja/rules/vikunja-integration.mdc":    "---\ndescription: test\n---\n",
	})
	cfg := &types.ProjectConfig{EnabledBundles: []string{"vikunja"}}

	resolved, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	if len(resolved.Assets) != 2 {
		t.Fatalf("启用 vikunja package 应展开 2 个资产, got %d", len(resolved.Assets))
	}
}
