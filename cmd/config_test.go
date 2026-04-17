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
