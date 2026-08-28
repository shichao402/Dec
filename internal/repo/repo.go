// Package repo 管理 Dec 仓库连接
package repo

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shichao402/Dec/internal/sysproc"
)

// authRequiredMarker 让「需要重新认证」这一语义能跨进程存活。
//
// 门面（TUI）与 dec-server 之间的操作失败只回传错误文本，Go 错误类型在 RPC 边界就丢了。
// pull 失败后门面必须能区分「仓库凭证过期」与其他失败（尤其 Bitwarden 401），才敢提示走
// GCM bootstrap，所以在错误文本里带一个稳定标记；展示前由 StripAuthMarker 去掉。
const authRequiredMarker = "[dec:repo-auth-required]"

// AuthenticationError 表示 HTTPS 仓库操作明确失败于凭证阶段。
// 上层可据此进入 Bitwarden → GCM bootstrap 确认流程；网络/DNS/仓库地址错误不得误触发。
type AuthenticationError struct {
	Host string
	Err  error
}

func (e *AuthenticationError) Error() string {
	if e == nil {
		return "仓库认证失败"
	}
	return fmt.Sprintf("%s 仓库 %s 认证失败: %v", authRequiredMarker, e.Host, e.Err)
}

func (e *AuthenticationError) Unwrap() error { return e.Err }

func IsAuthenticationError(err error) bool {
	var target *AuthenticationError
	return errors.As(err, &target)
}

// MessageIndicatesAuthRequired 判断一段（可能跨进程传回的）错误文本是否源自仓库认证失败。
func MessageIndicatesAuthRequired(message string) bool {
	return strings.Contains(message, authRequiredMarker)
}

// StripAuthMarker 去掉展示给用户时无意义的机器标记。
func StripAuthMarker(message string) string {
	return strings.TrimSpace(strings.ReplaceAll(message, authRequiredMarker, ""))
}

// classifyRemoteAuthError 把 git 远端操作失败按凭证原因分类。
// repoURL 决定是否 HTTPS 与 host；非 HTTPS 或非凭证类失败原样返回，避免误导用户走 bootstrap。
func classifyRemoteAuthError(repoURL, message string, err error) error {
	host, hostErr := RepoHost(repoURL)
	if hostErr != nil || !isHTTPSRepoURL(repoURL) || !looksLikeAuthenticationFailure(message) {
		return err
	}
	return &AuthenticationError{Host: host, Err: err}
}

// RepoHost 返回仓库 URL 的主机名。支持 https://host/... 与 git@host:path。
func RepoHost(repoURL string) (string, error) {
	raw := strings.TrimSpace(repoURL)
	if raw == "" {
		return "", fmt.Errorf("仓库地址为空")
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Hostname() != "" {
		return strings.ToLower(parsed.Hostname()), nil
	}
	if at := strings.LastIndex(raw, "@"); at >= 0 {
		raw = raw[at+1:]
	}
	if colon := strings.Index(raw, ":"); colon > 0 {
		return strings.ToLower(strings.TrimSpace(raw[:colon])), nil
	}
	return "", fmt.Errorf("无法从仓库地址解析主机: %q", repoURL)
}

// Probe 在不修改本地 repo 的前提下验证远端可访问性。
// 禁止终端/GCM 交互，认证失败交由 TUI 显式确认是否走 Bitwarden bootstrap。
func Probe(repoURL string) error {
	repoURL = strings.TrimSpace(repoURL)
	cmd := sysproc.Command("git", "ls-remote", "--heads", repoURL)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never")
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		message = err.Error()
	}
	return classifyRemoteAuthError(repoURL, message, fmt.Errorf("git ls-remote: %s", message))
}

func isHTTPSRepoURL(repoURL string) bool {
	lower := strings.ToLower(strings.TrimSpace(repoURL))
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

func looksLikeAuthenticationFailure(message string) bool {
	lower := strings.ToLower(message)
	for _, marker := range []string{
		"authentication failed",
		"credentials have expired",
		"credential has expired",
		"could not read username",
		"terminal prompts disabled",
		"access denied",
		"authorization failed",
		"unauthorized",
		"http 401",
		"error: 401",
		"http 403",
		"error: 403",
		"repository not found",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizeRepoURL(url string) string {
	trimmed := strings.TrimSpace(url)
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	trimmed = strings.TrimPrefix(trimmed, "git@github.com:")
	trimmed = strings.TrimPrefix(trimmed, "ssh://git@github.com/")
	trimmed = strings.TrimPrefix(trimmed, "https://github.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
	trimmed = strings.TrimPrefix(trimmed, "git://github.com/")
	return trimmed
}

func RepoURLsEquivalent(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	return normalizeRepoURL(a) == normalizeRepoURL(b)
}

// EnsureConnectedRepoMatches 检查本地 bare repo 的 remote 是否与期望 URL 一致；不一致时自动修复。
func EnsureConnectedRepoMatches(expectedURL string) error {
	trimmed := strings.TrimSpace(expectedURL)
	if trimmed == "" {
		return nil
	}

	if err := MigrateToBare(); err != nil {
		return err
	}

	connected, err := IsBareConnected()
	if err != nil {
		return err
	}
	if !connected {
		return nil
	}

	currentURL, err := GetBareRemoteURL()
	if err != nil {
		return err
	}
	if RepoURLsEquivalent(currentURL, trimmed) {
		return nil
	}

	return ConnectBare(trimmed)
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
	cmd := sysproc.Command("git", args...)
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
			return errors.New(marker.message)
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

// StatusEntry 是 git status --porcelain 的一行。
type StatusEntry struct {
	Code string
	Path string
}

// IsClean 检查工作区是否干净。
func (g *GitOps) IsClean() (bool, error) {
	entries, err := g.Status()
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func (g *GitOps) Status() ([]StatusEntry, error) {
	output, err := g.run("status", "--porcelain")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}
	var out []StatusEntry
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		out = append(out, StatusEntry{Code: strings.TrimSpace(line[:2]), Path: strings.TrimSpace(line[3:])})
	}
	return out, nil
}

// HasCachedDiff 检查暂存区是否相对 HEAD 有实质差异。
func (g *GitOps) HasCachedDiff() (bool, error) {
	cmd := sysproc.Command("git", "-C", g.workDir, "diff", "--cached", "--quiet")
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}

// ========================================
// 本地 git 命令封装（用于 internal/repo 内部）
// ========================================

func gitClone(url, targetDir string) error {
	cmd := sysproc.Command("git", "clone", url, targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone 失败: %s", strings.TrimSpace(string(output)))
	}
	return nil
}
