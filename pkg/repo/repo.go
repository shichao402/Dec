// Package repo 管理 Dec 仓库连接
package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// GetRootDir 获取 Dec 根目录 (~/.dec/)
func GetRootDir() (string, error) {
	if rootDir := os.Getenv("DEC_HOME"); rootDir != "" {
		return filepath.Abs(rootDir)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".dec"), nil
}

// IsConnected 检查 bare repo 是否已连接。
func IsConnected() (bool, error) {
	if err := MigrateToBare(); err != nil {
		return false, err
	}
	return IsBareConnected()
}

// Connect 连接用户的仓库。
func Connect(repoURL string) error {
	if err := MigrateToBare(); err != nil {
		return err
	}
	return ConnectBare(repoURL)
}

// GetGit 已废弃。bare repo 模式下请使用事务接口。
func GetGit() (*GitOps, error) {
	return nil, fmt.Errorf("bare repo 模式下不支持直接获取工作区，请使用事务接口")
}

// Pull 拉取远端最新内容。
func Pull() error {
	if err := MigrateToBare(); err != nil {
		return err
	}
	return FetchBare()
}

// CommitAndPush 已废弃。bare repo 模式下请使用事务接口。
func CommitAndPush(message string) ([]string, error) {
	return nil, fmt.Errorf("bare repo 模式下不支持直接提交，请使用事务接口")
}

// ========================================
// Git 操作
// ========================================

// GitOps 封装仓库目录的 Git 操作
type GitOps struct {
	workDir string
}

// NewGitOps 创建 Git 操作实例
func NewGitOps(workDir string) *GitOps {
	return &GitOps{workDir: workDir}
}

func (g *GitOps) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// Add 添加文件到暂存区
func (g *GitOps) Add(paths ...string) error {
	args := append([]string{"add"}, paths...)
	_, err := g.run(args...)
	return err
}

// Commit 提交暂存区的更改
func (g *GitOps) Commit(message string) error {
	_, err := g.run("commit", "-m", message)
	return err
}

// Push 推送到远程仓库
func (g *GitOps) Push() error {
	_, err := g.run("push")
	return err
}

// Pull 从远程仓库拉取
func (g *GitOps) Pull() error {
	if err := g.ensureNoSyncInProgress(); err != nil {
		return err
	}

	branch, err := g.currentBranch()
	if err != nil {
		return err
	}
	remoteRef := "origin/" + branch

	if err := g.fetchBranch(branch); err != nil {
		return err
	}

	ahead, behind, err := g.aheadBehind(remoteRef)
	if err != nil {
		return err
	}

	switch {
	case behind == 0:
		return nil
	case ahead == 0:
		_, err = g.run("merge", "--ff-only", remoteRef)
		return err
	default:
		_, err = g.run("merge", "--no-edit", remoteRef)
		if err != nil {
			return fmt.Errorf("自动合并远端更新失败: %w", err)
		}
		return nil
	}
}

func (g *GitOps) syncForWrite() ([]string, error) {
	if err := g.ensureNoSyncInProgress(); err != nil {
		return nil, err
	}

	branch, err := g.currentBranch()
	if err != nil {
		return nil, err
	}
	remoteRef := "origin/" + branch

	if err := g.fetchBranch(branch); err != nil {
		return nil, err
	}

	ahead, behind, err := g.aheadBehind(remoteRef)
	if err != nil {
		return nil, err
	}
	if behind == 0 {
		return nil, nil
	}
	if ahead == 0 {
		return nil, fmt.Errorf("远端已有新提交，请先同步后重试")
	}

	if _, err := g.run("merge", "--no-edit", remoteRef); err != nil {
		abortErr := g.abortMerge()
		if abortErr != nil {
			return nil, fmt.Errorf("自动合并远端更新失败: %v；回滚失败: %w", err, abortErr)
		}
		return nil, fmt.Errorf("自动合并远端更新失败，请处理 ~/.dec/repo.git 中的冲突后重试: %w", err)
	}

	return []string{"检测到远端已有更新，已自动合并到本地 Vault 仓库"}, nil
}

func (g *GitOps) abortMerge() error {
	_, err := g.run("merge", "--abort")
	if err != nil {
		return fmt.Errorf("git merge --abort 失败: %w", err)
	}
	return nil
}

func (g *GitOps) currentBranch() (string, error) {
	branch, err := g.run("branch", "--show-current")
	if err != nil {
		return "", err
	}
	if branch == "" {
		return "", fmt.Errorf("当前仓库不在分支上，无法同步")
	}
	return branch, nil
}

func (g *GitOps) fetchBranch(branch string) error {
	_, err := g.run("fetch", "--prune", "origin", branch)
	if err != nil {
		return fmt.Errorf("拉取远端引用失败: %w", err)
	}
	return nil
}

func (g *GitOps) aheadBehind(remoteRef string) (int, int, error) {
	output, err := g.run("rev-list", "--left-right", "--count", fmt.Sprintf("HEAD...%s", remoteRef))
	if err != nil {
		return 0, 0, fmt.Errorf("检查本地与远端分叉状态失败: %w", err)
	}

	parts := strings.Fields(output)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("无法解析分叉状态: %s", output)
	}

	ahead, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("解析本地提交数失败: %w", err)
	}
	behind, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("解析远端提交数失败: %w", err)
	}

	return ahead, behind, nil
}

func (g *GitOps) gitDir() (string, error) {
	gitDir, err := g.run("rev-parse", "--git-dir")
	if err != nil {
		return "", fmt.Errorf("获取 git 目录失败: %w", err)
	}
	if filepath.IsAbs(gitDir) {
		return gitDir, nil
	}
	return filepath.Join(g.workDir, gitDir), nil
}

func (g *GitOps) ensureNoSyncInProgress() error {
	gitDir, err := g.gitDir()
	if err != nil {
		return err
	}
	markers := []struct {
		path    string
		message string
	}{
		{filepath.Join(gitDir, "MERGE_HEAD"), "仓库中存在未完成的 merge，请先处理同步冲突后重试"},
		{filepath.Join(gitDir, "rebase-merge"), "仓库中存在未完成的 rebase，请先处理同步冲突后重试"},
		{filepath.Join(gitDir, "rebase-apply"), "仓库中存在未完成的 rebase，请先处理同步冲突后重试"},
	}

	for _, marker := range markers {
		if _, err := os.Stat(marker.path); err == nil {
			return fmt.Errorf(marker.message)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("检查仓库同步状态失败: %w", err)
		}
	}

	return nil
}

// getRemoteURL 获取指定 remote 的 URL
func (g *GitOps) getRemoteURL(remote string) (string, error) {
	url, err := g.run("config", "--get", fmt.Sprintf("remote.%s.url", remote))
	if err != nil {
		return "", err
	}
	return url, nil
}

// IsClean 检查工作区是否干净
func (g *GitOps) IsClean() (bool, error) {
	output, err := g.run("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return output == "", nil
}

// ========================================
// 本地 git 命令封装（用于 pkg/repo 内部）
// ========================================

func gitClone(url, targetDir string) error {
	cmd := exec.Command("git", "clone", url, targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone 失败: %s", strings.TrimSpace(string(output)))
	}
	return nil
}
