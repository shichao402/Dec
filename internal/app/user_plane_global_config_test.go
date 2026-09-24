package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

// 用户平面的 projectRoot 为空串，此时项目配置管理器会把 .dec/ 解析成
// 相对服务进程 cwd 的路径。当 cwd 恰好是 DEC_HOME 的父目录时，"项目配置" 与全局配置
// 指向同一个 ~/.dec/config.yaml：任何项目配置读写都会把全局配置改写成项目配置格式，
// repo_url / requires 随之丢失，用户平面 pull 于是报「未订阅」。
func TestUserPlaneOperationsKeepGlobalConfigIntact(t *testing.T) {
	home := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", filepath.Join(home, ".dec"))
	t.Chdir(home)
	useStubSecretsSession(t)

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"cli/dec.yaml": "name: cli\n",
		"cli/public/user/skills/cli-skill/SKILL.md": "---\nname: cli-skill\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{
		RepoURL:  remote,
		IDEs:     []string{"cursor"},
		Requires: types.RequiresSpec{"cli": types.RequiresVault},
	}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	assertGlobalConfigIntact := func(t *testing.T, step string) {
		t.Helper()
		got, err := config.LoadGlobalConfig()
		if err != nil {
			t.Fatalf("%s 后读取全局配置失败: %v", step, err)
		}
		if got.RepoURL != remote {
			t.Fatalf("%s 覆盖了全局配置 repo_url: %q", step, got.RepoURL)
		}
		if len(got.Requires.VaultProjects()) != 1 || got.Requires.VaultProjects()[0] != "cli" {
			t.Fatalf("%s 丢掉了全局配置 requires: %#v", step, got.Requires.VaultProjects())
		}
	}

	workspace := NewWorkspace(WorkspaceUser, "")

	if _, err := LoadWorkspaceOverviewOpts(workspace, OverviewLoadOpts{IncludeVaultBundles: true}); err != nil {
		t.Fatalf("LoadWorkspaceOverviewOpts() 失败: %v", err)
	}
	assertGlobalConfigIntact(t, "加载 overview")

	if _, err := InferVaultProject("", nil); err == nil {
		t.Fatal("空 projectRoot 的 vault project 推断应报错")
	}
	assertGlobalConfigIntact(t, "推断 vault project")

	if _, err := NeedsVaultProjectAutoApply(""); err == nil {
		t.Fatal("空 projectRoot 时不应把 cwd 下的配置当项目配置读")
	}
	assertGlobalConfigIntact(t, "判断是否需要应用 vault project")

	if _, err := LoadWorkspaceAssetSelection(workspace, nil); err != nil {
		t.Fatalf("LoadWorkspaceAssetSelection() 失败: %v", err)
	}
	assertGlobalConfigIntact(t, "加载订阅候选")

	pinRegistryToEmptyRemote(t, remote)
	if _, err := SetWorkspaceRequires(
		context.Background(),
		workspace,
		types.RequiresSpec{"cli": types.RequiresLatest},
		nil,
	); err != nil {
		t.Fatalf("SetWorkspaceRequires() 失败: %v", err)
	}
	saved, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if saved.RepoURL != remote || saved.Requires["cli"] != types.RequiresLatest {
		t.Fatalf("保存订阅后全局配置 = repo %q requires %#v", saved.RepoURL, saved.Requires)
	}

	if _, err := PullWorkspaceAssets(context.Background(), workspace, "", nil); err != nil && !strings.Contains(err.Error(), "注册表") && !strings.Contains(err.Error(), "tag") {
		t.Fatalf("PullWorkspaceAssets() 失败: %v", err)
	}
	after, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if after.RepoURL != remote || after.Requires["cli"] != types.RequiresLatest {
		t.Fatalf("pull 后全局配置 = repo %q requires %#v", after.RepoURL, after.Requires)
	}
}
