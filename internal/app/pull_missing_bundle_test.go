package app

import (
	"context"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

// 引用了已从 vault 删除的 bundle 时，pull 只报「请求 0 · 成功 0 · 失败 0」是看不懂的：
// 原因必须落在结构化结果里，而不是只发一条会被后续事件挤掉的 reporter 告警。
func TestPullProjectAssets_ReportsMissingEnabledBundles(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"live/public/project/skills/live-skill/SKILL.md": "---\nname: live-skill\n---\n",
		"live/dec.yaml": "name: live\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		Requires: types.RequiresSpec{"live": types.RequiresVault, "deleted": types.RequiresVault},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.SkippedReason != "未订阅任何项目" {
		t.Fatalf("SkippedReason = %q, vault pin 不再安装私仓项目", result.SkippedReason)
	}
	if result.PulledCount != 0 {
		t.Fatalf("PulledCount = %d, 期望 0", result.PulledCount)
	}
}

// 全部启用的 bundle 都不存在时，除了缺失清单还要给出跳过原因，否则界面只有一排 0。
func TestPullProjectAssets_SkipReasonWhenAllBundlesMissing(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"other/dec.yaml": "name: other\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		Requires: types.RequiresSpec{"vikunja": types.RequiresVault, "default": types.RequiresVault},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if result.SkippedReason != "未订阅任何项目" {
		t.Fatalf("SkippedReason = %q, vault pin 不再安装私仓项目", result.SkippedReason)
	}
}
