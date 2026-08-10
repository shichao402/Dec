package repo

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/pkg/diag"
)

// bareOpMu 串行化 bare 上的 fetch / worktree 增删，避免 TUI 并联 refresh 在 Windows 上互卡。
var bareOpMu sync.Mutex

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

// NewReadTransaction 创建只读事务（会先 FetchBare）。
func NewReadTransaction() (*Transaction, error) {
	return newTransaction(true, true)
}

// NewLocalReadTransaction 创建只读事务但不 fetch，用于 TUI 概览/列表等可接受略旧 refs 的场景。
func NewLocalReadTransaction() (*Transaction, error) {
	return newTransaction(true, false)
}

// NewReadTransactionAt 创建指定版本的只读事务。
// ref 可以是 commit hash、tag 或 branch 名称。
func NewReadTransactionAt(ref string) (*Transaction, error) {
	tx, err := newTransaction(true, true)
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
	return newTransaction(false, true)
}

func newTransaction(readOnly, fetch bool) (*Transaction, error) {
	label := fmt.Sprintf("bareTX readOnly=%v fetch=%v", readOnly, fetch)
	diag.StartupLog("bareOpMu waiting… (%s)", label)
	waitStart := time.Now()
	bareOpMu.Lock()
	diag.StartupLog("bareOpMu acquired after %dms (%s)", time.Since(waitStart).Milliseconds(), label)
	defer func() {
		bareOpMu.Unlock()
		diag.StartupLog("bareOpMu released (%s)", label)
	}()

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
		return nil, fmt.Errorf("仓库未连接\n\n请先到 Settings 页配置 Repo URL")
	}

	if fetch {
		diag.StartupLog("FetchBare starting")
		if err := FetchBare(); err != nil {
			diag.StartupLog("FetchBare failed: %v", err)
			return nil, err
		}
		diag.StartupLog("FetchBare done")
	}
	branch, err := GetDefaultBranch()
	if err != nil {
		return nil, err
	}

	worktreeDir, err := newWorktreePath()
	if err != nil {
		return nil, err
	}

	if readOnly {
		diag.StartupLog("addDetachedWorktree starting branch=%s", branch)
		if err := addDetachedWorktree(bareDir, worktreeDir, branch); err != nil {
			diag.StartupLog("addDetachedWorktree failed: %v", err)
			return nil, err
		}
		diag.StartupLog("addDetachedWorktree done")
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

func isNothingToCommitError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "nothing to commit")
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
	bareOpMu.Lock()
	defer bareOpMu.Unlock()
	if t.cleaned {
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

// CommitAndPush 提交并推送；若工作区或暂存区最终无实质变更则 committed=false。
func (t *Transaction) CommitAndPush(message string) (committed bool, err error) {
	if t.readOnly {
		return false, fmt.Errorf("只读事务不支持提交")
	}
	if t.cleaned {
		return false, fmt.Errorf("事务已关闭")
	}

	git := NewGitOps(t.worktreeDir)
	clean, err := git.IsClean()
	if err != nil {
		return false, err
	}
	if clean {
		return false, nil
	}

	if err := git.Add("."); err != nil {
		return false, fmt.Errorf("git add 失败: %w", err)
	}
	// status 可能因换行等判脏，add 后用 cached diff 判定是否真有可提交内容。
	if has, derr := git.HasCachedDiff(); derr != nil {
		return false, derr
	} else if !has {
		return false, nil
	}
	if err := git.Commit(message); err != nil {
		if isNothingToCommitError(err) {
			return false, nil
		}
		return false, fmt.Errorf("git commit 失败: %w", err)
	}
	if _, err := git.run("push", "origin", fmt.Sprintf("HEAD:%s", t.branch)); err == nil {
		t.syncBareRef(git)
		return true, nil
	} else if !isNonFastForwardPushError(err) {
		return false, fmt.Errorf("git push 失败: %w", err)
	}

	if err := git.ensureNoSyncInProgress(); err != nil {
		return false, err
	}
	if _, err := git.run("fetch", "origin", t.branch); err != nil {
		return false, fmt.Errorf("拉取远端引用失败: %w", err)
	}
	if _, err := git.run("merge", "--no-edit", "FETCH_HEAD"); err != nil {
		_ = git.abortMerge()
		return false, fmt.Errorf("与远端存在冲突，请稍后重试: %w", err)
	}
	if _, err := git.run("push", "origin", fmt.Sprintf("HEAD:%s", t.branch)); err != nil {
		return false, fmt.Errorf("git push 失败: %w", err)
	}
	t.syncBareRef(git)
	return true, nil
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
