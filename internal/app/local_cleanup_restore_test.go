package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

// TestLightCleanupThenPullRestoresAssets 覆盖「轻量清理后一次拉取即恢复」的闭环：
// 不要求重新填私仓地址、重新配置凭据、重新导入项目或重新勾选资产。
func TestLightCleanupThenPullRestoresAssets(t *testing.T) {
	home := t.TempDir()
	decHome := filepath.Join(home, ".dec")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DEC_HOME", decHome)
	useStubSecretsSession(t)

	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"default/public/project/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	projectRoot := filepath.Join(home, "work")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := config.RegisterManagedProject(projectRoot, "work"); err != nil {
		t.Fatalf("RegisterManagedProject() 失败: %v", err)
	}
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:        []string{"cursor"},
		ProjectName: "default",
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	// 先拉一次，得到清理前的基线。
	first, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("首次拉取失败: %v", err)
	}
	if first.PulledCount == 0 {
		t.Fatalf("首次拉取应安装资产: %+v", first)
	}
	installedSkill := filepath.Join(projectRoot, ".cursor", "skills", "dec-project-workflow", "SKILL.md")
	if _, err := os.Stat(installedSkill); err != nil {
		t.Fatalf("安装后的 skill 应存在: %v", err)
	}

	// 轻量清理：资产产物消失，但配置、凭据与项目登记都在。
	result, err := CleanupLocalInstallation(context.Background(), LocalCleanupInput{
		Confirmed: true, Tier: string(CleanupTierLight),
	}, nil)
	if err != nil {
		t.Fatalf("轻量清理失败: %v", err)
	}
	if len(result.Deleted) == 0 {
		t.Fatalf("轻量清理应删除资产产物: %+v", result)
	}
	if _, err := os.Stat(installedSkill); !os.IsNotExist(err) {
		t.Fatalf("轻量清理应删除已安装 skill: %v", err)
	}
	globalCfgPath, err := config.GetGlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(globalCfgPath); err != nil {
		t.Fatalf("轻量清理应保留全局配置 %s: %v", globalCfgPath, err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".dec", "config.yaml")); err != nil {
		t.Fatalf("轻量清理应保留项目配置与 requires: %v", err)
	}
	if _, err := os.Stat(filepath.Join(decHome, "repo.git")); !os.IsNotExist(err) {
		t.Fatalf("轻量清理应清掉本地资产仓库，等待拉取时重建: %v", err)
	}

	// 清理后不做任何重新配置，直接拉一次。
	second, err := PullProjectAssets(context.Background(), projectRoot, "", nil)
	if err != nil {
		t.Fatalf("轻量清理后拉取失败: %v", err)
	}
	if second.PulledCount != first.PulledCount {
		t.Fatalf("拉取应恢复到清理前的资产集合: 清理前 %d，清理后 %d", first.PulledCount, second.PulledCount)
	}
	if _, err := os.Stat(installedSkill); err != nil {
		t.Fatalf("拉取后资产应恢复可用: %v", err)
	}
	if _, err := os.Stat(filepath.Join(decHome, "repo.git", "HEAD")); err != nil {
		t.Fatalf("拉取应重建本地资产仓库: %v", err)
	}
	// 私仓地址未被要求重新填写。
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("清理后全局配置应仍可读: %v", err)
	}
	if strings.TrimSpace(cfg.RepoURL) == "" {
		t.Fatalf("轻量清理后私仓地址应仍然存在: %#v", cfg.RepoURL)
	}
	projects, err := config.ListManagedProjects()
	if err != nil {
		t.Fatalf("清理后受管项目应仍可读: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("轻量清理后项目登记应保留: %#v", projects)
	}
}
