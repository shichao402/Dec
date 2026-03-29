// Package repo 管理 Dec 仓库连接
package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GetRepoDir 获取本地仓库克隆目录
func GetRepoDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "repo"), nil
}

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

// IsConnected 检查仓库是否已连接
func IsConnected() (bool, error) {
	repoDir, err := GetRepoDir()
	if err != nil {
		return false, err
	}
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return false, nil
	}
	return true, nil
}

// Connect 连接用户的仓库
func Connect(repoURL string) error {
	repoDir, err := GetRepoDir()
	if err != nil {
		return err
	}

	// 检查是否已连接
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		return fmt.Errorf("仓库已连接: %s\n\n如需重新连接，请先删除 %s", repoDir, repoDir)
	}

	// 克隆仓库
	if err := gitClone(repoURL, repoDir); err != nil {
		return fmt.Errorf("克隆仓库失败: %w", err)
	}

	return nil
}

// GetGit 获取 repo 的 Git 操作实例
func GetGit() (*GitOps, error) {
	repoDir, err := GetRepoDir()
	if err != nil {
		return nil, err
	}
	ok, err := IsConnected()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("仓库未连接\n\n运行 dec repo <url> 连接仓库")
	}
	return NewGitOps(repoDir), nil
}

// Pull 拉取远端最新内容
func Pull() error {
	git, err := GetGit()
	if err != nil {
		return err
	}
	return git.Pull()
}

// CommitAndPush 提交并推送变更
func CommitAndPush(message string) ([]string, error) {
	git, err := GetGit()
	if err != nil {
		return nil, err
	}

	clean, err := git.IsClean()
	if err != nil {
		return nil, err
	}
	if clean {
		return nil, nil // 无变更
	}

	if err := git.Add("."); err != nil {
		return nil, fmt.Errorf("git add 失败: %w", err)
	}
	if err := git.Commit(message); err != nil {
		return nil, fmt.Errorf("git commit 失败: %w", err)
	}

	var warnings []string
	if err := git.Push(); err != nil {
		warnings = append(warnings, fmt.Sprintf("推送到远程仓库失败，已保存到本地: %v", err))
	}

	return warnings, nil
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
	_, err := g.run("pull")
	return err
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
