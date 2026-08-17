package app

import (
	"context"
	"strings"
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
		"bundles/live/skills/live-skill/SKILL.md": "---\nname: live-skill\n---\n",
		"bundles/live/bundle.yaml":                "name: live\nscope: project\nmembers:\n  - skills/live-skill\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Demo",
		EnabledBundles: []string{"live", "deleted"},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if len(result.MissingBundles) != 1 || result.MissingBundles[0] != "deleted" {
		t.Fatalf("MissingBundles = %#v, 期望 [deleted]", result.MissingBundles)
	}
	joined := strings.Join(result.NonFatalWarnings, " | ")
	if !strings.Contains(joined, "deleted") {
		t.Fatalf("NonFatalWarnings 应点名缺失的 bundle: %s", joined)
	}
	if result.PulledCount != 1 {
		t.Fatalf("PulledCount = %d, 仍在的 live 应被拉取", result.PulledCount)
	}
}

// 全部启用的 bundle 都不存在时，除了缺失清单还要给出跳过原因，否则界面只有一排 0。
func TestPullProjectAssets_SkipReasonWhenAllBundlesMissing(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	useStubSecretsSession(t)
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/other/bundle.yaml": "name: other\nscope: project\nmembers: []\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Demo",
		EnabledBundles: []string{"vikunja", "default"},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("PullProjectAssets() 失败: %v", err)
	}
	if len(result.MissingBundles) != 2 {
		t.Fatalf("MissingBundles = %#v, 期望两个都缺失", result.MissingBundles)
	}
	if strings.TrimSpace(result.SkippedReason) == "" {
		t.Fatal("没有资产可拉时必须给出跳过原因")
	}
}
