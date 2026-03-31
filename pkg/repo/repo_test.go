package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitOpsPull_FastForwardsWhenRemoteAdvances(t *testing.T) {
	localDir, remoteWorkDir := setupGitOpsTestRepos(t)

	writeFile(t, filepath.Join(remoteWorkDir, "remote.txt"), "from remote\n")
	runGit(t, remoteWorkDir, "add", ".")
	runGit(t, remoteWorkDir, "commit", "-m", "remote update")
	runGit(t, remoteWorkDir, "push", "origin", "main")

	git := NewGitOps(localDir)
	if err := git.Pull(); err != nil {
		t.Fatalf("Pull 应成功快进: %v", err)
	}

	if _, err := os.Stat(filepath.Join(localDir, "remote.txt")); err != nil {
		t.Fatalf("快进后应看到远端文件: %v", err)
	}

	if counts := strings.Fields(runGit(t, localDir, "rev-list", "--left-right", "--count", "HEAD...origin/main")); len(counts) != 2 || counts[0] != "0" || counts[1] != "0" {
		t.Fatalf("快进后本地与远端应一致，得到: %v", counts)
	}
}

func TestGitOpsPull_KeepsLocalAheadHistory(t *testing.T) {
	localDir, _ := setupGitOpsTestRepos(t)

	writeFile(t, filepath.Join(localDir, "local.txt"), "from local\n")
	runGit(t, localDir, "add", ".")
	runGit(t, localDir, "commit", "-m", "local only")

	git := NewGitOps(localDir)
	if err := git.Pull(); err != nil {
		t.Fatalf("本地仅 ahead 时 Pull 不应失败: %v", err)
	}

	if _, err := os.Stat(filepath.Join(localDir, "local.txt")); err != nil {
		t.Fatalf("ahead 场景应保留本地文件: %v", err)
	}

	if counts := strings.Fields(runGit(t, localDir, "rev-list", "--left-right", "--count", "HEAD...origin/main")); len(counts) != 2 || counts[0] != "1" || counts[1] != "0" {
		t.Fatalf("ahead 场景应保持本地领先，得到: %v", counts)
	}
}

