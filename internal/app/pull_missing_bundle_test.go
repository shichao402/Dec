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
	// vault pin 只贡献 secrets，不安装私仓项目资产：没有 Git 资产可拉取，但仍走 secrets。
	const want = "没有有效的已启用 Git 资产可拉取（仍尝试同步 secrets）"
	if result.SkippedReason != want {
		t.Fatalf("SkippedReason = %q, 期望 %q", result.SkippedReason, want)
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
	const want = "没有有效的已启用 Git 资产可拉取（仍尝试同步 secrets）"
	if result.SkippedReason != want {
		t.Fatalf("SkippedReason = %q, 期望 %q", result.SkippedReason, want)
	}
}
