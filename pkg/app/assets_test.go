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
		"default/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
		"cli/rules/cli-release-rules.mdc":          "description: test\n",
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
