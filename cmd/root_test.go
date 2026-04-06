package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

func setEnvForRootTest(t *testing.T, key, value string) {
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

func runGitRootTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s 失败: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func runGitNoDirRootTest(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s 失败: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func writeFileRootTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
}

func configureGitUserRootTest(t *testing.T, dir string) {
	t.Helper()
	runGitRootTest(t, dir, "config", "user.name", "Dec Cmd Test")
	runGitRootTest(t, dir, "config", "user.email", "dec-cmd-test@example.com")
}

func setupRemoteBareRepoRootTest(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	remoteBareDir := filepath.Join(root, "remote.git")
	seedDir := filepath.Join(root, "seed")

	runGitNoDirRootTest(t, "init", "--bare", remoteBareDir)
	runGitNoDirRootTest(t, "clone", remoteBareDir, seedDir)
	configureGitUserRootTest(t, seedDir)
	writeFileRootTest(t, filepath.Join(seedDir, "README.md"), "init\n")
	runGitRootTest(t, seedDir, "add", ".")
	runGitRootTest(t, seedDir, "commit", "-m", "initial commit")
	runGitRootTest(t, seedDir, "branch", "-M", "main")
	runGitRootTest(t, seedDir, "push", "-u", "origin", "main")
	runGitNoDirRootTest(t, "--git-dir", remoteBareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	return remoteBareDir
}

func TestVersionCommandRegistered(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"version"})
	if err != nil {
		t.Fatalf("查找 version 命令失败: %v", err)
	}
	if cmd == nil || cmd.Name() != "version" {
		t.Fatalf("期望找到 version 命令")
	}
}

func TestRunVersionPrintsCurrentVersion(t *testing.T) {
	oldVersion := appVersion
	oldBuildTime := appBuildTime
	defer func() {
		appVersion = oldVersion
		appBuildTime = oldBuildTime
		RootCmd.Version = getVersionString()
	}()

	SetVersion("v1.10.40", "2026-04-03_00:00:00")

	var buf bytes.Buffer
	versionCmd.SetOut(&buf)
	versionCmd.SetErr(&buf)

	if err := runVersion(versionCmd, nil); err != nil {
		t.Fatalf("runVersion 返回错误: %v", err)
	}

	if got := buf.String(); got != "v1.10.40\n" {
		t.Fatalf("runVersion 输出 = %q, 期望 %q", got, "v1.10.40\n")
	}
}

func TestGetVersionFallsBackToVersionFileWhenAppVersionIsDev(t *testing.T) {
	tempDir := t.TempDir()
	versionFile := filepath.Join(tempDir, "version.json")
	if err := os.WriteFile(versionFile, []byte("{\n  \"version\": \"v9.9.9\"\n}\n"), 0644); err != nil {
		t.Fatalf("写入 version.json 失败: %v", err)
	}

	oldVersion := appVersion
	oldBuildTime := appBuildTime
	defer func() {
		appVersion = oldVersion
		appBuildTime = oldBuildTime
		RootCmd.Version = getVersionString()
	}()

	appVersion = "dev"
	appBuildTime = "unknown"
	RootCmd.Version = getVersionString()
	chdirForTest(t, tempDir)

	if got := GetVersion(); got != "v9.9.9" {
		t.Fatalf("GetVersion() = %q, 期望 %q", got, "v9.9.9")
	}
}

func TestRunConfigShowRepairsRepoConnectionAndPrintsCurrentRemote(t *testing.T) {
	decHome := t.TempDir()
	setEnvForRootTest(t, "DEC_HOME", decHome)
	remoteA := setupRemoteBareRepoRootTest(t)
	remoteB := setupRemoteBareRepoRootTest(t)

	if err := repo.Connect(remoteA); err != nil {
		t.Fatalf("repo.Connect(remoteA) 失败: %v", err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remoteB}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

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

	if err := runConfigShow(configShowCmd, nil); err != nil {
		t.Fatalf("runConfigShow() 失败: %v", err)
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
	if !strings.Contains(out, "当前远端: "+remoteB) {
		t.Fatalf("config show 应展示修复后的当前远端, 实际输出:\n%s", out)
	}
	if !strings.Contains(out, "连接校验: ✅ 与全局配置一致") {
		t.Fatalf("config show 应展示连接校验通过, 实际输出:\n%s", out)
	}

	bareRemote, err := repo.GetBareRemoteURL()
	if err != nil {
		t.Fatalf("GetBareRemoteURL() 失败: %v", err)
	}
	if bareRemote != remoteB {
		t.Fatalf("runConfigShow 后 bare origin 应被修复为 %q, got %q", remoteB, bareRemote)
	}
}
