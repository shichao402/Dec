package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

func TestRemoveBundleRejectsWhenUnconfirmed(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	_, err := RemoveBundle(RemoveBundleInput{
		ProjectRoot: t.TempDir(),
		BundleName:  "vikunja",
		Confirmed:   false,
	}, nil)
	if !errors.Is(err, ErrRemoveNotConfirmed) {
		t.Fatalf("未确认时应返回 ErrRemoveNotConfirmed, 实际: %v", err)
	}
}

func TestRemoveBundleRejectsEmptyName(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	_, err := RemoveBundle(RemoveBundleInput{
		ProjectRoot: t.TempDir(),
		BundleName:  "",
		Confirmed:   true,
	}, nil)
	if err == nil {
		t.Fatal("空 bundle 名应返回错误")
	}
}

func TestRemoveBundleRemovesRemoteAndCleansLocal(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/vikunja/rules/vikunja-rules.mdc":          "---\ndescription: test\n---\n",
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

	cacheBundle := filepath.Join(projectRoot, ".dec", "cache", "vikunja")
	cacheSkill := filepath.Join(cacheBundle, "skills", "vikunja-workflow")
	if err := os.MkdirAll(cacheSkill, 0755); err != nil {
		t.Fatalf("创建 cache 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkill, "SKILL.md"), []byte("cache"), 0644); err != nil {
		t.Fatalf("写 cache 失败: %v", err)
	}
	ideSkill := filepath.Join(projectRoot, ".cursor", "skills", "dec-vikunja-workflow")
	if err := os.MkdirAll(ideSkill, 0755); err != nil {
		t.Fatalf("创建 IDE skill 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ideSkill, "SKILL.md"), []byte("installed"), 0644); err != nil {
		t.Fatalf("写 IDE skill 失败: %v", err)
	}

	var events []OperationEvent
	result, err := RemoveBundle(RemoveBundleInput{
		ProjectRoot: projectRoot,
		BundleName:  "vikunja",
		Members: []AssetSelectionItem{
			{Name: "vikunja-workflow", Type: "skill", Vault: "vikunja"},
			{Name: "vikunja-rules", Type: "rule", Vault: "vikunja"},
		},
		Confirmed: true,
	}, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("RemoveBundle() 失败: %v", err)
	}

	if result.BundleName != "vikunja" {
		t.Fatalf("BundleName = %q, 期望 %q", result.BundleName, "vikunja")
	}
	if result.MemberCount != 2 {
		t.Fatalf("MemberCount = %d, 期望 2", result.MemberCount)
	}
	if result.VersionCommit == "" {
		t.Fatal("VersionCommit 不应为空")
	}
	if !result.RemovedFromCache {
		t.Fatal("应清理 bundle 缓存")
	}
	if !result.ConfigUpdated {
		t.Fatal("应更新项目配置")
	}
	if len(result.RemovedFromIDEs) == 0 {
		t.Fatal("应至少清理 1 个 IDE")
	}

	if _, err := os.Stat(ideSkill); !os.IsNotExist(err) {
		t.Fatalf("IDE skill 目录应已删除, err=%v", err)
	}
	if _, err := os.Stat(cacheBundle); !os.IsNotExist(err) {
		t.Fatalf("bundle cache 目录应已删除, err=%v", err)
	}

	updatedConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if len(updatedConfig.EnabledBundles) != 0 {
		t.Fatalf("EnabledBundles 应已清空, got %v", updatedConfig.EnabledBundles)
	}

	var sawFinish bool
	for _, event := range events {
		if event.Scope == "remove.finish" {
			sawFinish = true
			break
		}
	}
	if !sawFinish {
		t.Fatal("应存在 remove.finish 事件")
	}
}

func TestRemoveBundleReturnsNotFound(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/other-workflow/SKILL.md": "---\nname: other-workflow\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs: []string{"cursor"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	_, err := RemoveBundle(RemoveBundleInput{
		ProjectRoot: projectRoot,
		BundleName:  "missing-bundle",
		Confirmed:   true,
	}, nil)
	if err == nil {
		t.Fatal("找不到远端 bundle 时应返回错误")
	}
}