func TestGitOpsPull_AutoMergesDivergedHistory(t *testing.T) {
	localDir, remoteWorkDir := setupGitOpsTestRepos(t)

	writeFile(t, filepath.Join(localDir, "local.txt"), "from local\n")
	runGit(t, localDir, "add", ".")
	runGit(t, localDir, "commit", "-m", "local change")

	writeFile(t, filepath.Join(remoteWorkDir, "remote.txt"), "from remote\n")
	runGit(t, remoteWorkDir, "add", ".")
	runGit(t, remoteWorkDir, "commit", "-m", "remote change")
	runGit(t, remoteWorkDir, "push", "origin", "main")

	git := NewGitOps(localDir)
	if err := git.Pull(); err != nil {
		t.Fatalf("分叉场景应自动 merge: %v", err)
	}

	for _, path := range []string{"local.txt", "remote.txt"} {
		if _, err := os.Stat(filepath.Join(localDir, path)); err != nil {
			t.Fatalf("自动 merge 后应保留 %s: %v", path, err)
		}
	}

	parents := strings.Fields(runGit(t, localDir, "rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 {
		t.Fatalf("分叉自动 merge 后 HEAD 应为 merge commit，得到: %v", parents)
	}
}

func TestGitOpsPull_FailsClearlyWhenMergeInProgress(t *testing.T) {
	localDir, _ := setupGitOpsTestRepos(t)

	mergeHead := filepath.Join(localDir, ".git", "MERGE_HEAD")
	writeFile(t, mergeHead, "dummy\n")

	git := NewGitOps(localDir)
	err := git.Pull()
	if err == nil {
		t.Fatalf("存在未完成 merge 时应返回错误")
	}
	if !strings.Contains(err.Error(), "未完成的 merge") {
		t.Fatalf("错误信息应提示未完成 merge，得到: %v", err)
	}
}

func TestCommitAndPush_AutoMergesRemoteAdvance(t *testing.T) {
	localDir, remoteWorkDir, decHome := setupCommitAndPushTestRepos(t)
	setEnvForTest(t, "DEC_HOME", decHome)

	writeFile(t, filepath.Join(localDir, "local.txt"), "from local\n")
	writeFile(t, filepath.Join(remoteWorkDir, "remote.txt"), "from remote\n")
	runGit(t, remoteWorkDir, "add", ".")
	runGit(t, remoteWorkDir, "commit", "-m", "remote change")
	runGit(t, remoteWorkDir, "push", "origin", "main")

	warnings, err := CommitAndPush("local change")
	if err != nil {
		t.Fatalf("CommitAndPush 应自动合并远端更新: %v", err)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "已自动合并") {
		t.Fatalf("应返回自动合并提示，得到: %v", warnings)
	}

	for _, path := range []string{"local.txt", "remote.txt"} {
		if _, err := os.Stat(filepath.Join(localDir, path)); err != nil {
			t.Fatalf("推送后应保留 %s: %v", path, err)
		}
	}

	if counts := strings.Fields(runGit(t, localDir, "rev-list", "--left-right", "--count", "HEAD...origin/main")); len(counts) != 2 || counts[0] != "0" || counts[1] != "0" {
		t.Fatalf("自动合并并推送后应与远端一致，得到: %v", counts)
	}
}

func TestCommitAndPush_ReturnsClearErrorOnMergeConflict(t *testing.T) {
	localDir, remoteWorkDir, decHome := setupCommitAndPushTestRepos(t)
	setEnvForTest(t, "DEC_HOME", decHome)

	sharedPath := filepath.Join(localDir, "shared.txt")
	writeFile(t, sharedPath, "local change\n")
	writeFile(t, filepath.Join(remoteWorkDir, "shared.txt"), "remote change\n")
	runGit(t, remoteWorkDir, "add", ".")
	runGit(t, remoteWorkDir, "commit", "-m", "remote conflict")
	runGit(t, remoteWorkDir, "push", "origin", "main")

	warnings, err := CommitAndPush("local conflict")
	if err == nil {
		t.Fatalf("真实冲突时应返回错误")
	}
	if warnings != nil {
		t.Fatalf("冲突时不应返回 warning: %v", warnings)
	}
	if !strings.Contains(err.Error(), "自动合并远端更新失败") {
		t.Fatalf("错误信息应说明自动合并失败，得到: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(localDir, ".git", "MERGE_HEAD")); !os.IsNotExist(statErr) {
		t.Fatalf("冲突后应自动 abort merge，MERGE_HEAD 不应存在: %v", statErr)
	}
	if status := runGit(t, localDir, "status", "--porcelain"); status != "" {
		t.Fatalf("自动 abort merge 后工作区应恢复干净，得到: %q", status)
	}
}

func setupGitOpsTestRepos(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	remoteBareDir := filepath.Join(root, "remote.git")
	seedDir := filepath.Join(root, "seed")
	localDir := filepath.Join(root, "local")
	remoteWorkDir := filepath.Join(root, "remote-work")

	runGitNoDir(t, "init", "--bare", remoteBareDir)
	runGitNoDir(t, "clone", remoteBareDir, seedDir)
	configureGitUser(t, seedDir)
	writeFile(t, filepath.Join(seedDir, "README.md"), "init\n")
	runGit(t, seedDir, "add", ".")
	runGit(t, seedDir, "commit", "-m", "initial commit")
	runGit(t, seedDir, "branch", "-M", "main")
	runGit(t, seedDir, "push", "-u", "origin", "main")
	runGitNoDir(t, "--git-dir", remoteBareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	runGitNoDir(t, "clone", remoteBareDir, localDir)
	configureGitUser(t, localDir)
	runGit(t, localDir, "config", "pull.rebase", "true")

	runGitNoDir(t, "clone", remoteBareDir, remoteWorkDir)
	configureGitUser(t, remoteWorkDir)

	return localDir, remoteWorkDir
}

func setupCommitAndPushTestRepos(t *testing.T) (string, string, string) {
	t.Helper()

	decHome := t.TempDir()
	root := t.TempDir()
	remoteBareDir := filepath.Join(root, "remote.git")
	seedDir := filepath.Join(root, "seed")
	localDir := filepath.Join(decHome, "repo")
	remoteWorkDir := filepath.Join(root, "remote-work")

	runGitNoDir(t, "init", "--bare", remoteBareDir)
	runGitNoDir(t, "clone", remoteBareDir, seedDir)
	configureGitUser(t, seedDir)
	writeFile(t, filepath.Join(seedDir, "shared.txt"), "base\n")
	runGit(t, seedDir, "add", ".")
	runGit(t, seedDir, "commit", "-m", "initial commit")
	runGit(t, seedDir, "branch", "-M", "main")
	runGit(t, seedDir, "push", "-u", "origin", "main")
	runGitNoDir(t, "--git-dir", remoteBareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	runGitNoDir(t, "clone", remoteBareDir, localDir)
	configureGitUser(t, localDir)
	runGit(t, localDir, "config", "pull.rebase", "true")

	runGitNoDir(t, "clone", remoteBareDir, remoteWorkDir)
	configureGitUser(t, remoteWorkDir)

	return localDir, remoteWorkDir, decHome
}

func configureGitUser(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "config", "user.name", "Dec Test")
	runGit(t, dir, "config", "user.email", "dec-test@example.com")
}

func setEnvForTest(t *testing.T, key, value string) {
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s 失败: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func runGitNoDir(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s 失败: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}
