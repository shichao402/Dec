package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/version"
)

const (
	// checkInterval 两次成功检查之间的最小间隔
	checkInterval = 24 * time.Hour
	// retryInterval 检查失败后的重试间隔，避免离线环境每次启动都发请求
	retryInterval = time.Hour
	// httpTimeout 单个版本来源的请求超时
	httpTimeout = 8 * time.Second
	// maxVersionBody 版本信息响应体读取上限
	maxVersionBody = 1 << 20
	// stateFile 上次检查状态文件名
	stateFile = "update_state.json"
)

// CheckState 记录上次检查状态
type CheckState struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version"`
	// LastAttempt 记录最近一次检查尝试（含失败），用于失败退避
	LastAttempt time.Time `json:"last_attempt,omitempty"`
}

// CheckResult 版本检查结果
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	NeedUpdate     bool
}

// versionSource 一个版本号来源
type versionSource struct {
	name  string
	url   string
	parse func([]byte) (string, error)
}

// versionSources 按优先级返回版本号来源。
// raw.githubusercontent.com 在部分网络环境下会间歇性超时，因此额外提供 CDN 与
// GitHub API 兜底，任一来源成功即可完成检查。
func versionSources() []versionSource {
	cfg := config.GetSystemConfig()
	branch := cfg.UpdateBranch
	if branch == "" {
		branch = "ReleaseLatest"
	}

	return []versionSource{
		{
			name:  "raw.githubusercontent.com",
			url:   cfg.VersionURL,
			parse: parseVersionJSON,
		},
		{
			name:  "cdn.jsdelivr.net",
			url:   fmt.Sprintf("https://cdn.jsdelivr.net/gh/%s/%s@%s/version.json", cfg.RepoOwner, cfg.RepoName, branch),
			parse: parseVersionJSON,
		},
		{
			name:  "api.github.com",
			url:   fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", cfg.RepoOwner, cfg.RepoName),
			parse: parseReleaseTag,
		},
	}
}

func parseVersionJSON(body []byte) (string, error) {
	var info struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("解析版本信息失败: %w", err)
	}
	return info.Version, nil
}

func parseReleaseTag(body []byte) (string, error) {
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("解析版本信息失败: %w", err)
	}
	return release.TagName, nil
}

// fetchLatestVersion 依次尝试各来源获取最新版本号
func fetchLatestVersion() (string, error) {
	return fetchFromSources(versionSources())
}

// fetchFromSources 返回第一个成功来源的版本号，全部失败时汇总各来源的原因
func fetchFromSources(sources []versionSource) (string, error) {
	failures := make([]string, 0, len(sources))

	for _, source := range sources {
		latest, err := fetchVersionFrom(source)
		if err == nil {
			return latest, nil
		}
		failures = append(failures, fmt.Sprintf("  - %s: %s", source.name, err))
	}

	return "", fmt.Errorf("请求版本信息失败，已尝试 %d 个来源:\n%s", len(sources), strings.Join(failures, "\n"))
}

func fetchVersionFrom(source versionSource) (string, error) {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(source.url)
	if err != nil {
		return "", errors.New(describeRequestError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxVersionBody))
	if err != nil {
		return "", fmt.Errorf("读取版本信息失败: %w", err)
	}

	latest, err := source.parse(body)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(latest) == "" {
		return "", errors.New("远程版本号为空")
	}

	return strings.TrimSpace(latest), nil
}

// describeRequestError 去掉 *url.Error 携带的重复 URL，只保留失败原因
func describeRequestError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return fmt.Sprintf("请求超时（超过 %s）", httpTimeout)
		}
		if urlErr.Err != nil {
			return urlErr.Err.Error()
		}
	}
	return err.Error()
}

