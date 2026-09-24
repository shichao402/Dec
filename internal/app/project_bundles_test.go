package app

import (
	"context"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

// vault 不再是订阅。连不上注册表时，订阅版本仍可保存。
func TestSetWorkspaceRequires_RejectsVaultPin(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"live/dec.yaml": "name: live\n",
		"live/public/project/skills/live-skill/SKILL.md": "---\nname: live-skill\n---\n",
		"mine/dec.yaml": "name: mine\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	pinRegistryToEmptyRemote(t, remote)

	projectRoot := t.TempDir()
	result, err := SetWorkspaceRequires(
		context.Background(),
		NewWorkspace(WorkspaceProject, projectRoot),
		types.RequiresSpec{
			"live":    types.RequiresLatest,
			"deleted": types.RequiresVault,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("SetWorkspaceRequires() 失败: %v", err)
	}
	if len(result.Subscribed) != 1 || result.Subscribed[0].Project != "live" || result.Subscribed[0].Pin != types.RequiresLatest {
		t.Fatalf("Subscribed = %#v, 期望只留 live@latest", result.Subscribed)
	}
	if len(result.Rejected) != 1 || !strings.Contains(result.Rejected[0], "deleted") {
		t.Fatalf("vault pin 应被拒: %#v", result.Rejected)
	}

	loaded, err := config.NewProjectConfigManager(projectRoot).LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Requires.OfficialProjects()) != 1 || loaded.Requires["live"] != types.RequiresLatest {
		t.Fatalf("Requires = %#v, 期望 live: latest", loaded.Requires)
	}
}

// 离线时无从校验官方发布状态，不能因此保存不了。
func TestSetWorkspaceRequires_AllowsOfficialWhenRegistryUnreachable(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	if err := config.NewProjectConfigManager(projectRoot).SaveProjectConfig(&types.ProjectConfig{}); err != nil {
		t.Fatal(err)
	}

	// 仓库未连接：订阅版本在 published==nil 时放行。
	result, err := SetWorkspaceRequires(
		context.Background(),
		NewWorkspace(WorkspaceProject, projectRoot),
		types.RequiresSpec{"relkit": "latest"},
		nil,
	)
	if err != nil {
		t.Fatalf("SetWorkspaceRequires() 失败: %v", err)
	}
	if len(result.Subscribed) != 1 || result.Subscribed[0].Project != "relkit" {
		t.Fatalf("Subscribed = %#v", result.Subscribed)
	}
}
