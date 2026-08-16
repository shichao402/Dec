package app

import (
	"testing"

	"github.com/shichao402/Dec/internal/types"
)

func TestSynthesizeVaultBundles_CreatesImplicitBundlePerVault(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/vikunja/rules/vikunja-integration.mdc":    "---\ndescription: test\n---\n",
		"bundles/cli/skills/cli-release-workflow/SKILL.md": "---\nname: cli-release-workflow\n---\n",
	})

	byName := make(map[string][]vaultBundle)
	overviews := synthesizeVaultBundles(repoDir, byName, nil)

	if len(overviews) != 2 {
		t.Fatalf("期望 2 个隐式 bundle, got %d", len(overviews))
	}
	if len(byName["vikunja"]) != 1 {
		t.Fatalf("vikunja bundle 应存在, got %#v", byName["vikunja"])
	}
	vikunja := byName["vikunja"][0].bundle
	if len(vikunja.Members) != 2 {
		t.Fatalf("vikunja bundle 应有 2 个成员, got %v", vikunja.Members)
	}
}

func TestListBundleAssetMembers_IncludesCommands(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"bundles/pkv/commands/pkv/pkv-download.md": "# pkv-download\n",
		"bundles/pkv/skills/helper/SKILL.md":       "---\nname: helper\n---\n",
	})
	got := listBundleAssetMembers(repoDir, "pkv")
	want := map[string]bool{"commands/pkv": true, "skills/helper": true}
	if len(got) != len(want) {
		t.Fatalf("members = %#v, want %d entries", got, len(want))
	}
	for _, m := range got {
		if !want[m] {
			t.Fatalf("unexpected member %q in %#v", m, got)
		}
	}
}

func TestListBundleAssetMembers_AllKnownKinds(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"bundles/demo/skills/s1/SKILL.md": "---\nname: s1\n---\n",
		"bundles/demo/commands/c1/note.md": "# c1\n",
		"bundles/demo/rules/r1.mdc":        "---\ndescription: r1\n---\n",
		"bundles/demo/mcp/m1.json":         `{"mcpServers":{}}`,
	})
	got := listBundleAssetMembers(repoDir, "demo")
	want := map[string]bool{
		"skills/s1":   true,
		"commands/c1": true,
		"rules/r1":    true,
		"mcp/m1":      true,
	}
	if len(got) != len(want) {
		t.Fatalf("members = %#v, want %d entries covering all VaultAssetKinds", got, len(want))
	}
	for _, m := range got {
		if !want[m] {
			t.Fatalf("unexpected member %q in %#v", m, got)
		}
	}
}

func TestSynthesizeVaultBundles_SkipsWhenExplicitBundleExists(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/vikunja/bundle.yaml": `name: vikunja
description: explicit
members:
  - skills/vikunja-workflow
`,
	})

	byName := map[string][]vaultBundle{
		"vikunja": {{vaultName: "vikunja", bundle: types.Bundle{Name: "vikunja", Members: []string{"skills/vikunja-workflow"}}}},
	}
	overviews := synthesizeVaultBundles(repoDir, byName, []BundleOverview{{Name: "vikunja", VaultName: "vikunja"}})

	if len(overviews) != 1 {
		t.Fatalf("已有显式 bundle 时不应再合成, got %d overviews", len(overviews))
	}
	if len(byName["vikunja"]) != 1 {
		t.Fatalf("byName 中 vikunja 条目应保持 1 条, got %d", len(byName["vikunja"]))
	}
}

func TestResolveDesiredAssets_VaultBundleViaEnabledBundles(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/vikunja/rules/vikunja-integration.mdc":    "---\ndescription: test\n---\n",
	})
	cfg := &types.ProjectConfig{EnabledBundles: []string{"vikunja"}}

	resolved, err := resolveDesiredAssets(cfg, repoDir, nil)
	if err != nil {
		t.Fatalf("resolveDesiredAssets() 失败: %v", err)
	}
	if len(resolved.Assets) != 2 {
		t.Fatalf("启用 vikunja bundle 应展开 2 个资产, got %d", len(resolved.Assets))
	}
}
