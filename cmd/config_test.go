package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

func setHomeForConfigTest(t *testing.T, home string) {
	t.Helper()

	oldHome, hadHome := os.LookupEnv("HOME")
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("设置 HOME 失败: %v", err)
	}
	t.Cleanup(func() {
		if hadHome {
			_ = os.Setenv("HOME", oldHome)
		} else {
			_ = os.Unsetenv("HOME")
		}
	})
}

func TestInstallBuiltinAssetsForCodexInternalUsesInternalUserDir(t *testing.T) {
	homeDir := t.TempDir()
	setHomeForConfigTest(t, homeDir)

	if err := installBuiltinAssetsForIDE("codex-internal"); err != nil {
		t.Fatalf("安装 codex-internal 内置资产失败: %v", err)
	}

	for _, skillName := range []string{"dec", "dec-extract-asset"} {
		internalSkill := filepath.Join(homeDir, ".codex-internal", "skills", skillName, "SKILL.md")
		if _, err := os.Stat(internalSkill); err != nil {
			t.Fatalf("应写入 ~/.codex-internal/skills/%s/SKILL.md: %v", skillName, err)
		}
	}

	wrongSkill := filepath.Join(homeDir, ".codex", "skills", "dec", "SKILL.md")
	if _, err := os.Stat(wrongSkill); !os.IsNotExist(err) {
		t.Fatalf("codex-internal 的用户级内置 skill 不应写到 ~/.codex: %v", err)
	}
}

func TestInstallBuiltinAssetsForClaudeInternalUsesInternalUserDir(t *testing.T) {
	homeDir := t.TempDir()
	setHomeForConfigTest(t, homeDir)

	if err := installBuiltinAssetsForIDE("claude-internal"); err != nil {
		t.Fatalf("安装 claude-internal 内置资产失败: %v", err)
	}

	for _, skillName := range []string{"dec", "dec-extract-asset"} {
		internalSkill := filepath.Join(homeDir, ".claude-internal", "skills", skillName, "SKILL.md")
		if _, err := os.Stat(internalSkill); err != nil {
			t.Fatalf("应写入 ~/.claude-internal/skills/%s/SKILL.md: %v", skillName, err)
		}
	}

	wrongSkill := filepath.Join(homeDir, ".claude", "skills", "dec", "SKILL.md")
	if _, err := os.Stat(wrongSkill); !os.IsNotExist(err) {
		t.Fatalf("claude-internal 的用户级内置 skill 不应写到 ~/.claude: %v", err)
	}
}

func TestInstallBuiltinAssetsReplacesSkillDirectory(t *testing.T) {
	homeDir := t.TempDir()
	setHomeForConfigTest(t, homeDir)

	staleFile := filepath.Join(homeDir, ".cursor", "skills", "dec", "stale.txt")
	if err := os.MkdirAll(filepath.Dir(staleFile), 0755); err != nil {
		t.Fatalf("创建旧目录失败: %v", err)
	}
	if err := os.WriteFile(staleFile, []byte("old"), 0644); err != nil {
		t.Fatalf("写入旧文件失败: %v", err)
	}

	if err := installBuiltinAssetsForIDE("cursor"); err != nil {
		t.Fatalf("安装 cursor 内置资产失败: %v", err)
	}

	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatalf("重装内置 skill 时应清理旧文件: %v", err)
	}
}