// Check 检查是否有新版本可用
func Check(currentVersion string) (*CheckResult, error) {
	latest, err := fetchLatestVersion()
	if err != nil {
		recordFailedAttempt()
		return nil, err
	}

	result := &CheckResult{
		CurrentVersion: currentVersion,
		LatestVersion:  latest,
		NeedUpdate:     version.NeedUpdate(currentVersion, latest),
	}

	now := time.Now()
	_ = saveState(&CheckState{
		LastCheck:     now,
		LatestVersion: latest,
		LastAttempt:   now,
	})

	return result, nil
}

// ShouldCheck 判断是否应该刷新远程版本号。
// 成功检查后 24 小时内不再请求；失败后按 retryInterval 退避。
func ShouldCheck() bool {
	state, err := loadState()
	if err != nil {
		return true // 状态文件不存在或损坏，应该检查
	}
	if time.Since(state.LastCheck) < checkInterval {
		return false
	}
	if !state.LastAttempt.IsZero() && time.Since(state.LastAttempt) < retryInterval {
		return false
	}
	return true
}

// CheckBackground 供启动路径使用：只读本地缓存，绝不阻塞在网络请求上。
// 需要刷新时在后台拉取最新版本并落盘，供下次启动使用。
// 返回 nil 表示无需更新。
func CheckBackground(currentVersion string) *CheckResult {
	if ShouldCheck() {
		go refreshStateFn()
	}

	state, err := loadState()
	if err != nil || state.LatestVersion == "" {
		return nil
	}
	if !version.NeedUpdate(currentVersion, state.LatestVersion) {
		return nil
	}

	return &CheckResult{
		CurrentVersion: currentVersion,
		LatestVersion:  state.LatestVersion,
		NeedUpdate:     true,
	}
}

// refreshStateFn 后台刷新入口，测试中可替换
var refreshStateFn = refreshState

// refreshState 静默刷新缓存的最新版本号
func refreshState() {
	latest, err := fetchLatestVersion()
	if err != nil {
		recordFailedAttempt()
		return
	}

	now := time.Now()
	_ = saveState(&CheckState{
		LastCheck:     now,
		LatestVersion: latest,
		LastAttempt:   now,
	})
}

// recordFailedAttempt 记录失败的检查时间，保留已缓存的版本号
func recordFailedAttempt() {
	state, err := loadState()
	if err != nil {
		state = &CheckState{}
	}
	state.LastAttempt = time.Now()
	_ = saveState(state)
}

// DoUpdate 执行自更新：下载指定版本的二进制并替换当前可执行文件。
// latestVersion 由调用方在 Check 时取得，避免重复请求远程版本信息；留空时才重新请求。
func DoUpdate(currentVersion, latestVersion string) error {
	latest := strings.TrimSpace(latestVersion)
	if latest == "" {
		fetched, err := fetchLatestVersion()
		if err != nil {
			return err
		}
		latest = fetched
	}

	if !version.NeedUpdate(currentVersion, latest) {
		return fmt.Errorf("当前已是最新版本 %s", currentVersion)
	}

	// 1. 确定下载 URL
	downloadURL, err := buildDownloadURL(latest)
	if err != nil {
		return err
	}

	// 2. 下载新版本到临时文件
	tmpFile, err := downloadBinary(downloadURL)
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	// 3. 替换当前二进制
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("解析可执行文件路径失败: %w", err)
	}

	if err := replaceBinary(tmpFile, execPath); err != nil {
		return err
	}

	// 4. 更新检查状态
	now := time.Now()
	_ = saveState(&CheckState{
		LastCheck:     now,
		LatestVersion: latest,
		LastAttempt:   now,
	})

	return nil
}

// ManualInstallCommand 返回手动覆盖安装命令
func ManualInstallCommand() string {
	return manualInstallCommand(runtime.GOOS, false)
}

// MirrorInstallCommand 返回走 CDN 镜像的手动覆盖安装命令，
// 用于 raw.githubusercontent.com 不可达的网络环境。
func MirrorInstallCommand() string {
	return manualInstallCommand(runtime.GOOS, true)
}

