package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewReadTransaction_CreatesWorktree(t *testing.T) {
	_, _, _, _ = setupBareRepoFixture(t)

	tx, err := NewReadTransaction()
	if err != nil {
		t.Fatalf("NewReadTransaction 失败: %v", err)
	}
	defer tx.Close()

	if !tx.readOnly {
		t.Fatalf("只读事务应标记 readOnly")
	}
	if tx.tempBranch != "" {
		t.Fatalf("只读事务不应创建临时分支")
	}
	if _, err := os.Stat(filepath.Join(tx.WorkDir(), "README.md")); err != nil {
		t.Fatalf("只读工作区应包含仓库文件: %v", err)
	}
}

func TestNewWriteTransaction_CreatesTempBranchAndCloseCleansUp(t *testing.T) {
	_, bareDir, _, _ := setupBareRepoFixture(t)
	configureBareGitUser(t, bareDir)

	tx, err := NewWriteTransaction()
	if err != nil {
		t.Fatalf("NewWriteTransaction 失败: %v", err)
	}

	if tx.readOnly {
		t.Fatalf("可写事务不应标记 readOnly")
	}
	if tx.tempBranch == "" {
		t.Fatalf("可写事务应创建临时分支")
	}
	branch := runGit(t, tx.WorkDir(), "branch", "--show-current")
	if branch != tx.tempBranch {
		t.Fatalf("当前分支 = %q, want %q", branch, tx.tempBranch)
	}

	workDir := tx.WorkDir()
	tempBranch := tx.tempBranch
	tx.Close()

	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Fatalf("Close 后工作区应被删除")
	}
	listed := runGitNoDir(t, "--git-dir", bareDir, "branch", "--list", tempBranch)
	if strings.TrimSpace(listed) != "" {
		t.Fatalf("Close 后临时分支应被删除, got: %q", listed)
	}
}

func TestTransactionRollback_RemovesWorktree(t *testing.T) {
	_, bareDir, _, _ := setupBareRepoFixture(t)
	configureBareGitUser(t, bareDir)

	tx, err := NewWriteTransaction()
	if err != nil {
		t.Fatalf("NewWriteTransaction 失败: %v", err)
	}

	workDir := tx.WorkDir()
	tempBranch := tx.tempBranch
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback 失败: %v", err)
	}

	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Fatalf("Rollback 后工作区应被删除")
	}
	listed := runGitNoDir(t, "--git-dir", bareDir, "branch", "--list", tempBranch)
	if strings.TrimSpace(listed) != "" {
		t.Fatalf("Rollback 后临时分支应被删除, got: %q", listed)
	}
}

func TestTransactionCommitAndPush_Succeeds(t *testing.T) {
	_, bareDir, _, remoteWorkDir := setupBareRepoFixture(t)
	configureBareGitUser(t, bareDir)

	tx, err := NewWriteTransaction()
	if err != nil {
		t.Fatalf("NewWriteTransaction 失败: %v", err)
	}
	defer tx.Close()

	writeFile(t, filepath.Join(tx.WorkDir(), "feature.txt"), "from transaction\n")
	if err := tx.CommitAndPush("add feature"); err != nil {
		t.Fatalf("CommitAndPush 失败: %v", err)
	}

	runGit(t, remoteWorkDir, "pull", "--ff-only")
	if _, err := os.Stat(filepath.Join(remoteWorkDir, "feature.txt")); err != nil {
		t.Fatalf("远端工作区应看到提交文件: %v", err)
	}

	// 验证 bare repo 的 main 分支已同步到推送后的 commit
	bareHead := runGitNoDir(t, "--git-dir", bareDir, "rev-parse", "refs/heads/main")
	remoteHead := runGit(t, remoteWorkDir, "rev-parse", "HEAD")
	if bareHead != remoteHead {
		t.Fatalf("bare repo main 应与远端同步, bare=%s remote=%s", bareHead, remoteHead)
	}
}

func TestTransactionCommitAndPush_MergesRemoteAdvance(t *testing.T) {
	_, bareDir, _, remoteWorkDir := setupBareRepoFixture(t)
	configureBareGitUser(t, bareDir)

	tx, err := NewWriteTransaction()
	if err != nil {
		t.Fatalf("NewWriteTransaction 失败: %v", err)
	}
	defer tx.Close()

	writeFile(t, filepath.Join(tx.WorkDir(), "local.txt"), "from local\n")
	writeFile(t, filepath.Join(remoteWorkDir, "remote.txt"), "from remote\n")
	runGit(t, remoteWorkDir, "add", ".")
	runGit(t, remoteWorkDir, "commit", "-m", "remote update")
	runGit(t, remoteWorkDir, "push", "origin", "main")

	if err := tx.CommitAndPush("local update"); err != nil {
		t.Fatalf("CommitAndPush 自动合并失败: %v", err)
	}

	runGit(t, remoteWorkDir, "pull", "--ff-only")
	for _, path := range []string{"local.txt", "remote.txt"} {
		if _, err := os.Stat(filepath.Join(remoteWorkDir, path)); err != nil {
			t.Fatalf("自动合并后远端应保留 %s: %v", path, err)
		}
	}
}

func TestTransactionCommitAndPush_FailsOnConflict(t *testing.T) {
	_, bareDir, _, remoteWorkDir := setupBareRepoFixture(t)
	configureBareGitUser(t, bareDir)

	tx, err := NewWriteTransaction()
	if err != nil {
		t.Fatalf("NewWriteTransaction 失败: %v", err)
	}
	defer tx.Close()

	writeFile(t, filepath.Join(tx.WorkDir(), "shared.txt"), "local change\n")
	writeFile(t, filepath.Join(remoteWorkDir, "shared.txt"), "remote change\n")
	runGit(t, remoteWorkDir, "add", ".")
	runGit(t, remoteWorkDir, "commit", "-m", "remote conflict")
	runGit(t, remoteWorkDir, "push", "origin", "main")

	err = tx.CommitAndPush("local conflict")
	if err == nil {
		t.Fatalf("真实冲突时应返回错误")
	}
	if !strings.Contains(err.Error(), "与远端存在冲突") {
		t.Fatalf("错误信息应提示冲突, got: %v", err)
	}
	gitDir, dirErr := NewGitOps(tx.WorkDir()).gitDir()
	if dirErr != nil {
		t.Fatalf("获取事务 git 目录失败: %v", dirErr)
	}
	if _, statErr := os.Stat(filepath.Join(gitDir, "MERGE_HEAD")); !os.IsNotExist(statErr) {
		t.Fatalf("冲突后应自动 abort merge: %v", statErr)
	}
}

func TestTransactionCommitAndPush_ReadOnlyFails(t *testing.T) {
	setupBareRepoFixture(t)

	tx, err := NewReadTransaction()
	if err != nil {
		t.Fatalf("NewReadTransaction 失败: %v", err)
	}
	defer tx.Close()

	err = tx.CommitAndPush("should fail")
	if err == nil {
		t.Fatalf("只读事务提交应失败")
	}
	if !strings.Contains(err.Error(), "只读事务") {
		t.Fatalf("错误信息应提示只读事务, got: %v", err)
	}
}

func configureBareGitUser(t *testing.T, bareDir string) {
	t.Helper()
	runGitNoDir(t, "--git-dir", bareDir, "config", "user.name", "Dec Transaction Test")
	runGitNoDir(t, "--git-dir", bareDir, "config", "user.email", "dec-transaction-test@example.com")
}