func TestRunConfigInitWritesConfigBeforeManualEditFallback(t *testing.T) {
	decHome := t.TempDir()
	setEnvForRootTest(t, "DEC_HOME", decHome)

	remote := setupRemoteBareRepoConfigInitTest(t, map[string]string{
		"default/skills/project-workflow/SKILL.md": "---\nname: project-workflow\n---\n",
		"cli/rules/cli-release-rules.mdc":          "---\ndescription: test\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote, Editor: "__missing_editor__"}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	chdirForTest(t, projectRoot)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建 stdout pipe 失败: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
		_ = r.Close()
	}()

	if err := runConfigInit(configInitCmd, nil); err != nil {
		t.Fatalf("runConfigInit() 失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭写端失败: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("读取输出失败: %v", err)
	}
	_ = r.Close()

	out := buf.String()
	for _, want := range []string{
		"📝 配置已生成:",
		"📝 变量模板已生成:",
		"⚠️  无法打开编辑器:",
		"请手动编辑",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("runConfigInit 输出缺少 %q:\n%s", want, out)
		}
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if loaded.Version != types.ProjectConfigVersionV2 {
		t.Fatalf("version = %q, 期望 %q", loaded.Version, types.ProjectConfigVersionV2)
	}
	if loaded.Available == nil || loaded.Available.Count() != 2 {
		t.Fatalf("Available.Count() = %d, 期望 2", loaded.Available.Count())
	}
	if loaded.Enabled == nil || !loaded.Enabled.IsEmpty() {
		t.Fatalf("Enabled 应为空, got %#v", loaded.Enabled)
	}
	if _, err := os.Stat(mgr.GetVarsPath()); err != nil {
		t.Fatalf("vars 模板应已写入: %v", err)
	}
	if loaded.Available.FindAsset("skill", "project-workflow", "default") == nil {
		t.Fatal("available 中缺少 default/project-workflow")
	}
	if loaded.Available.FindAsset("rule", "cli-release-rules", "cli") == nil {
		t.Fatal("available 中缺少 cli/cli-release-rules")
	}
}

func TestRunConfigRepoUsesAppLayerAndPersistsGlobalConfig(t *testing.T) {
	decHome := t.TempDir()
	setEnvForRootTest(t, "DEC_HOME", decHome)
	remote := setupRemoteBareRepoRootTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建 stdout pipe 失败: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
		_ = r.Close()
	}()

	if err := runConfigRepo(configRepoCmd, []string{remote}); err != nil {
		t.Fatalf("runConfigRepo() 失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭写端失败: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("读取输出失败: %v", err)
	}
	_ = r.Close()

	out := buf.String()
	if !strings.Contains(out, "✅ 仓库已连接:") {
		t.Fatalf("runConfigRepo 输出缺少连接成功提示:\n%s", out)
	}

	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() 失败: %v", err)
	}
	if globalConfig.RepoURL != remote {
		t.Fatalf("repo_url = %q, 期望 %q", globalConfig.RepoURL, remote)
	}
}

func setupRemoteBareRepoConfigInitTest(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	remoteBareDir := filepath.Join(root, "remote.git")
	seedDir := filepath.Join(root, "seed")

	runGitNoDirRootTest(t, "init", "--bare", remoteBareDir)
	runGitNoDirRootTest(t, "clone", remoteBareDir, seedDir)
	configureGitUserRootTest(t, seedDir)
	writeFileRootTest(t, filepath.Join(seedDir, "README.md"), "init\n")
	for path, content := range files {
		writeFileRootTest(t, filepath.Join(seedDir, path), content)
	}
	runGitRootTest(t, seedDir, "add", ".")
	runGitRootTest(t, seedDir, "commit", "-m", "initial commit")
	runGitRootTest(t, seedDir, "branch", "-M", "main")
	runGitRootTest(t, seedDir, "push", "-u", "origin", "main")
	runGitNoDirRootTest(t, "--git-dir", remoteBareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	return remoteBareDir
}

// ========================================
// dec config project CLI
// ========================================

// resetConfigProjectFlags 重置项目级命令用到的 package-level flag 状态，防止测试间互相污染。
func resetConfigProjectFlags(t *testing.T) {
	t.Helper()
	prevIDEs := configProjectIDEs
	prevClear := configProjectClear
	configProjectIDEs = nil
	configProjectClear = false
	t.Cleanup(func() {
		configProjectIDEs = prevIDEs
		configProjectClear = prevClear
	})
}

func captureStdoutForConfigTest(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建 stdout pipe 失败: %v", err)
	}
	os.Stdout = w

	runErr := fn()
	if err := w.Close(); err != nil {
		os.Stdout = oldStdout
		t.Fatalf("关闭写端失败: %v", err)
	}
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("读取输出失败: %v", err)
	}
	_ = r.Close()
	return buf.String(), runErr
}

func TestRunConfigProject_ReadOnlyShowsInheritState(t *testing.T) {
	resetConfigProjectFlags(t)

	decHome := t.TempDir()
	setEnvForRootTest(t, "DEC_HOME", decHome)
	setHomeForConfigTest(t, decHome)

	if err := config.SaveGlobalConfig(&types.GlobalConfig{IDEs: []string{"cursor", "codex"}}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	chdirForTest(t, projectRoot)

	out, err := captureStdoutForConfigTest(t, func() error {
		return runConfigProject(configProjectCmd, nil)
	})
	if err != nil {
		t.Fatalf("runConfigProject() 失败: %v", err)
	}

	for _, want := range []string{
		"项目级 IDE 配置",
		"未覆盖",
		"全局 IDE: cursor, codex",
		"生效 IDE: cursor, codex",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("输出缺少 %q:\n%s", want, out)
		}
	}
}

