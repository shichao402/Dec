package vault

import (
	"fmt"
	"os/exec"
	"strings"
)

// GitOps 封装 vault 目录的 Git 操作
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

// Clone 克隆远程仓库到 workDir
func (g *GitOps) Clone(url string) error {
	cmd := exec.Command("git", "clone", url, g.workDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone 失败: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// Init 在 workDir 中初始化 Git 仓库
func (g *GitOps) Init() error {
	_, err := g.run("init")
	return err
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

// HasRemote 检查是否配置了远程仓库
func (g *GitOps) HasRemote() (bool, error) {
	output, err := g.run("remote", "-v")
	if err != nil {
		return false, nil
	}
	return output != "", nil
}

// SetRemote 设置远程仓库地址
func (g *GitOps) SetRemote(url string) error {
	_, err := g.run("remote", "add", "origin", url)
	return err
}

// CreateGitHubRepo 通过 gh CLI 创建 GitHub 仓库，返回仓库 URL
func CreateGitHubRepo(name string) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("未找到 gh CLI，请先安装: https://cli.github.com")
	}

	cmd := exec.Command("gh", "repo", "create", name, "--private")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("创建 GitHub 仓库失败: %s", strings.TrimSpace(string(output)))
	}

	// gh repo create 输出仓库 URL
	repoURL := strings.TrimSpace(string(output))
	if repoURL == "" {
		// 如果没有直接输出 URL，通过 gh 查询
		cmd = exec.Command("gh", "repo", "view", name, "--json", "url", "-q", ".url")
		urlOutput, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("https://github.com/%s", name), nil
		}
		repoURL = strings.TrimSpace(string(urlOutput))
	}

	return repoURL, nil
}
