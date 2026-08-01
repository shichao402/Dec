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
		IDEs:           []string{"codex"},
		Editor:         "code --wait",
		EnabledBundles: []string{"default"},
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
	if len(state.Bundles) != 2 {
		t.Fatalf("Bundles = %d, 期望 2", len(state.Bundles))
	}
	if !bundleEnabledInState(state, "default") {
		t.Fatal("default bundle 应为启用态")
	}
	if bundleEnabledInState(state, "cli") {
		t.Fatal("cli bundle 未被引用，不应为启用态")
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
	if len(state.Bundles) < 2 {
		t.Fatalf("无项目配置时仍应发现 bundle, Bundles = %d", len(state.Bundles))
	}
	for _, bo := range state.Bundles {
		if bo.Enabled {
			t.Fatalf("无项目配置时不应有已启用 bundle: %s", bo.Name)
		}
	}
}

// TestSaveEnabledBundlesPreservesOtherFields 保证保存 bundle 勾选时，
// 未参与本次交互的字段（IDEs / Editor）原样保留。
func TestSaveEnabledBundlesPreservesOtherFields(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:   []string{"codex"},
		Editor: "code --wait",
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := SaveEnabledBundles(projectRoot, []string{"default"}, nil)
	if err != nil {
		t.Fatalf("SaveEnabledBundles() 失败: %v", err)
	}
	if result.EnabledBundleCount != 1 {
		t.Fatalf("EnabledBundleCount = %d, 期望 1", result.EnabledBundleCount)
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
	if !reflect.DeepEqual(loaded.EnabledBundles, []string{"default"}) {
		t.Fatalf("EnabledBundles = %#v, 期望 [default]", loaded.EnabledBundles)
	}
}

// TestSaveEnabledBundlesNormalizes 保证传入列表会去重、去空白。
func TestSaveEnabledBundlesNormalizes(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{IDEs: []string{"codex"}}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := SaveEnabledBundles(projectRoot, []string{"combo", "  combo  ", "", "vikunja"}, nil)
	if err != nil {
		t.Fatalf("SaveEnabledBundles() 失败: %v", err)
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

// TestSaveEnabledBundlesEmptyPersistsNil 保证传入空列表时 EnabledBundles 清空为 nil，
// 便于 yaml omitempty 移除该键。
func TestSaveEnabledBundlesEmptyPersistsNil(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"codex"},
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	if _, err := SaveEnabledBundles(projectRoot, []string{}, nil); err != nil {
		t.Fatalf("SaveEnabledBundles() 失败: %v", err)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if loaded.EnabledBundles != nil {
		t.Fatalf("EnabledBundles = %#v, 期望 nil", loaded.EnabledBundles)
	}
}

// TestSaveEnabledBundlesRoundTrip 覆盖「取消 bundle 后勾选自己弹回来」的回归点：
// 取消并保存后重新加载，该 bundle 不应再被判定为启用。
func TestSaveEnabledBundlesRoundTrip(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/vikunja/rules/vikunja-integration.mdc":    "---\ndescription: test\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"codex"},
		EnabledBundles: []string{"vikunja", "cli"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	state, err := LoadAssetSelection(projectRoot, nil)
	if err != nil {
		t.Fatalf("LoadAssetSelection() 失败: %v", err)
	}
	if !bundleEnabledInState(state, "vikunja") || !bundleEnabledInState(state, "cli") {
		t.Fatal("前置条件不成立：两个 bundle 初始都应为启用态")
	}

	if _, err := SaveEnabledBundles(projectRoot, []string{"cli"}, nil); err != nil {
		t.Fatalf("SaveEnabledBundles() 失败: %v", err)
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

func TestListEffectiveEnabledAssetsExpandsBundleMembers(t *testing.T) {
	state := &AssetSelectionState{
		Bundles: []AssetBundleOption{
			{
				Name:    "vikunja",
				Enabled: true,
				Members: []AssetSelectionItem{
					{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja"},
					{Name: "vikunja-rules", Type: "rule", Vault: "vikunja"},
				},
			},
			{
				Name:    "cli",
				Enabled: false,
				Members: []AssetSelectionItem{
					{Name: "cli-release-rules", Type: "rule", Vault: "cli"},
				},
			},
		},
	}

	got := ListEffectiveEnabledAssets(state)
	if len(got) != 2 {
		t.Fatalf("有效启用资产数 = %d, 期望 2（仅 vikunja 成员）", len(got))
	}

	groups := ListEffectiveEnabledGroups(state)
	if len(groups) != 1 {
		t.Fatalf("分组数 = %d, 期望 1", len(groups))
	}
	if groups[0].Label != "bundle/vikunja" || len(groups[0].Items) != 2 {
		t.Fatalf("分组 = %#v, 期望 bundle/vikunja 含 2 项", groups[0])
	}
}

func TestListEffectiveEnabledAssetsDeduplicatesAcrossBundles(t *testing.T) {
	shared := AssetSelectionItem{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja"}
	state := &AssetSelectionState{
		Bundles: []AssetBundleOption{
			{Name: "vikunja", Enabled: true, Members: []AssetSelectionItem{shared}},
			{Name: "combo", Enabled: true, Members: []AssetSelectionItem{shared}},
		},
	}

	got := ListEffectiveEnabledAssets(state)
	if len(got) != 1 {
		t.Fatalf("去重后有效启用资产数 = %d, 期望 1", len(got))
	}
	if len(ListEffectiveEnabledGroups(state)) != 2 {
		t.Fatal("两个 bundle 都应各自成组")
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
