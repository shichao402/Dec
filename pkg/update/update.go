package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/version"
)

const (
	// checkInterval 两次自动检查之间的最小间隔
	checkInterval = 24 * time.Hour
	// httpTimeout HTTP 请求超时
	httpTimeout = 10 * time.Second
	// stateFile 上次检查状态文件名
	stateFile = "update_state.json"
)

// CheckState 记录上次检查状态
type CheckState struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version"`
}

// CheckResult 版本检查结果
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	NeedUpdate     bool
}

// fetchLatestVersion 从远程获取最新版本号
func fetchLatestVersion() (string, error) {
	url := config.GetVersionURL()

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("请求版本信息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("获取版本信息失败: HTTP %d", resp.StatusCode)
	}

	var info struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("解析版本信息失败: %w", err)
	}

	if info.Version == "" {
		return "", fmt.Errorf("远程版本号为空")
	}

	return info.Version, nil
}

// Check 检查是否有新版本可用
func Check(currentVersion string) (*CheckResult, error) {
	latest, err := fetchLatestVersion()
	if err != nil {
		return nil, err
	}

	result := &CheckResult{
		CurrentVersion: currentVersion,
		LatestVersion:  latest,
		NeedUpdate:     version.NeedUpdate(currentVersion, latest),
	}

	// 保存检查状态
	_ = saveState(&CheckState{
		LastCheck:     time.Now(),
		LatestVersion: latest,
	})

	return result, nil
}

// ShouldCheck 判断是否应该执行自动检查（距上次检查超过 24 小时）
func ShouldCheck() bool {
	state, err := loadState()
	if err != nil {
		return true // 状态文件不存在或损坏，应该检查
	}
	return time.Since(state.LastCheck) >= checkInterval
}

// CheckBackground 用于启动时后台检查，静默失败
// 返回 nil 表示无需更新或检查失败
func CheckBackground(currentVersion string) *CheckResult {
	if !ShouldCheck() {
		// 即使跳过网络请求，也检查上次缓存的结果
		state, err := loadState()
		if err == nil && state.LatestVersion != "" {
			if version.NeedUpdate(currentVersion, state.LatestVersion) {
				return &CheckResult{
					CurrentVersion: currentVersion,
					LatestVersion:  state.LatestVersion,
					NeedUpdate:     true,
				}
			}
		}
		return nil
	}

	result, err := Check(currentVersion)
	if err != nil {
		return nil
	}
	if !result.NeedUpdate {
		return nil
	}
	return result
}

// DoUpdate 执行自更新：下载最新二进制并替换当前可执行文件
func DoUpdate(currentVersion string) error {
	// 1. 检查最新版本
	latest, err := fetchLatestVersion()
	if err != nil {
		return err
	}

	if !version.NeedUpdate(currentVersion, latest) {
		return fmt.Errorf("当前已是最新版本 %s", currentVersion)
	}

	// 2. 确定下载 URL
	downloadURL, err := buildDownloadURL(latest)
	if err != nil {
		return err
	}

	// 3. 下载新版本到临时文件
	tmpFile, err := downloadBinary(downloadURL)
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	// 4. 替换当前二进制
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

	// 5. 更新检查状态
	_ = saveState(&CheckState{
		LastCheck:     time.Now(),
		LatestVersion: latest,
	})

	return nil
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
	rootDir, err := paths.GetRootDir()
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
