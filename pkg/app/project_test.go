package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

func setEnvForProjectTest(t *testing.T, key, value string) {
	t.Helper()
	oldValue, existed := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("设置环境变量失败: %v", err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, oldValue)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func runGitProjectTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s 失败: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func runGitNoDirProjectTest(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s 失败: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func writeFileProjectTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
}

func configureGitUserProjectTest(t *testing.T, dir string) {
	t.Helper()
	runGitProjectTest(t, dir, "config", "user.name", "Dec App Test")
	runGitProjectTest(t, dir, "config", "user.email", "dec-app-test@example.com")
}

func setupRemoteBareRepoProjectTest(t *testing.T, files map[string]string) string {
	t.Helper()

	// 事务提交会在 bare repo 下 clone 出 worktree 并调 git commit。
	// CI runner 默认没配全局 user.name/email，导致 commit 失败。
	// 通过环境变量兜底身份信息，覆盖整条测试链路（包括未来从 bare 克隆的 worktree）。
	setEnvForProjectTest(t, "GIT_AUTHOR_NAME", "Dec App Test")
	setEnvForProjectTest(t, "GIT_AUTHOR_EMAIL", "dec-app-test@example.com")
	setEnvForProjectTest(t, "GIT_COMMITTER_NAME", "Dec App Test")
	setEnvForProjectTest(t, "GIT_COMMITTER_EMAIL", "dec-app-test@example.com")

	root := t.TempDir()
	remoteBareDir := filepath.Join(root, "remote.git")
	seedDir := filepath.Join(root, "seed")

	runGitNoDirProjectTest(t, "init", "--bare", remoteBareDir)
	runGitNoDirProjectTest(t, "clone", remoteBareDir, seedDir)
	configureGitUserProjectTest(t, seedDir)
	writeFileProjectTest(t, filepath.Join(seedDir, "README.md"), "init\n")
	for path, content := range files {
		writeFileProjectTest(t, filepath.Join(seedDir, path), content)
	}
	runGitProjectTest(t, seedDir, "add", ".")
	runGitProjectTest(t, seedDir, "commit", "-m", "initial commit")
	runGitProjectTest(t, seedDir, "branch", "-M", "main")
	runGitProjectTest(t, seedDir, "push", "-u", "origin", "main")
	runGitNoDirProjectTest(t, "--git-dir", remoteBareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	return remoteBareDir
}

func TestPrepareProjectConfigInitRequiresConnectedRepo(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	_, err := PrepareProjectConfigInit(t.TempDir(), nil)
	if err == nil {
		t.Fatal("未连接仓库时应返回错误")
	}
	if !strings.Contains(err.Error(), "仓库未连接") {
		t.Fatalf("错误信息应提示先连接仓库，实际: %v", err)
	}
}

func TestPrepareProjectConfigInitPreservesExistingConfigAndWritesFiles(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"default/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
		"cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:   []string{"codex"},
		Editor: "code --wait",
		Enabled: &types.AssetList{
			Rules: []types.AssetRef{{Name: "cli-release-rules", Vault: "cli"}},
		},
	}); err != nil {
		t.Fatalf("写入现有项目配置失败: %v", err)
	}

	var events []OperationEvent
	prepared, err := PrepareProjectConfigInit(projectRoot, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("PrepareProjectConfigInit() 失败: %v", err)
	}
	if !prepared.ExistingConfig {
		t.Fatal("已有配置时应标记 ExistingConfig")
	}
	if !prepared.VarsCreated {
		t.Fatal("首次执行应创建 vars 模板")
	}
	if prepared.AssetCount != 2 {
		t.Fatalf("AssetCount = %d, 期望 2", prepared.AssetCount)
	}
	if prepared.ProjectConfig == nil {
		t.Fatal("扫描到资产后应返回项目配置")
	}
	if len(events) == 0 {
		t.Fatal("应向 reporter 发出事件")
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("重新加载项目配置失败: %v", err)
	}
	if loaded.Editor != "code --wait" {
		t.Fatalf("Editor = %q, 期望 %q", loaded.Editor, "code --wait")
	}
	if len(loaded.IDEs) != 1 || loaded.IDEs[0] != "codex" {
		t.Fatalf("IDEs = %#v, 期望保留原值", loaded.IDEs)
	}
	if loaded.Enabled.Count() != 1 {
		t.Fatalf("Enabled.Count() = %d, 期望 1", loaded.Enabled.Count())
	}
	if loaded.Available == nil || loaded.Available.Count() != 2 {
		t.Fatalf("Available.Count() = %d, 期望 2", loaded.Available.Count())
	}
	if loaded.Available.FindAsset("skill", "project-workflow", "default") == nil {
		t.Fatal("available 中缺少 default/project-workflow")
	}
	if loaded.Available.FindAsset("rule", "cli-release-rules", "cli") == nil {
		t.Fatal("available 中缺少 cli/cli-release-rules")
	}
	if _, err := os.Stat(prepared.ConfigPath); err != nil {
		t.Fatalf("配置文件应已写入: %v", err)
	}
	if _, err := os.Stat(prepared.VarsPath); err != nil {
		t.Fatalf("vars 模板应已写入: %v", err)
	}
}

func TestPrepareProjectConfigInitSkipsWriteWhenRepoHasNoAssets(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, nil)
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	prepared, err := PrepareProjectConfigInit(projectRoot, nil)
	if err != nil {
		t.Fatalf("PrepareProjectConfigInit() 失败: %v", err)
	}
	if prepared.AssetCount != 0 {
		t.Fatalf("AssetCount = %d, 期望 0", prepared.AssetCount)
	}
	if prepared.ProjectConfig != nil {
		t.Fatal("无资产时不应创建项目配置对象")
	}
	if prepared.VarsCreated {
		t.Fatal("无资产时不应创建 vars 模板")
	}
	if mgr.Exists() {
		t.Fatal("无资产时不应写入 .dec/config.yaml")
	}
	if _, err := os.Stat(prepared.VarsPath); !os.IsNotExist(err) {
		t.Fatalf("无资产时不应写入 vars 文件: %v", err)
	}
}
