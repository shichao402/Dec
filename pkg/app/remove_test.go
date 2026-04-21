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

func TestRemoveAssetRejectsWhenUnconfirmed(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	_, err := RemoveAsset(RemoveAssetInput{
		ProjectRoot: t.TempDir(),
		Type:        "skill",
		Name:        "project-workflow",
		Confirmed:   false,
	}, nil)
	if !errors.Is(err, ErrRemoveNotConfirmed) {
		t.Fatalf("未确认时应返回 ErrRemoveNotConfirmed, 实际: %v", err)
	}
}

func TestRemoveAssetRejectsInvalidType(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	_, err := RemoveAsset(RemoveAssetInput{
		ProjectRoot: t.TempDir(),
		Type:        "invalid",
		Name:        "anything",
		Confirmed:   true,
	}, nil)
	if err == nil {
		t.Fatal("非法资产类型应返回错误")
	}
}

func TestRemoveAssetRemovesRemoteAndCleansLocal(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"default/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
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

	// 预置 cache 和 IDE 目录，验证清理路径。
	cacheSkill := filepath.Join(projectRoot, ".dec", "cache", "default", "skills", "project-workflow")
	if err := os.MkdirAll(cacheSkill, 0755); err != nil {
		t.Fatalf("创建 cache 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheSkill, "SKILL.md"), []byte("cache"), 0644); err != nil {
		t.Fatalf("写 cache 失败: %v", err)
	}
	ideSkill := filepath.Join(projectRoot, ".cursor", "skills", "dec-project-workflow")
	if err := os.MkdirAll(ideSkill, 0755); err != nil {
		t.Fatalf("创建 IDE skill 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ideSkill, "SKILL.md"), []byte("installed"), 0644); err != nil {
		t.Fatalf("写 IDE skill 失败: %v", err)
	}

	var events []OperationEvent
	result, err := RemoveAsset(RemoveAssetInput{
		ProjectRoot: projectRoot,
		Type:        "skill",
		Name:        "project-workflow",
		Confirmed:   true,
	}, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("RemoveAsset() 失败: %v", err)
	}

	if result.Vault != "default" {
		t.Fatalf("Vault = %q, 期望 %q", result.Vault, "default")
	}
	if result.VersionCommit == "" {
		t.Fatal("VersionCommit 不应为空")
	}
	if !result.RemovedFromCache {
		t.Fatal("应清理缓存")
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
	if _, err := os.Stat(cacheSkill); !os.IsNotExist(err) {
		t.Fatalf("cache 目录应已删除, err=%v", err)
	}

	updatedConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if updatedConfig.Enabled != nil && updatedConfig.Enabled.FindAsset("skill", "project-workflow", "default") != nil {
		t.Fatal("Enabled 中不应再包含该资产")
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

func TestRemoveAssetReturnsNotFound(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"default/skills/other-workflow/SKILL.md": "---\nname: other-workflow\n---\n",
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

	_, err := RemoveAsset(RemoveAssetInput{
		ProjectRoot: projectRoot,
		Type:        "skill",
		Name:        "missing-asset",
		Confirmed:   true,
	}, nil)
	if err == nil {
		t.Fatal("找不到远端资产时应返回错误")
	}
}
