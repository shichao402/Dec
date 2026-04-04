package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	bareRepoDirName   = "repo.git"
	legacyRepoDirName = "repo"
)

// GetBareRepoDir 获取本地 bare repo 目录
func GetBareRepoDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, bareRepoDirName), nil
}

func getLegacyRepoDir() (string, error) {
	rootDir, err := GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, legacyRepoDirName), nil
}

// IsBareConnected 检查 bare repo 是否已连接
func IsBareConnected() (bool, error) {
	bareDir, err := GetBareRepoDir()
	if err != nil {
		return false, err
	}
	return isBareRepo(bareDir)
}

func isBareRepo(dir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	cmd := exec.Command("git", "--git-dir", dir, "rev-parse", "--is-bare-repository")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git rev-parse --is-bare-repository: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)) == "true", nil
}

// ConnectBare 连接用户仓库到本地 bare repo
func ConnectBare(repoURL string) error {
	bareDir, err := GetBareRepoDir()
	if err != nil {
		return err
	}

	ok, err := isBareRepo(bareDir)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("仓库已连接: %s\n\n如需重新连接，请先删除 %s", bareDir, bareDir)
	}

	if err := os.MkdirAll(filepath.Dir(bareDir), 0755); err != nil {
		return fmt.Errorf("创建仓库目录失败: %w", err)
	}

	if err := gitCloneBare(repoURL, bareDir); err != nil {
		return fmt.Errorf("克隆仓库失败: %w", err)
	}

	return nil
}

// FetchBare 拉取 bare repo 远端引用
func FetchBare() error {
	bareDir, err := GetBareRepoDir()
	if err != nil {
		return err
	}
	ok, err := isBareRepo(bareDir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("仓库未连接\n\n运行 dec repo <url> 连接仓库")
	}

	cmd := exec.Command("git", "--git-dir", bareDir, "fetch", "--prune", "origin", "+refs/heads/*:refs/heads/*")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch --prune origin: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// GetDefaultBranch 获取默认分支名
func GetDefaultBranch() (string, error) {
	bareDir, err := GetBareRepoDir()
	if err != nil {
		return "", err
	}

	cmd := exec.Command("git", "--git-dir", bareDir, "symbolic-ref", "--short", "HEAD")
	output, err := cmd.CombinedOutput()
	if err == nil {
		branch := strings.TrimSpace(string(output))
		if branch != "" {
			return branch, nil
		}
	}

	for _, branch := range []string{"main", "master"} {
		refPath := filepath.Join(bareDir, "refs", "heads", branch)
		if _, statErr := os.Stat(refPath); statErr == nil {
			return branch, nil
		}
	}

	return "", fmt.Errorf("无法确定默认分支")
}

// MigrateToBare 将旧的工作区仓库迁移为 bare repo
func MigrateToBare() error {
	legacyDir, err := getLegacyRepoDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(legacyDir, ".git")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
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
	if ok {
		return nil
	}

	legacyGit := NewGitOps(legacyDir)
	if err := legacyGit.ensureNoSyncInProgress(); err != nil {
		return fmt.Errorf("旧仓库存在未完成的同步状态，请先处理后再迁移: %w", err)
	}
	clean, err := legacyGit.IsClean()
	if err != nil {
		return fmt.Errorf("检查旧仓库状态失败: %w", err)
	}
	if !clean {
		return fmt.Errorf("旧仓库 %s 存在未提交修改，请先处理后再迁移", legacyDir)
	}

	if err := os.MkdirAll(filepath.Dir(bareDir), 0755); err != nil {
		return fmt.Errorf("创建 bare repo 父目录失败: %w", err)
	}
	// 迁移前先读取旧仓库的真正远程 URL
	originURL, err := legacyGit.getRemoteURL("origin")
	if err != nil {
		return fmt.Errorf("读取旧仓库远程 URL 失败: %w", err)
	}

	if err := gitCloneBare(legacyDir, bareDir); err != nil {
		return fmt.Errorf("从旧仓库迁移 bare repo 失败: %w", err)
	}

	// clone --bare 后 origin 指向旧的本地路径，需要恢复为真正的远程 URL
	if originURL != "" {
		if err := setBareRemoteURL(bareDir, "origin", originURL); err != nil {
			return fmt.Errorf("迁移成功，但更新远程 URL 失败: %w", err)
		}
	}

	if err := os.RemoveAll(legacyDir); err != nil {
		return fmt.Errorf("迁移成功，但删除旧仓库失败: %w", err)
	}

	return nil
}

func setBareRemoteURL(bareDir, remote, url string) error {
	cmd := exec.Command("git", "--git-dir", bareDir, "remote", "set-url", remote, url)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git remote set-url 失败: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func gitCloneBare(url, targetDir string) error {
	cmd := exec.Command("git", "clone", "--bare", url, targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone --bare 失败: %s", strings.TrimSpace(string(output)))
	}
	return nil
}
