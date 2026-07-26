package app

import (
	"reflect"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

func TestLoadAssetSelectionReturnsEnabledState(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "description: test\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:   []string{"codex"},
		Editor: "code --wait",
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	state, err := LoadAssetSelection(projectRoot, nil)
	if err != nil {
		t.Fatalf("LoadAssetSelection() 失败: %v", err)
	}
	if !state.ExistingConfig {
		t.Fatal("应识别现有项目配置")
	}
	if len(state.Items) != 2 {
		t.Fatalf("Items = %d, 期望 2", len(state.Items))
	}
	var enabledCount int
	for _, item := range state.Items {
		if item.Name == "project-workflow" && item.Type == "skill" && item.Vault == "default" && item.Enabled {
			enabledCount++
		}
		if item.Name == "cli-release-rules" && item.Type == "rule" && item.Vault == "cli" && item.Enabled {
			t.Fatal("cli-release-rules 不应为 enabled")
		}
	}
	if enabledCount != 1 {
		t.Fatalf("project-workflow enabled 匹配数 = %d, 期望 1", enabledCount)
	}
}

func TestLoadAssetSelectionDiscoversBundlesWithoutProjectConfig(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	state, err := LoadAssetSelection(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("LoadAssetSelection() 失败: %v", err)
	}
	if state.ExistingConfig {
		t.Fatal("无 .dec/config.yaml 时不应标记 ExistingConfig")
	}
	if len(state.Items) != 2 {
		t.Fatalf("Items = %d, 期望 2", len(state.Items))
	}
	if len(state.Bundles) < 2 {
		t.Fatalf("无项目配置时仍应发现 bundle, Bundles = %d", len(state.Bundles))
	}
}

func TestSaveAssetSelectionPersistsEnabledAssetsAndPreservesEditor(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:   []string{"codex"},
		Editor: "code --wait",
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := SaveAssetSelection(projectRoot, AssetSaveSelection{
		Items: []AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default", Enabled: true},
			{Name: "cli-release-rules", Type: "rule", Vault: "cli", Enabled: false},
		},
	}, nil)
	if err != nil {
		t.Fatalf("SaveAssetSelection() 失败: %v", err)
	}
	if result.EnabledCount != 1 || result.AvailableCount != 2 {
		t.Fatalf("保存结果计数错误: %+v", result)
	}
	if !result.VarsCreated {
		t.Fatal("首次保存应创建 vars 模板")
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if loaded.Editor != "code --wait" {
		t.Fatalf("Editor = %q, 期望 %q", loaded.Editor, "code --wait")
	}
	if !reflect.DeepEqual(loaded.IDEs, []string{"codex"}) {
		t.Fatalf("IDEs = %#v, 期望 %#v", loaded.IDEs, []string{"codex"})
	}
	if loaded.Available.Count() != 2 {
		t.Fatalf("Available.Count() = %d, 期望 2", loaded.Available.Count())
	}
	if loaded.Enabled.Count() != 1 {
		t.Fatalf("Enabled.Count() = %d, 期望 1", loaded.Enabled.Count())
	}
	if loaded.Enabled.FindAsset("skill", "project-workflow", "default") == nil {
		t.Fatal("enabled 中缺少 project-workflow")
	}
	if loaded.Enabled.FindAsset("rule", "cli-release-rules", "cli") != nil {
		t.Fatal("未启用的 rule 不应写入 enabled")
	}
}

// TestSaveAssetSelectionPreservesEnabledBundles 保证在仅更新单资产勾选时，
// 预先写入磁盘的 EnabledBundles 不会被覆盖清空。
//
// 这是 #93 的关键回归点：旧实现 new 一个 ProjectConfig 再只拷 IDEs/Editor，会吞掉 EnabledBundles。
func TestSaveAssetSelectionPreservesEnabledBundles(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"codex"},
		Editor:         "code --wait",
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	// 不传 EnabledBundles，仅保存单资产状态。
	if _, err := SaveAssetSelection(projectRoot, AssetSaveSelection{
		Items: []AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default", Enabled: true},
		},
	}, nil); err != nil {
		t.Fatalf("SaveAssetSelection() 失败: %v", err)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if !reflect.DeepEqual(loaded.EnabledBundles, []string{"vikunja"}) {
		t.Fatalf("EnabledBundles 被覆盖: %#v, 期望 [vikunja]", loaded.EnabledBundles)
	}
}

