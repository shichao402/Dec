package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

func TestPullProjectAssetsSkipsWithoutEnabledAssets(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	result, err := PullProjectAssets(context.Background(), t.TempDir(), "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	want := "config.yaml 与本机用户级均无已启用的 bundle"
	if result.SkippedReason != want {
		t.Fatalf("SkippedReason = %q, 期望 %q", result.SkippedReason, want)
	}
}

// TestPullProjectAssetsWarnsOnMissingBundle 覆盖「配置里引用了仓库中已不存在的 bundle」：
// 解析阶段给出告警；无 Git 资产时仍走 secrets（测试注入 stub），整单不报错。
func TestPullProjectAssetsWarnsOnMissingBundle(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/another-workflow/SKILL.md": `---
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
		EnabledBundles: []string{"deleted-vault"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return &secrets.StubClient{} }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	var events []OperationEvent
	result, err := PullProjectAssets(context.Background(), projectRoot, "", ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	wantSkip := "没有有效的已启用 Git 资产可拉取（仍尝试同步 secrets）"
	if result.SkippedReason != wantSkip {
		t.Fatalf("SkippedReason = %q, 期望 %q", result.SkippedReason, wantSkip)
	}
	var sawWarn bool
	for _, event := range events {
		if event.Level == EventWarn && strings.Contains(event.Message, "deleted-vault") {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Fatalf("期望针对失效 bundle 的 warning，事件: %#v", events)
	}
}

func TestPullProjectAssetsInstallsAssetsAndReportsProgress(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/project-workflow/SKILL.md": `---
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
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"default"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	var events []OperationEvent
	result, err := PullProjectAssets(context.Background(), projectRoot, "", ReporterFunc(func(event OperationEvent) {
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

	giPath := filepath.Join(projectRoot, ".gitignore")
	giData, giErr := os.ReadFile(giPath)
	if giErr != nil || !strings.Contains(string(giData), "/.secrets/") {
		t.Fatalf("pull 应确保 .gitignore 包含 /.secrets/: err=%v content=%q", giErr, giData)
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
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/combo/skills/bundle-skill/SKILL.md": "---\nname: bundle-skill\n---\n",
		"bundles/combo/rules/bundle-rule.mdc":        "---\ndescription: rule\n---\n",
		"bundles/combo/bundle.yaml": `name: combo
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
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
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

// substituteAssetVars 之前会用 `_` 吞掉 LoadVarsConfig 的 error，
// 导致 YAML 语法错误下变量被静默丢弃。本用例覆盖修复后的感知路径：
// 解析失败必须通过 reporter 发一个 EventWarn 事件，且消息里带 vars.yaml 路径。
func TestSubstituteAssetVarsReportsProjectVarsParseError(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	projectRoot := t.TempDir()
	decDir := filepath.Join(projectRoot, ".dec")
	if err := os.MkdirAll(decDir, 0755); err != nil {
		t.Fatalf("创建 .dec 失败: %v", err)
	}
	// 写入语法错误的 vars.yaml
	varsPath := filepath.Join(decDir, "vars.yaml")
	if err := os.WriteFile(varsPath, []byte("vars:\n  FOO: [unclosed\n"), 0644); err != nil {
		t.Fatalf("写入损坏的 vars.yaml 失败: %v", err)
	}

	mgr := config.NewProjectConfigManager(projectRoot)

	var events []OperationEvent
	reporter := ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	})

	// projectIDEs 留空即可：LoadVarsConfig 的 error 在进入 IDE 循环之前就应该被报告。
	substituteAssetVars("skill", "any-asset", projectRoot, nil, mgr, reporter)

	var sawWarn bool
	for _, event := range events {
		if event.Level != EventWarn || event.Scope != "pull.vars" {
			continue
		}
		if strings.Contains(event.Message, varsPath) && strings.Contains(event.Message, "解析") {
			sawWarn = true
			break
		}
	}
	if !sawWarn {
		t.Fatalf("期望收到包含 vars.yaml 路径的 pull.vars warn 事件, 实际事件: %#v", events)
	}
}

func TestPullProjectAssetsCleansDeselectedBundleAssets(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/vikunja/bundle.yaml": `name: vikunja
members:
  - skill/vikunja-workflow
`,
		"bundles/combo/skills/bundle-skill/SKILL.md": "---\nname: bundle-skill\n---\n",
		"bundles/combo/bundle.yaml": `name: combo
members:
  - skill/bundle-skill
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	if _, err := PullProjectAssets(context.Background(), projectRoot, "", nil); err != nil {
		t.Fatalf("首次 pull vikunja 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".dec", "cache", "vikunja", "skills", "vikunja-workflow", "SKILL.md")); err != nil {
		t.Fatalf("vikunja cache 应存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-vikunja-workflow", "SKILL.md")); err != nil {
		t.Fatalf("vikunja IDE skill 应存在: %v", err)
	}

	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"combo"},
	}); err != nil {
		t.Fatalf("切换 enabled_bundles 失败: %v", err)
	}

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("切换 bundle 后再 pull 失败: %v", err)
	}
	if result.PulledCount != 1 {
		t.Fatalf("PulledCount = %d, 期望 1", result.PulledCount)
	}
	if len(result.CleanedAssets) == 0 {
		t.Fatalf("应清理 vikunja 残留资产, CleanedAssets=%#v", result.CleanedAssets)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".dec", "cache", "vikunja", "skills", "vikunja-workflow", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("取消后 vikunja cache 应被删除, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-vikunja-workflow", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("取消后 vikunja IDE skill 应被删除, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-bundle-skill", "SKILL.md")); err != nil {
		t.Fatalf("新 bundle skill 应安装: %v", err)
	}
}

func TestPullProjectAssetsCleansWhenAllBundlesDeselected(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/vikunja/bundle.yaml": `name: vikunja
members:
  - skill/vikunja-workflow
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Demo",
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}
	if _, err := PullProjectAssets(context.Background(), projectRoot, "", nil); err != nil {
		t.Fatalf("首次 pull 失败: %v", err)
	}

	landedSecret := filepath.Join(projectRoot, ".secrets", "bundles", "vikunja", "env", "vikunja.env")
	if err := os.MkdirAll(filepath.Dir(landedSecret), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(landedSecret, []byte("X=1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Demo",
		IDEs:           []string{"cursor"},
		EnabledBundles: nil,
	}); err != nil {
		t.Fatalf("清空 enabled_bundles 失败: %v", err)
	}

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("全取消后再 pull 失败: %v", err)
	}
	if result.SkippedReason == "" {
		t.Fatal("期望 SkippedReason 非空")
	}
	if len(result.CleanedAssets) == 0 {
		t.Fatalf("应清理 Dec 资产残留, CleanedAssets=%#v", result.CleanedAssets)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-vikunja-workflow", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("全取消后 IDE skill 应删除, err=%v", err)
	}
	// 已落地的密文件在 .secrets/ 同步根，停用 bundle 不会也不该动它——删除走 Remote 页。
	if _, err := os.Stat(landedSecret); err != nil {
		t.Fatalf("停用 bundle 不应删除已落地的密文件: %v", err)
	}
}
