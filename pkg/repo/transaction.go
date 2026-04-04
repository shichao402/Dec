package repo

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Transaction 封装基于 bare repo 的短生命周期工作区
// readOnly=true 表示只读事务，仅用于读取本地工作区文件。
type Transaction struct {
	bareDir     string
	worktreeDir string
	branch      string
	tempBranch  string
	readOnly    bool
	cleaned     bool
}

// NewReadTransaction 创建只读事务。
func NewReadTransaction() (*Transaction, error) {
	return newTransaction(true)
}

// NewReadTransactionAt 创建指定版本的只读事务。
// ref 可以是 commit hash、tag 或 branch 名称。
func NewReadTransactionAt(ref string) (*Transaction, error) {
	tx, err := newTransaction(true)
	if err != nil {
		return nil, err
	}

	git := NewGitOps(tx.worktreeDir)
	if _, err := git.run("checkout", ref); err != nil {
		tx.Close()
		return nil, fmt.Errorf("切换到版本 %s 失败: %w", ref, err)
	}

	return tx, nil
}

// NewWriteTransaction 创建可写事务。
func NewWriteTransaction() (*Transaction, error) {
	return newTransaction(false)
}

func newTransaction(readOnly bool) (*Transaction, error) {
	if err := MigrateToBare(); err != nil {
		return nil, err
	}

	bareDir, err := GetBareRepoDir()
	if err != nil {
		return nil, err
	}
	ok, err := isBareRepo(bareDir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("仓库未连接\n\n运行 dec repo <url> 连接仓库")
	}

	branch, err := GetDefaultBranch()
	if err != nil {
		return nil, err
	}
	if err := FetchBare(); err != nil {
		return nil, err
	}

	worktreeDir, err := newWorktreePath()
	if err != nil {
		return nil, err
	}

	if readOnly {
		if err := addDetachedWorktree(bareDir, worktreeDir, branch); err != nil {
			return nil, err
		}
		return &Transaction{bareDir: bareDir, worktreeDir: worktreeDir, branch: branch, readOnly: true}, nil
	}

	tempBranch, err := randomBranchName("dec-tx")
	if err != nil {
		return nil, err
	}
	if err := addDetachedWorktree(bareDir, worktreeDir, branch); err != nil {
		return nil, err
	}

	git := NewGitOps(worktreeDir)
	if _, err := git.run("switch", "-c", tempBranch); err != nil {
		_ = removeWorktree(bareDir, worktreeDir)
		return nil, fmt.Errorf("创建事务分支失败: %w", err)
	}

	return &Transaction{
		bareDir:     bareDir,
		worktreeDir: worktreeDir,
		branch:      branch,
		tempBranch:  tempBranch,
	}, nil
}

func newWorktreePath() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return "", err
	}
	name, err := randomBranchName("worktree")
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, name), nil
}

func randomBranchName(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(buf)), nil
}

func addDetachedWorktree(bareDir, worktreeDir, startPoint string) error {
	cmd := exec.Command("git", "--git-dir", bareDir, "worktree", "add", "--detach", worktreeDir, startPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add 失败: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func removeWorktree(bareDir, worktreeDir string) error {
	cmd := exec.Command("git", "--git-dir", bareDir, "worktree", "remove", "--force", worktreeDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove 失败: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func isNonFastForwardPushError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "fetch first") ||
		strings.Contains(msg, "[rejected]")
}

// WorkDir 返回事务工作目录
func (t *Transaction) WorkDir() string {
	return t.worktreeDir
}

// CommitHash 返回当前事务工作目录的 HEAD commit hash
func (t *Transaction) CommitHash() string {
	git := NewGitOps(t.worktreeDir)
	hash, err := git.run("rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return hash
}

// Rollback 终止事务并清理临时目录。
func (t *Transaction) Rollback() error {
	return t.cleanup()
}

// Close 关闭事务并清理资源。
func (t *Transaction) Close() {
	_ = t.cleanup()
}

func (t *Transaction) cleanup() error {
	if t == nil || t.cleaned {
		return nil
	}
	t.cleaned = true

	var cleanupErr error
	if err := removeWorktree(t.bareDir, t.worktreeDir); err != nil {
		cleanupErr = err
		_ = os.RemoveAll(t.worktreeDir)
	}

	pruneCmd := exec.Command("git", "--git-dir", t.bareDir, "worktree", "prune")
	_ = pruneCmd.Run()

	if t.tempBranch != "" {
		deleteCmd := exec.Command("git", "--git-dir", t.bareDir, "branch", "-D", t.tempBranch)
		_ = deleteCmd.Run()
	}

	return cleanupErr
}

// CommitAndPush 提交、同步并推送事务中的变更。
func (t *Transaction) CommitAndPush(message string) error {
	if t.readOnly {
		return fmt.Errorf("只读事务不支持提交")
	}
	if t.cleaned {
		return fmt.Errorf("事务已关闭")
	}

	git := NewGitOps(t.worktreeDir)
	clean, err := git.IsClean()
	if err != nil {
		return err
	}
	if clean {
		return nil
	}

	if err := git.Add("."); err != nil {
		return fmt.Errorf("git add 失败: %w", err)
	}
	if err := git.Commit(message); err != nil {
		return fmt.Errorf("git commit 失败: %w", err)
	}
	if _, err := git.run("push", "origin", fmt.Sprintf("HEAD:%s", t.branch)); err == nil {
		t.syncBareRef(git)
		return nil
	} else if !isNonFastForwardPushError(err) {
		return fmt.Errorf("git push 失败: %w", err)
	}

	if err := git.ensureNoSyncInProgress(); err != nil {
		return err
	}
	if _, err := git.run("fetch", "origin", t.branch); err != nil {
		return fmt.Errorf("拉取远端引用失败: %w", err)
	}
	if _, err := git.run("merge", "--no-edit", "FETCH_HEAD"); err != nil {
		_ = git.abortMerge()
		return fmt.Errorf("与远端存在冲突，请稍后重试: %w", err)
	}
	if _, err := git.run("push", "origin", fmt.Sprintf("HEAD:%s", t.branch)); err != nil {
		return fmt.Errorf("git push 失败: %w", err)
	}
	t.syncBareRef(git)
	return nil
}

// syncBareRef 将 worktree 的 HEAD 同步到 bare repo 的目标分支
func (t *Transaction) syncBareRef(git *GitOps) {
	hash, err := git.run("rev-parse", "HEAD")
	if err != nil {
		return
	}
	cmd := exec.Command("git", "--git-dir", t.bareDir, "update-ref", fmt.Sprintf("refs/heads/%s", t.branch), hash)
	_ = cmd.Run()
}