// TestSaveAssetSelectionWritesEnabledBundles 保证 TUI 传入的 EnabledBundles
// 原样落盘，并完成去重 / trim 行为。
func TestSaveAssetSelectionWritesEnabledBundles(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs: []string{"codex"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := SaveAssetSelection(projectRoot, AssetSaveSelection{
		EnabledBundles: []string{"combo", "  combo  ", "", "vikunja"},
	}, nil)
	if err != nil {
		t.Fatalf("SaveAssetSelection() 失败: %v", err)
	}
	if result.EnabledBundleCount != 2 {
		t.Fatalf("EnabledBundleCount = %d, 期望 2", result.EnabledBundleCount)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if !reflect.DeepEqual(loaded.EnabledBundles, []string{"combo", "vikunja"}) {
		t.Fatalf("EnabledBundles = %#v, 期望 [combo vikunja]", loaded.EnabledBundles)
	}
}

// TestSaveAssetSelectionEmptyBundlesPersistNil 保证传入空列表时 EnabledBundles 清空为 nil，
// 便于 yaml omitempty 移除该键。
func TestListEffectiveEnabledAssetsIncludesBundleMembers(t *testing.T) {
	state := &AssetSelectionState{
		Items: []AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default", Enabled: true},
			{Name: "cli-release-rules", Type: "rule", Vault: "cli", Enabled: false},
			{Name: "off-asset", Type: "mcp", Vault: "default", Enabled: false},
		},
		Bundles: []AssetBundleOption{
			{
				Name:    "vikunja",
				Enabled: true,
				Members: []AssetSelectionItem{
					{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja", Enabled: false},
					{Name: "vikunja-rules", Type: "rule", Vault: "vikunja", Enabled: false},
				},
			},
			{
				Name:    "cli",
				Enabled: false,
				Members: []AssetSelectionItem{
					{Name: "cli-release-rules", Type: "rule", Vault: "cli", Enabled: false},
				},
			},
		},
	}

	got := ListEffectiveEnabledAssets(state)
	if len(got) != 3 {
		t.Fatalf("有效启用资产数 = %d, 期望 3 (1 standalone + 2 bundle members)", len(got))
	}

	groups := ListEffectiveEnabledGroups(state)
	if len(groups) != 2 {
		t.Fatalf("分组数 = %d, 期望 2 (bundle/vikunja + 独立启用)", len(groups))
	}
	if groups[0].Label != "bundle/vikunja" || len(groups[0].Items) != 2 {
		t.Fatalf("第一组 = %#v, 期望 bundle/vikunja 含 2 项", groups[0])
	}
	if groups[1].Label != "独立启用" || len(groups[1].Items) != 1 || groups[1].Items[0].Name != "project-workflow" {
		t.Fatalf("第二组 = %#v, 期望独立启用 project-workflow", groups[1])
	}
}

func TestListEffectiveEnabledAssetsDeduplicatesStandaloneAndBundle(t *testing.T) {
	state := &AssetSelectionState{
		Items: []AssetSelectionItem{
			{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja", Enabled: true},
		},
		Bundles: []AssetBundleOption{
			{
				Name:    "vikunja",
				Enabled: true,
				Members: []AssetSelectionItem{
					{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja", Enabled: false},
				},
			},
		},
	}

	got := ListEffectiveEnabledAssets(state)
	if len(got) != 1 {
		t.Fatalf("去重后有效启用资产数 = %d, 期望 1", len(got))
	}
	groups := ListEffectiveEnabledGroups(state)
	if len(groups) != 1 || groups[0].Label != "bundle/vikunja" {
		t.Fatalf("重复资产应只出现在 bundle 组: %#v", groups)
	}
}

// TestSaveAssetSelectionDropsMembersOfDeselectedBundle 覆盖「取消 bundle 后勾选自己弹回来」的回归点：
// 早期项目的 enabled_assets 里往往同时留着 bundle 成员的独立引用，若保存时把它们原样写回，
// 下次 LoadAssetSelection 会按 standalone 反推该 bundle 仍启用。
func TestSaveAssetSelectionDropsMembersOfDeselectedBundle(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/vikunja/rules/vikunja-integration.mdc":    "---\ndescription: test\n---\n",
		// cli bundle 有两个成员但只有一个被独立启用：它不会被反推成启用态，
		// 因此该独立引用必须在保存后原样保留。
		"bundles/cli/rules/cli-release-rules.mdc": "---\ndescription: test\n---\n",
		"bundles/cli/rules/cli-extra-rules.mdc":   "---\ndescription: test\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	for _, tc := range []struct {
		name              string
		diskEnabledBundle []string
	}{
		{name: "显式 enabled_bundles", diskEnabledBundle: []string{"vikunja"}},
		{name: "仅由 standalone 反推", diskEnabledBundle: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			mgr := config.NewProjectConfigManager(projectRoot)
			if err := mgr.SaveProjectConfig(&types.ProjectConfig{
				IDEs:           []string{"codex"},
				EnabledBundles: tc.diskEnabledBundle,
				Enabled: &types.AssetList{
					Skills: []types.AssetRef{{Name: "vikunja-workflow", Vault: "vikunja"}},
					Rules: []types.AssetRef{
						{Name: "vikunja-integration", Vault: "vikunja"},
						{Name: "cli-release-rules", Vault: "cli"},
					},
				},
			}); err != nil {
				t.Fatalf("SaveProjectConfig() 失败: %v", err)
			}

			state, err := LoadAssetSelection(projectRoot, nil)
			if err != nil {
				t.Fatalf("LoadAssetSelection() 失败: %v", err)
			}
			if !bundleEnabledInState(state, "vikunja") {
				t.Fatal("前置条件不成立：vikunja 初始应为启用态")
			}

			// 模拟 TUI 取消 vikunja 后保存：Items 仍带着磁盘上的成员启用态。
			if _, err := SaveAssetSelection(projectRoot, AssetSaveSelection{
				Items:          state.Items,
				EnabledBundles: []string{},
			}, nil); err != nil {
				t.Fatalf("SaveAssetSelection() 失败: %v", err)
			}

			reloaded, err := LoadAssetSelection(projectRoot, nil)
			if err != nil {
				t.Fatalf("重新 LoadAssetSelection() 失败: %v", err)
			}
			if bundleEnabledInState(reloaded, "vikunja") {
				t.Fatal("取消并保存后重新加载，vikunja 不应再是启用态")
			}

			saved, err := mgr.LoadProjectConfig()
			if err != nil {
				t.Fatalf("LoadProjectConfig() 失败: %v", err)
			}
			if saved.Enabled.FindAsset("skill", "vikunja-workflow", "vikunja") != nil {
				t.Fatal("被取消 bundle 的成员不应留在 enabled_assets")
			}
			if saved.Enabled.FindAsset("rule", "cli-release-rules", "cli") == nil {
				t.Fatal("其它 bundle 的成员引用不应被牵连清理")
			}
		})
	}
}

// TestSaveAssetSelectionKeepsMembersOfStillEnabledBundle 保证只取消其中一个 bundle 时，
// 仍启用的 bundle 的成员引用不被误剔除。
func TestSaveAssetSelectionKeepsMembersOfStillEnabledBundle(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja", "cli"},
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "vikunja-workflow", Vault: "vikunja"}},
			Rules:  []types.AssetRef{{Name: "cli-release-rules", Vault: "cli"}},
		},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	state, err := LoadAssetSelection(projectRoot, nil)
	if err != nil {
		t.Fatalf("LoadAssetSelection() 失败: %v", err)
	}
	if _, err := SaveAssetSelection(projectRoot, AssetSaveSelection{
		Items:          state.Items,
		EnabledBundles: []string{"cli"},
	}, nil); err != nil {
		t.Fatalf("SaveAssetSelection() 失败: %v", err)
	}

	reloaded, err := LoadAssetSelection(projectRoot, nil)
	if err != nil {
		t.Fatalf("重新 LoadAssetSelection() 失败: %v", err)
	}
	if bundleEnabledInState(reloaded, "vikunja") {
		t.Fatal("被取消的 vikunja 不应再是启用态")
	}
	if !bundleEnabledInState(reloaded, "cli") {
		t.Fatal("未被取消的 cli 应保持启用态")
	}
}

func bundleEnabledInState(state *AssetSelectionState, name string) bool {
	for _, bo := range state.Bundles {
		if bo.Name == name && bo.Enabled {
			return true
		}
	}
	return false
}

func TestSaveAssetSelectionEmptyBundlesPersistNil(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"codex"},
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	if _, err := SaveAssetSelection(projectRoot, AssetSaveSelection{
		EnabledBundles: []string{},
	}, nil); err != nil {
		t.Fatalf("SaveAssetSelection() 失败: %v", err)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if loaded.EnabledBundles != nil {
		t.Fatalf("EnabledBundles = %#v, 期望 nil", loaded.EnabledBundles)
	}
}
