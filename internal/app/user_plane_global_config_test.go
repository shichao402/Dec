package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

// 用户平面的 projectRoot 为空串（`dec --user`），此时项目配置管理器会把 .dec/ 解析成
// 相对服务进程 cwd 的路径。当 cwd 恰好是 DEC_HOME 的父目录时，"项目配置" 与全局配置
// 指向同一个 ~/.dec/config.yaml：任何项目配置读写都会把全局配置改写成项目配置格式，
// repo_url / enabled_bundles 随之丢失，用户平面 pull 于是报 "未启用 bundle"。
func TestUserPlaneOperationsKeepGlobalConfigIntact(t *testing.T) {
	home := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", filepath.Join(home, ".dec"))
	t.Chdir(home)
	useStubSecretsSession(t)

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/cli/skills/cli-skill/SKILL.md": "---\nname: cli-skill\n---\n",
		"bundles/cli/bundle.yaml":               "name: cli\nscope: user\nmembers:\n  - skill/cli-skill\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{
		RepoURL:        remote,
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"cli"},
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
		if len(got.EnabledBundles) != 1 || got.EnabledBundles[0] != "cli" {
			t.Fatalf("%s 丢掉了全局配置 enabled_bundles: %#v", step, got.EnabledBundles)
		}
	}

	workspace := NewWorkspace(WorkspaceUser, "")

	if _, err := LoadWorkspaceOverviewOpts(workspace, OverviewLoadOpts{IncludeVaultBundles: true}); err != nil {
		t.Fatalf("LoadWorkspaceOverviewOpts() 失败: %v", err)
	}
	assertGlobalConfigIntact(t, "加载 overview")

	// overview 之后 TUI 曾无条件推断 vault project；空 projectRoot 会让它读写 cwd 下的
	// .dec/config.yaml，也就是全局配置本身。
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
	assertGlobalConfigIntact(t, "加载 bundle 选择")

	if _, err := SaveWorkspaceEnabledBundles(workspace, []string{"cli"}, nil); err != nil {
		t.Fatalf("SaveWorkspaceEnabledBundles() 失败: %v", err)
	}
	assertGlobalConfigIntact(t, "保存 bundle 选择")

	result, err := PullWorkspaceAssets(context.Background(), workspace, "", nil)
	if err != nil {
		t.Fatalf("PullWorkspaceAssets() 失败: %v", err)
	}
	if result.SkippedReason != "" {
		t.Fatalf("已启用 cli 时不应跳过 pull: %q", result.SkippedReason)
	}
	assertGlobalConfigIntact(t, "执行 pull")
}
