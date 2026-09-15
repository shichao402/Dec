package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shichao402/Dec/internal/sysproc"
)

// WithPersistentWorktree 串行打开一个由调用方指定目录的长期工作树。
// 它使用独立分支，避免占用 bare repo 默认分支而阻塞既有短事务的 fetch/update-ref。
func WithPersistentWorktree(worktreeDir, identity string, fn func(worktreeDir, branch, defaultBranch string) error) error {
	bareOpMu.Lock()
	defer bareOpMu.Unlock()

	if err := MigrateToBare(); err != nil {
		return err
	}
	bareDir, err := GetBareRepoDir()
	if err != nil {
		return err
	}
	ok, err := isBareRepo(bareDir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("仓库未连接\n\n请先到 Settings 页配置 Repo URL")
	}
	defaultBranch, err := GetDefaultBranch()
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(identity))
	branch := "dec-sync-" + hex.EncodeToString(sum[:6])

	if err := ensurePersistentWorktree(bareDir, worktreeDir, branch, defaultBranch); err != nil {
		return err
	}
	return fn(worktreeDir, branch, defaultBranch)
}

func ensurePersistentWorktree(bareDir, worktreeDir, branch, startPoint string) error {
	reusable, err := reusePersistentWorktree(bareDir, worktreeDir, branch)
	if err != nil {
		return err
	}
	if reusable {
		return nil
	}
	return createPersistentWorktree(bareDir, worktreeDir, branch, startPoint)
}

// reusePersistentWorktree 判断已有目录能否直接复用。目录不存在、或损坏到只能推倒
// 重来并已被清理时返回 false，由调用方重建。
func reusePersistentWorktree(bareDir, worktreeDir, branch string) (bool, error) {
	if _, err := os.Stat(filepath.Join(worktreeDir, ".git")); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	out, runErr := sysproc.Command("git", "-C", worktreeDir, "rev-parse", "--is-inside-work-tree").CombinedOutput()
	if runErr != nil || strings.TrimSpace(string(out)) != "true" {
		return false, fmt.Errorf("持久同步目录 %s 不是有效 Git worktree", worktreeDir)
	}
	commonOut, commonErr := sysproc.Command("git", "-C", worktreeDir, "rev-parse", "--git-common-dir").CombinedOutput()
	if commonErr != nil {
		return false, fmt.Errorf("读取持久同步 worktree 归属失败: %s", strings.TrimSpace(string(commonOut)))
	}
	common := strings.TrimSpace(string(commonOut))
	if !filepath.IsAbs(common) {
		common = filepath.Join(worktreeDir, common)
	}
	common, _ = filepath.Abs(common)
	expected, _ := filepath.Abs(bareDir)
	if !sameRepoPath(common, expected) {
		return false, fmt.Errorf("持久同步目录 %s 属于其它 Git 仓库", worktreeDir)
	}
	branchOut, branchErr := sysproc.Command("git", "-C", worktreeDir, "branch", "--show-current").CombinedOutput()
	if branchErr != nil || strings.TrimSpace(string(branchOut)) != branch {
		return false, fmt.Errorf("持久同步目录 %s 未处于预期分支 %s", worktreeDir, branch)
	}
	// 分支名对得上但分支还没有任何提交时 HEAD 解析不出来，ahead/behind 与 merge 全部
	// 失败，而上面的分支名校验永远通过，这个状态自己好不了。工作副本里的内容都能从
	// vault 与作者源重建，直接清掉走重建路径。
	if !worktreeHasCommit(worktreeDir) {
		if err := os.RemoveAll(worktreeDir); err != nil {
			return false, fmt.Errorf("清理无提交的持久同步目录 %s 失败: %w", worktreeDir, err)
		}
		return false, nil
	}
	return true, nil
}

func createPersistentWorktree(bareDir, worktreeDir, branch, startPoint string) error {
	if entries, err := os.ReadDir(worktreeDir); err == nil && len(entries) > 0 {
		return fmt.Errorf("持久同步目录 %s 非空且不是 Git worktree", worktreeDir)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return err
	}
	_ = sysproc.Command("git", "--git-dir", bareDir, "worktree", "prune").Run()
	// 上次目录被手工删除时可能只剩分支；确认未被 worktree 使用后重建。
	if out, err := sysproc.Command("git", "--git-dir", bareDir, "branch", "--list", branch).CombinedOutput(); err == nil &&
		strings.TrimSpace(string(out)) != "" {
		if delOut, delErr := sysproc.Command("git", "--git-dir", bareDir, "branch", "-D", branch).CombinedOutput(); delErr != nil {
			return fmt.Errorf("清理旧同步分支失败: %s", strings.TrimSpace(string(delOut)))
		}
	}
	cmd := sysproc.Command("git", "--git-dir", bareDir, "worktree", "add", "-b", branch, worktreeDir, startPoint)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("创建持久同步 worktree 失败: %s", strings.TrimSpace(string(out)))
	}
	// 起点解析异常时 worktree add 可能留下无提交的分支。就地失败，免得损坏状态
	// 活到下一次同步才以 "unknown revision HEAD" 的形式冒出来。
	if !worktreeHasCommit(worktreeDir) {
		return fmt.Errorf("持久同步目录 %s 创建后仍无有效 HEAD（起点 %s）", worktreeDir, startPoint)
	}
	return nil
}

func worktreeHasCommit(worktreeDir string) bool {
	return sysproc.Command("git", "-C", worktreeDir, "rev-parse", "--verify", "--quiet", "HEAD").Run() == nil
}

func sameRepoPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