func TestRunConfigProject_WritesOverride(t *testing.T) {
	resetConfigProjectFlags(t)

	decHome := t.TempDir()
	setEnvForRootTest(t, "DEC_HOME", decHome)
	setHomeForConfigTest(t, decHome)

	if err := config.SaveGlobalConfig(&types.GlobalConfig{IDEs: []string{"cursor"}}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	projectRoot := t.TempDir()
	chdirForTest(t, projectRoot)

	configProjectIDEs = []string{"codex", "codebuddy"}

	out, err := captureStdoutForConfigTest(t, func() error {
		return runConfigProject(configProjectCmd, nil)
	})
	if err != nil {
		t.Fatalf("runConfigProject() 失败: %v", err)
	}

	if !strings.Contains(out, "已保存项目级 IDE 覆盖: codex, codebuddy") {
		t.Fatalf("输出缺少写入成功提示:\n%s", out)
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if len(loaded.IDEs) != 2 || loaded.IDEs[0] != "codex" || loaded.IDEs[1] != "codebuddy" {
		t.Fatalf("loaded.IDEs = %#v, 期望 [codex codebuddy]", loaded.IDEs)
	}
}

func TestRunConfigProject_ClearRemovesOverride(t *testing.T) {
	resetConfigProjectFlags(t)

	decHome := t.TempDir()
	setEnvForRootTest(t, "DEC_HOME", decHome)
	setHomeForConfigTest(t, decHome)

	projectRoot := t.TempDir()
	chdirForTest(t, projectRoot)

	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:   []string{"cursor", "codex"},
		Editor: "vim",
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	configProjectClear = true

	out, err := captureStdoutForConfigTest(t, func() error {
		return runConfigProject(configProjectCmd, nil)
	})
	if err != nil {
		t.Fatalf("runConfigProject(--clear) 失败: %v", err)
	}
	if !strings.Contains(out, "已清除项目级 IDE 覆盖") {
		t.Fatalf("输出缺少清除成功提示:\n%s", out)
	}

	reloaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if len(reloaded.IDEs) != 0 {
		t.Fatalf("reloaded.IDEs = %#v, 期望已清除", reloaded.IDEs)
	}
	if reloaded.Editor != "vim" {
		t.Fatalf("Editor 字段应保留, 得到 %q", reloaded.Editor)
	}

	// YAML 文本级验证：不再包含 ides: 字段
	raw, err := os.ReadFile(filepath.Join(mgr.GetDecDir(), "config.yaml"))
	if err != nil {
		t.Fatalf("读取 config.yaml 失败: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ides:") && !strings.HasPrefix(trimmed, "#") {
			t.Fatalf("config.yaml 不应包含 ides: 字段, 命中行: %q\n完整内容:\n%s", line, string(raw))
		}
	}
}

func TestRunConfigProject_ClearIdempotentWithoutOverride(t *testing.T) {
	resetConfigProjectFlags(t)

	decHome := t.TempDir()
	setEnvForRootTest(t, "DEC_HOME", decHome)
	setHomeForConfigTest(t, decHome)

	projectRoot := t.TempDir()
	chdirForTest(t, projectRoot)

	configProjectClear = true

	out, err := captureStdoutForConfigTest(t, func() error {
		return runConfigProject(configProjectCmd, nil)
	})
	if err != nil {
		t.Fatalf("runConfigProject(--clear) 在无覆盖时应幂等成功, 但报错: %v", err)
	}
	if !strings.Contains(out, "未设置 IDE 覆盖") {
		t.Fatalf("输出缺少幂等提示:\n%s", out)
	}
}

func TestRunConfigProject_RejectsIDEAndClearTogether(t *testing.T) {
	resetConfigProjectFlags(t)

	projectRoot := t.TempDir()
	chdirForTest(t, projectRoot)

	configProjectIDEs = []string{"cursor"}
	configProjectClear = true

	err := runConfigProject(configProjectCmd, nil)
	if err == nil {
		t.Fatal("--ide 与 --clear 同时使用时应返回错误")
	}
	if !strings.Contains(err.Error(), "不能同时使用") {
		t.Fatalf("错误信息应提示不能同时使用, 实际: %v", err)
	}
}
