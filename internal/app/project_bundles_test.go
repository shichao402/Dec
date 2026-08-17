package app

import (
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

// 项目平面此前直接写盘，于是已从 vault 删除的 bundle 名能一直留在 enabled_bundles 里，
// pull 每次只报一句「找不到声明，已忽略」。勾选必须挡在配置之外。
func TestSaveWorkspaceEnabledBundles_ProjectPlaneRejectsUnknownBundle(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/live/skills/live-skill/SKILL.md": "---\nname: live-skill\n---\n",
		"bundles/live/bundle.yaml":                "name: live\nscope: project\nmembers:\n  - skills/live-skill\n",
		"bundles/mine/bundle.yaml":                "name: mine\nscope: user\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	result, err := SaveWorkspaceEnabledBundles(
		NewWorkspace(WorkspaceProject, projectRoot),
		[]string{"live", "mine", "deleted"}, nil)
	if err != nil {
		t.Fatalf("SaveWorkspaceEnabledBundles() 失败: %v", err)
	}
	if result.EnabledBundleCount != 1 {
		t.Fatalf("EnabledBundleCount = %d, 期望只留 live", result.EnabledBundleCount)
	}
	if len(result.RejectedBundles) != 2 {
		t.Fatalf("RejectedBundles = %#v, 期望 mine 与 deleted 各一条", result.RejectedBundles)
	}
	joined := strings.Join(result.RejectedBundles, " | ")
	if !strings.Contains(joined, "mine") || !strings.Contains(joined, "scope: user") {
		t.Fatalf("scope: user 的包应说明属于用户平面: %s", joined)
	}
	if !strings.Contains(joined, "deleted") {
		t.Fatalf("vault 里不存在的包应被拒: %s", joined)
	}

	loaded, err := config.NewProjectConfigManager(projectRoot).LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.EnabledBundles) != 1 || loaded.EnabledBundles[0] != "live" {
		t.Fatalf("enabled_bundles = %#v, 期望 [live]", loaded.EnabledBundles)
	}
}

// 离线时无从校验，不能因此保存不了。
func TestValidateProjectEnabledBundles_AllowsWhenRepoDisconnected(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	rejected, err := validateProjectEnabledBundles([]string{"anything"}, nil)
	if err != nil {
		t.Fatalf("validateProjectEnabledBundles() 失败: %v", err)
	}
	if len(rejected) != 0 {
		t.Fatalf("仓库未连接时不应拒绝: %#v", rejected)
	}
}

// 隐式 bundle（目录有资产但没 manifest）在项目平面同样算可见。
func TestValidateProjectEnabledBundles_AcceptsSynthesizedBundle(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/implicit/skills/implicit-skill/SKILL.md": "---\nname: implicit-skill\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote}); err != nil {
		t.Fatal(err)
	}

	rejected, err := validateProjectEnabledBundles([]string{"implicit"}, nil)
	if err != nil {
		t.Fatalf("validateProjectEnabledBundles() 失败: %v", err)
	}
	if len(rejected) != 0 {
		t.Fatalf("隐式 bundle 不应被拒: %#v", rejected)
	}
}
