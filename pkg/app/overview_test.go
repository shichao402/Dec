package app

import (
	"reflect"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

func TestLoadProjectOverviewWithExistingProjectConfig(t *testing.T) {
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
		IDEs:   []string{"codex"},
		Editor: "code --wait",
		Available: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
			Rules:  []types.AssetRef{{Name: "cli-release-rules", Vault: "cli"}},
		},
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Name: "project-workflow", Vault: "default"}},
		},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}
	if _, err := mgr.EnsureVarsConfigTemplate(); err != nil {
		t.Fatalf("EnsureVarsConfigTemplate() 失败: %v", err)
	}

	overview, err := LoadProjectOverview(projectRoot)
	if err != nil {
		t.Fatalf("LoadProjectOverview() 失败: %v", err)
	}
	if !overview.RepoConnected {
		t.Fatal("应识别已连接仓库")
	}
	if overview.RepoRemoteURL != remote {
		t.Fatalf("RepoRemoteURL = %q, 期望 %q", overview.RepoRemoteURL, remote)
	}
	if !overview.ProjectConfigReady {
		t.Fatal("应识别项目配置已存在")
	}
	if !overview.VarsFileReady {
		t.Fatal("应识别 vars 模板已存在")
	}
	if overview.AvailableCount != 2 {
		t.Fatalf("AvailableCount = %d, 期望 2", overview.AvailableCount)
	}
	if overview.EnabledCount != 1 {
		t.Fatalf("EnabledCount = %d, 期望 1", overview.EnabledCount)
	}
	if !reflect.DeepEqual(overview.IDEs, []string{"codex"}) {
		t.Fatalf("IDEs = %#v, 期望 %#v", overview.IDEs, []string{"codex"})
	}
	if overview.Editor != "code --wait" {
		t.Fatalf("Editor = %q, 期望 %q", overview.Editor, "code --wait")
	}
}

func TestLoadProjectOverviewFallsBackToDefaultsWithoutProjectConfig(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	overview, err := LoadProjectOverview(t.TempDir())
	if err != nil {
		t.Fatalf("LoadProjectOverview() 失败: %v", err)
	}
	if overview.RepoConnected {
		t.Fatal("未连接仓库时不应标记为已连接")
	}
	if overview.ProjectConfigReady {
		t.Fatal("未初始化项目时不应标记项目配置已存在")
	}
	if overview.VarsFileReady {
		t.Fatal("未初始化项目时不应标记 vars 文件已存在")
	}
	if overview.AvailableCount != 0 || overview.EnabledCount != 0 {
		t.Fatalf("未初始化项目时资产计数应为 0, got available=%d enabled=%d", overview.AvailableCount, overview.EnabledCount)
	}
	if !reflect.DeepEqual(overview.IDEs, []string{"cursor"}) {
		t.Fatalf("默认 IDE = %#v, 期望 %#v", overview.IDEs, []string{"cursor"})
	}
	if overview.Editor == "" {
		t.Fatal("默认 editor 不应为空")
	}
	if len(overview.IDEWarnings) != 0 {
		t.Fatalf("无项目配置时不应产生 IDE 警告, got %#v", overview.IDEWarnings)
	}
}
