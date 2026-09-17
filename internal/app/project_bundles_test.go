package app

import (
	"context"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

// 项目平面此前直接写盘，于是已从 vault 删除的项目名能一直留在 requires 里。
// 勾选必须挡在配置之外。
func TestSetWorkspaceRequires_ProjectPlaneRejectsUnknownProject(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"live/dec.yaml": "name: live\n",
		"live/public/project/skills/live-skill/SKILL.md": "---\nname: live-skill\n---\n",
		"mine/dec.yaml": "name: mine\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	result, err := SetWorkspaceRequires(
		context.Background(),
		NewWorkspace(WorkspaceProject, projectRoot),
		types.RequiresSpec{
			"live":    types.RequiresVault,
			"deleted": types.RequiresVault,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("SetWorkspaceRequires() 失败: %v", err)
	}
	if len(result.Subscribed) != 1 || result.Subscribed[0].Project != "live" {
		t.Fatalf("Subscribed = %#v, 期望只留 live", result.Subscribed)
	}
	if len(result.Rejected) != 1 || !strings.Contains(result.Rejected[0], "deleted") {
		t.Fatalf("vault 里不存在的项目应被拒: %#v", result.Rejected)
	}

	loaded, err := config.NewProjectConfigManager(projectRoot).LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Requires.VaultProjects()) != 1 || loaded.Requires.VaultProjects()[0] != "live" {
		t.Fatalf("Requires = %#v, 期望 [live]", loaded.Requires.VaultProjects())
	}
}

// 离线时无从校验官方发布状态，不能因此保存不了；私仓 pin 仍要求能扫到私仓。
func TestSetWorkspaceRequires_AllowsOfficialWhenRegistryUnreachable(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	if err := config.NewProjectConfigManager(projectRoot).SaveProjectConfig(&types.ProjectConfig{}); err != nil {
		t.Fatal(err)
	}

	// 仓库未连接：私仓 pin 会因扫仓失败而报错；官方 pin 在 published==nil 时放行。
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
