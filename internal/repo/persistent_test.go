package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 分支名对得上、但分支没有任何提交的 worktree 会让 ahead/behind 与 merge 全部失败，
// 且分支名校验永远通过。复用检查必须识别它并重建，否则该项目的同步永久卡死。
func TestEnsurePersistentWorktreeRebuildsBranchWithoutCommits(t *testing.T) {
	_, bareDir, _, _ := setupBareRepoFixture(t)
	worktreeDir := filepath.Join(t.TempDir(), "sync", "vault")
	branch := "dec-sync-test"

	// 复现现场：worktree 建好后停在一个同名但没有任何提交的分支上，
	// index 里还 stage 着整个 vault。
	runGitNoDir(t, "--git-dir", bareDir, "worktree", "add", "--no-checkout", "-b", branch+"-seed", worktreeDir, "main")
	runGit(t, worktreeDir, "checkout", "--orphan", branch)
	if worktreeHasCommit(worktreeDir) {
		t.Fatal("fixture 应构造出没有提交的分支")
	}
	if got := runGit(t, worktreeDir, "branch", "--show-current"); got != branch {
		t.Fatalf("fixture 分支 = %q, want %q", got, branch)
	}

	if err := ensurePersistentWorktree(bareDir, worktreeDir, branch, "main"); err != nil {
		t.Fatalf("ensurePersistentWorktree 应重建损坏的 worktree: %v", err)
	}

	if !worktreeHasCommit(worktreeDir) {
		t.Fatal("重建后 HEAD 应能解析出提交")
	}
	if got := runGit(t, worktreeDir, "branch", "--show-current"); got != branch {
		t.Fatalf("重建后分支 = %q, want %q", got, branch)
	}
	head := runGit(t, worktreeDir, "rev-parse", "HEAD")
	mainHead := runGitNoDir(t, "--git-dir", bareDir, "rev-parse", "main")
	if head != mainHead {
		t.Fatalf("重建后应基于起点 main，HEAD = %q, main = %q", head, mainHead)
	}
}

func TestEnsurePersistentWorktreeReusesHealthyWorktree(t *testing.T) {
	_, bareDir, _, _ := setupBareRepoFixture(t)
	worktreeDir := filepath.Join(t.TempDir(), "sync", "vault")
	branch := "dec-sync-test"

	if err := ensurePersistentWorktree(bareDir, worktreeDir, branch, "main"); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	// 复用而非重建的可观察证据：未跟踪文件不会被清掉。
	marker := filepath.Join(worktreeDir, "local-only.txt")
	if err := os.WriteFile(marker, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensurePersistentWorktree(bareDir, worktreeDir, branch, "main"); err != nil {
		t.Fatalf("复用健康 worktree 失败: %v", err)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("健康 worktree 应被原样复用: %v", err)
	}
}

func TestEnsurePersistentWorktreeRejectsUnexpectedBranch(t *testing.T) {
	_, bareDir, _, _ := setupBareRepoFixture(t)
	worktreeDir := filepath.Join(t.TempDir(), "sync", "vault")

	if err := ensurePersistentWorktree(bareDir, worktreeDir, "dec-sync-test", "main"); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}

	err := ensurePersistentWorktree(bareDir, worktreeDir, "dec-sync-other", "main")
	if err == nil {
		t.Fatal("分支不符应报错，而不是静默重建别人的目录")
	}
	if !strings.Contains(err.Error(), "未处于预期分支") {
		t.Fatalf("错误信息应指出分支不符, got: %v", err)
	}
}