func manualInstallCommand(goos string, mirror bool) string {
	cfg := config.GetSystemConfig()
	branch := cfg.UpdateBranch
	if branch == "" {
		branch = "ReleaseLatest"
	}

	base := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/scripts", cfg.RepoOwner, cfg.RepoName, branch)
	if mirror {
		base = fmt.Sprintf("https://cdn.jsdelivr.net/gh/%s/%s@%s/scripts", cfg.RepoOwner, cfg.RepoName, branch)
	}

	if goos == "windows" {
		return fmt.Sprintf("iwr -useb %s/install.ps1 | iex", base)
	}
	return fmt.Sprintf("curl -fsSL %s/install.sh | bash", base)
}

// NetworkHelp 返回版本检查/下载失败时的排障建议。
// Go 只识别 HTTP(S)_PROXY 环境变量，不会读取 macOS 系统代理或 PAC 配置，
// 因此开了代理客户端也可能出现直连超时。
func NetworkHelp() string {
	var sb strings.Builder
	sb.WriteString("网络不可达时可以尝试：\n")
	sb.WriteString("  1. 走 CDN 镜像手动覆盖安装：\n")
	sb.WriteString("     " + MirrorInstallCommand() + "\n")
	sb.WriteString("  2. 直连 GitHub 手动覆盖安装：\n")
	sb.WriteString("     " + ManualInstallCommand() + "\n")
	sb.WriteString("  3. 若使用代理客户端，需显式导出环境变量（Dec 不读取系统代理/PAC 设置）：\n")
	sb.WriteString("     export HTTPS_PROXY=http://127.0.0.1:<端口>")
	return sb.String()
}

// buildDownloadURL 根据平台构建下载 URL
func buildDownloadURL(version string) (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}

	binaryName := fmt.Sprintf("dec-%s-%s%s", goos, goarch, ext)
	cfg := config.GetSystemConfig()
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		cfg.RepoOwner, cfg.RepoName, version, binaryName)

	return url, nil
}

// downloadBinary 下载二进制到临时文件
func downloadBinary(url string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "dec-update-*")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}

	return tmpFile.Name(), nil
}

// replaceBinary 替换当前二进制文件
func replaceBinary(newPath, targetPath string) error {
	// 设置可执行权限
	if err := os.Chmod(newPath, 0755); err != nil {
		return fmt.Errorf("设置权限失败: %w", err)
	}

	// 备份旧文件
	backupPath := targetPath + ".bak"
	if err := os.Rename(targetPath, backupPath); err != nil {
		return fmt.Errorf("备份旧版本失败: %w", err)
	}

	// 替换
	if err := copyFilePath(newPath, targetPath); err != nil {
		// 恢复备份
		_ = os.Rename(backupPath, targetPath)
		return fmt.Errorf("替换二进制失败: %w", err)
	}

	// 设置可执行权限
	if err := os.Chmod(targetPath, 0755); err != nil {
		// 恢复备份
		_ = os.Remove(targetPath)
		_ = os.Rename(backupPath, targetPath)
		return fmt.Errorf("设置权限失败: %w", err)
	}

	// macOS: 清除下载的扩展属性（com.apple.provenance 等），避免被系统阻止执行
	if runtime.GOOS == "darwin" {
		_ = exec.Command("xattr", "-cr", targetPath).Run()
	}

	// 清理备份
	_ = os.Remove(backupPath)

	return nil
}

// copyFilePath 复制文件
func copyFilePath(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// ── 状态文件读写 ──

func getStatePath() (string, error) {
	rootDir, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, stateFile), nil
}

func loadState() (*CheckState, error) {
	path, err := getStatePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	state := &CheckState{}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, err
	}

	return state, nil
}

func saveState(state *CheckState) error {
	path, err := getStatePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(data, '\n'), 0644)
}

// FormatUpdateHint 格式化更新提示信息
func FormatUpdateHint(result *CheckResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("新版本可用: %s -> %s\n", result.CurrentVersion, result.LatestVersion))
	sb.WriteString("运行 dec update 更新到最新版本")
	return sb.String()
}
