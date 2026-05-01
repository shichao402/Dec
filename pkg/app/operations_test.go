package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

func TestPullProjectAssetsSkipsWithoutEnabledAssets(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	result, err := PullProjectAssets(t.TempDir(), "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.SkippedReason != "config.yaml 中没有已启用的资产或 bundle" {
		t.Fatalf("SkippedReason = %q, 期望 %q", result.SkippedReason, "config.yaml 中没有已启用的资产或 bundle")
	}
}

func TestPullProjectAssetsSkipsWhenEnabledAssetsDoNotExistInAvailable(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"default/skills/another-workflow/SKILL.md": `---
name: another-workflow
---
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		Available: &types.AssetList{
			Skills: []types.AssetRef{{Name: "another-workflow", Vault: "default"}},
		},
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := PullProjectAssets(projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.SkippedReason != "没有有效的已启用资产可拉取" {
		t.Fatalf("SkippedReason = %q, 期望 %q", result.SkippedReason, "没有有效的已启用资产可拉取")
	}
	if len(result.ValidationWarnings) != 1 || !strings.Contains(result.ValidationWarnings[0], "project-workflow") {
		t.Fatalf("ValidationWarnings = %#v, 期望包含 project-workflow", result.ValidationWarnings)
	}
}

func TestPullProjectAssetsInstallsAssetsAndReportsProgress(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"default/skills/project-workflow/SKILL.md": `---
name: project-workflow
---
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs: []string{"cursor"},
		Available: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	var events []OperationEvent
	result, err := PullProjectAssets(projectRoot, "", ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.RequestedCount != 1 || result.PulledCount != 1 || result.FailedCount != 0 {
		t.Fatalf("结果计数异常: %+v", result)
	}
	if len(result.EffectiveIDEs) != 1 || result.EffectiveIDEs[0] != "cursor" {
		t.Fatalf("EffectiveIDEs = %#v, 期望 %#v", result.EffectiveIDEs, []string{"cursor"})
	}
	if strings.TrimSpace(result.VersionCommit) == "" {
		t.Fatal("VersionCommit 不应为空")
	}

	if _, err := os.Stat(filepath.Join(projectRoot, ".dec", "cache", "default", "skills", "project-workflow", "SKILL.md")); err != nil {
		t.Fatalf("缓存文件应存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-project-workflow", "SKILL.md")); err != nil {
		t.Fatalf("安装后的 skill 应存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".dec", ".version")); err != nil {
		t.Fatalf(".dec/.version 应存在: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectRoot, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("dec 不应写入 .gitignore, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "mise.local.toml")); !os.IsNotExist(err) {
		t.Fatalf("dec 不应写入 mise.local.toml, stat err = %v", err)
	}

	var sawStart, sawFinish bool
	for _, event := range events {
		if event.Scope == "pull.start" {
			sawStart = true
		}
		if event.Scope == "pull.finish" {
			sawFinish = true
		}
	}
	if !sawStart || !sawFinish {
		t.Fatalf("事件流缺少开始或结束事件: %#v", events)
	}
}

func TestPullProjectAssetsInstallsBundleMembers(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"default/skills/bundle-skill/SKILL.md": "---\nname: bundle-skill\n---\n",
		"default/rules/bundle-rule.mdc":        "---\ndescription: rule\n---\n",
		"default/bundles/combo.yaml": `name: combo
description: bundle-integration test
members:
  - skill/bundle-skill
  - rule/bundle-rule
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	// 注意：EnabledBundles 填 combo，Enabled 为空。Available 里也没有这两个成员——
	// 验证 bundle-sourced 资产走豁免分支能装上。
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs: []string{"cursor"},
		Available: &types.AssetList{
			Skills: []types.AssetRef{{Name: "some-other-skill", Vault: "default"}},
		},
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := PullProjectAssets(projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.RequestedCount != 2 || result.PulledCount != 2 {
		t.Fatalf("结果计数异常: %+v", result)
	}

	// 两个成员都应以 bundle/combo 作为来源
	if len(result.AssetSources) != 2 {
		t.Fatalf("AssetSources 长度 = %d, 期望 2; 内容 %#v", len(result.AssetSources), result.AssetSources)
	}
	for key, sources := range result.AssetSources {
		if len(sources) != 1 || sources[0] != "bundle/combo" {
			t.Fatalf("AssetSources[%s] = %#v, 期望 [bundle/combo]", key, sources)
		}
	}

	// BundleOverviews 里 combo 被标记启用
	var sawEnabledCombo bool
	for _, b := range result.BundleOverviews {
		if b.Name == "combo" && b.Enabled {
			sawEnabledCombo = true
		}
	}
	if !sawEnabledCombo {
		t.Fatalf("BundleOverviews = %#v, 期望包含 enabled=true 的 combo", result.BundleOverviews)
	}

	// 两个成员实际安装到 .cursor/ 下
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-bundle-skill", "SKILL.md")); err != nil {
		t.Fatalf("bundle skill 未安装: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "rules", "dec-bundle-rule.mdc")); err != nil {
		t.Fatalf("bundle rule 未安装: %v", err)
	}
}
