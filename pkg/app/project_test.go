package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/secrets"
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
	// Windows 上 os.UserHomeDir 读 USERPROFILE；测试只设 HOME 时内置资产会写到真家里。
	if key == "HOME" {
		setEnvForProjectTest(t, "USERPROFILE", value)
	}
}

func useStubSecretsSession(t *testing.T) {
	t.Helper()
	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)
	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{}}
	}
	t.Cleanup(func() { secretsClientFactory = orig })
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
		"bundles/default/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"codex"},
		Editor:         "code --wait",
		EnabledBundles: []string{"cli"},
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
	if len(loaded.EnabledBundles) != 1 || loaded.EnabledBundles[0] != "cli" {
		t.Fatalf("EnabledBundles = %#v, 期望保留 [cli]", loaded.EnabledBundles)
	}
	if _, err := os.Stat(prepared.ConfigPath); err != nil {
		t.Fatalf("配置文件应已写入: %v", err)
	}
	if _, err := os.Stat(prepared.VarsPath); err != nil {
		t.Fatalf("vars 模板应已写入: %v", err)
	}
}

func TestPrepareProjectConfigInitPreservesEnabledBundlesAndDiscoversBundles(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatalf("写入现有项目配置失败: %v", err)
	}

	prepared, err := PrepareProjectConfigInit(projectRoot, nil)
	if err != nil {
		t.Fatalf("PrepareProjectConfigInit() 失败: %v", err)
	}
	if len(prepared.ProjectConfig.EnabledBundles) != 1 || prepared.ProjectConfig.EnabledBundles[0] != "vikunja" {
		t.Fatalf("EnabledBundles 应保留, got %v", prepared.ProjectConfig.EnabledBundles)
	}
	if prepared.BundleCount < 2 {
		t.Fatalf("BundleCount = %d, 期望至少 2", prepared.BundleCount)
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

func TestInferVaultProjectDoesNotWriteConfig(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"projects/dec-app.yaml": `name: dec-app
bundles:
  - vikunja
  - cli
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := filepath.Join(t.TempDir(), "dec-app")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatalf("创建项目目录失败: %v", err)
	}

	inference, err := InferVaultProject(projectRoot, nil)
	if err != nil {
		t.Fatalf("InferVaultProject() 失败: %v", err)
	}
	if inference == nil {
		t.Fatal("应推断出 vault project")
	}
	if inference.ProjectName != "dec-app" {
		t.Fatalf("ProjectName = %q, 期望 dec-app", inference.ProjectName)
	}
	if len(inference.EnabledBundles) != 2 {
		t.Fatalf("EnabledBundles = %#v, 期望 2 个", inference.EnabledBundles)
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	if mgr.Exists() {
		t.Fatal("推断阶段不应写入 .dec/config.yaml")
	}
}

func TestTryAutoApplyVaultProjectAppliesWhenNoConfig(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"projects/dec-app.yaml": `name: dec-app
bundles:
  - vikunja
  - cli
ides:
  - cursor
editor: code --wait
`,
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
		"bundles/cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
		"bundles/vikunja/bundle.yaml": `name: vikunja
members:
  - skill/vikunja-workflow
`,
		"bundles/cli/bundle.yaml": `name: cli
members:
  - rule/cli-release-rules
`,
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := filepath.Join(t.TempDir(), "dec-app")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatalf("创建项目目录失败: %v", err)
	}

	result, err := TryAutoApplyVaultProject(projectRoot, nil)
	if err != nil {
		t.Fatalf("TryAutoApplyVaultProject() 失败: %v", err)
	}
	if !result.Applied {
		t.Fatal("应自动应用 vault project")
	}
	if result.ProjectName != "dec-app" {
		t.Fatalf("ProjectName = %q, 期望 dec-app", result.ProjectName)
	}
	if len(result.EnabledBundles) != 2 {
		t.Fatalf("EnabledBundles = %#v, 期望 2 个", result.EnabledBundles)
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	if !mgr.Exists() {
		t.Fatal("应写入 .dec/config.yaml")
	}
	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if loaded.ProjectName != "dec-app" {
		t.Fatalf("ProjectName = %q, 期望 dec-app", loaded.ProjectName)
	}
	if len(loaded.EnabledBundles) != 2 {
		t.Fatalf("EnabledBundles = %#v, 期望 2 个", loaded.EnabledBundles)
	}
	if _, err := os.Stat(result.VarsPath); err != nil {
		t.Fatalf("应创建 vars 模板: %v", err)
	}
}

func TestTryAutoApplyVaultProjectSkipsWhenProjectNameSet(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"projects/dec-app.yaml": `name: dec-app
bundles:
  - vikunja
`,
		"bundles/vikunja/skills/vikunja-workflow/SKILL.md": "---\nname: vikunja-workflow\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := filepath.Join(t.TempDir(), "dec-app")
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "dec-app",
		EnabledBundles: []string{"custom"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := TryAutoApplyVaultProject(projectRoot, nil)
	if err != nil {
		t.Fatalf("TryAutoApplyVaultProject() 失败: %v", err)
	}
	if result.Applied {
		t.Fatal("project_name 已设置时不应再次自动应用")
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if len(loaded.EnabledBundles) != 1 || loaded.EnabledBundles[0] != "custom" {
		t.Fatalf("EnabledBundles 不应被覆盖, got %#v", loaded.EnabledBundles)
	}
}

func TestTryAutoApplyVaultProjectSkipsWhenVaultProjectMissing(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"bundles/default/skills/foo/SKILL.md": "---\nname: foo\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	projectRoot := filepath.Join(t.TempDir(), "missing-project")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatalf("创建项目目录失败: %v", err)
	}

	result, err := TryAutoApplyVaultProject(projectRoot, nil)
	if err != nil {
		t.Fatalf("TryAutoApplyVaultProject() 失败: %v", err)
	}
	if result.Applied {
		t.Fatal("vault 无同名 project 时不应写入配置")
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	if mgr.Exists() {
		t.Fatal("不应创建 .dec/config.yaml")
	}
}

func TestNeedsVaultProjectAutoApply(t *testing.T) {
	projectRoot := t.TempDir()
	needs, err := NeedsVaultProjectAutoApply(projectRoot)
	if err != nil {
		t.Fatalf("NeedsVaultProjectAutoApply() 失败: %v", err)
	}
	if !needs {
		t.Fatal("无 config 时应需要自动匹配")
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "my-app"}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}
	needs, err = NeedsVaultProjectAutoApply(projectRoot)
	if err != nil {
		t.Fatalf("NeedsVaultProjectAutoApply() 失败: %v", err)
	}
	if needs {
		t.Fatal("project_name 已设置时不应需要自动匹配")
	}
}

func TestEnsureLocalProjectConfig_CreatesMinimalConfig(t *testing.T) {
	projectRoot := t.TempDir()
	result, err := EnsureLocalProjectConfig(projectRoot, nil)
	if err != nil {
		t.Fatalf("EnsureLocalProjectConfig() = %v", err)
	}
	if result == nil || result.ProjectConfig == nil {
		t.Fatal("应返回 ProjectConfig")
	}
	if result.ProjectConfig.ProjectName == "" {
		t.Fatal("应写入 basename 作为 project_name")
	}
	if !result.VarsCreated {
		t.Fatal("首次应创建 vars 模板")
	}
	result2, err := EnsureLocalProjectConfig(projectRoot, nil)
	if err != nil {
		t.Fatalf("第二次 EnsureLocalProjectConfig() = %v", err)
	}
	if !result2.ExistingConfig {
		t.Fatal("第二次应识别为已存在")
	}
}
