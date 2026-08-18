package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/version"
	"cnb.cool/shichao402/relkit/sdk"
	"cnb.cool/shichao402/relkit/sdk/apply"
)

const (
	checkInterval = 24 * time.Hour
	retryInterval = time.Hour
	stateFile     = "update_state.json"
	productName   = "dec"
	channelName   = "dev"
	keyID         = "dec-2026"
)

// CheckState records last check status (legacy fields kept for TUI compatibility).
type CheckState struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version"`
	LastAttempt   time.Time `json:"last_attempt,omitempty"`
}

// CheckResult is the version check result shape used by CLI/TUI.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	NeedUpdate     bool
}

func entryURLs() []string {
	return []string{
		"https://updates.firoyang.com/rup/directory/dec.pb",
	}
}

func stateDir() (string, error) {
	root, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return root, nil
}

func newUpdater(currentVersion, component string) (*sdk.Updater, error) {
	keys, err := trustedKeys()
	if err != nil {
		return nil, err
	}
	code, err := sdk.SemverCode(currentVersion)
	if err != nil {
		// dev / unknown → treat as code 0 so any release is newer
		code = 0
	}
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	selectors := map[string]string{
		"os":        runtime.GOOS,
		"arch":      runtime.GOARCH,
		"component": component,
	}
	cfg := config.GetSystemConfig()
	channel := cfg.Channel
	if channel == "" {
		channel = channelName
	}
	return &sdk.Updater{
		Product:         productName,
		Channel:         channel,
		CurrentCode:     code,
		EntryURLs:       entryURLs(),
		TrustedKeys:     keys,
		ClientSelectors: selectors,
		StateStore:      sdk.NewFileStateStore(dir, productName, channel),
		Policy:          sdk.DefaultPolicy(),
	}, nil
}

// Check checks whether a newer version is available via RUP.
func Check(currentVersion string) (*CheckResult, error) {
	u, err := newUpdater(currentVersion, "dec")
	if err != nil {
		recordFailedAttempt()
		return nil, err
	}
	result := u.CheckForce(context.Background(), true)
	if result.Err != nil {
		recordFailedAttempt()
		return nil, result.Err
	}
	latest := currentVersion
	need := false
	if result.Available != nil && result.Available.Target != nil {
		latest = result.Available.Target.Version
		if !strings.HasPrefix(latest, "v") {
			latest = "v" + latest
		}
		need = version.NeedUpdate(currentVersion, latest)
	}
	out := &CheckResult{
		CurrentVersion: currentVersion,
		LatestVersion:  latest,
		NeedUpdate:     need,
	}
	now := time.Now()
	_ = saveState(&CheckState{LastCheck: now, LatestVersion: latest, LastAttempt: now})
	return out, nil
}

// ShouldCheck reports whether a remote refresh is due.
func ShouldCheck() bool {
	state, err := loadState()
	if err != nil {
		return true
	}
	if time.Since(state.LastCheck) < checkInterval {
		return false
	}
	if !state.LastAttempt.IsZero() && time.Since(state.LastAttempt) < retryInterval {
		return false
	}
	return true
}

// CheckBackground is non-blocking: reads local cache and may refresh in background.
func CheckBackground(currentVersion string) *CheckResult {
	if ShouldCheck() {
		go refreshStateFn(currentVersion)
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

var refreshStateFn = func(currentVersion string) {
	_, _ = Check(currentVersion)
}

func recordFailedAttempt() {
	state, err := loadState()
	if err != nil {
		state = &CheckState{}
	}
	state.LastAttempt = time.Now()
	_ = saveState(state)
}

// DoUpdate downloads and replaces the current binary via RUP + sdk/apply.
func DoUpdate(currentVersion, latestVersion string) error {
	latest := strings.TrimSpace(latestVersion)
	u, err := newUpdater(currentVersion, "dec")
	if err != nil {
		return err
	}
	result := u.CheckForce(context.Background(), true)
	if result.Err != nil {
		return result.Err
	}
	if result.Available == nil {
		return fmt.Errorf("当前已是最新版本 %s", currentVersion)
	}
	targetVer := result.Available.Target.Version
	if !strings.HasPrefix(targetVer, "v") {
		targetVer = "v" + targetVer
	}
	if latest != "" && !strings.EqualFold(strings.TrimPrefix(latest, "v"), strings.TrimPrefix(targetVer, "v")) {
		// Prefer the version Check already resolved; mismatch is informational only.
	}
	if !version.NeedUpdate(currentVersion, targetVer) {
		return fmt.Errorf("当前已是最新版本 %s", currentVersion)
	}

	tmpDir, err := os.MkdirTemp("", "dec-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	type componentDownload struct {
		name string
		path string
	}
	components := []string{"dec-server", "dec-mcp", "dec-exec", "dec"}
	downloads := make([]componentDownload, 0, len(components))
	for _, component := range components {
		componentUpdater, err := newUpdater(currentVersion, component)
		if err != nil {
			return err
		}
		componentResult := componentUpdater.CheckForce(context.Background(), true)
		if componentResult.Err != nil {
			return componentResult.Err
		}
		if componentResult.Available == nil {
			return fmt.Errorf("发布缺少 %s 组件", component)
		}
		dest := filepath.Join(tmpDir, component+ext)
		if err := componentUpdater.Download(context.Background(), componentResult.Available, dest); err != nil {
			return fmt.Errorf("下载 %s 失败: %w", component, err)
		}
		downloads = append(downloads, componentDownload{name: component, path: dest})
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	binDir := filepath.Dir(executable)
	for _, download := range downloads {
		target := filepath.Join(binDir, download.name+ext)
		if _, err := os.Stat(target); os.IsNotExist(err) {
			if err := os.WriteFile(target, nil, 0o755); err != nil {
				return fmt.Errorf("创建 %s 安装位失败: %w", download.name, err)
			}
		}
		if err := apply.ReplaceFile(download.path, target); err != nil {
			return fmt.Errorf("替换 %s 失败: %w", download.name, err)
		}
	}
	now := time.Now()
	_ = saveState(&CheckState{LastCheck: now, LatestVersion: targetVer, LastAttempt: now})
	return nil
}

// ManualInstallCommand returns the primary first-install command (CNB raw scripts).
// This is for fresh installs / docs — not an update-failure escape hatch.
func ManualInstallCommand() string {
	return manualInstallCommand(runtime.GOOS, false)
}

// MirrorInstallCommand returns an optional GitHub mirror install command.
// Prefer ManualInstallCommand; GitHub is a documentation backup, not required for self-update.
func MirrorInstallCommand() string {
	return manualInstallCommand(runtime.GOOS, true)
}

func manualInstallCommand(goos string, githubMirror bool) string {
	cfg := config.GetSystemConfig()
	branch := cfg.UpdateBranch
	if branch == "" {
		branch = "ReleaseLatest"
	}
	owner := cfg.RepoOwner
	if owner == "" {
		owner = "shichao402"
	}
	name := cfg.RepoName
	if name == "" {
		name = "Dec"
	}
	var base string
	if githubMirror {
		base = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/scripts", owner, name, branch)
	} else {
		base = fmt.Sprintf("https://cnb.cool/%s/%s/-/git/raw/%s/scripts", owner, name, branch)
	}
	if goos == "windows" {
		return fmt.Sprintf("iwr -useb %s/install.ps1 | iex", base)
	}
	return fmt.Sprintf("curl -fsSL %s/install.sh | bash", base)
}

// NetworkHelp returns self-update troubleshooting hints (RUP/COS only).
// Update failure is not an invitation to reinstall via GitHub/CNB install scripts.
func NetworkHelp() string {
	var sb strings.Builder
	sb.WriteString("自更新检查/下载只走 https://updates.firoyang.com/ ，与首次安装无关。\n")
	sb.WriteString("网络不可达时可以尝试：\n")
	sb.WriteString("  1. 确认本机可访问 https://updates.firoyang.com/\n")
	sb.WriteString("  2. 若使用代理客户端，需显式设置环境变量（Dec 不读取系统代理/PAC）：\n")
	if runtime.GOOS == "windows" {
		sb.WriteString("     $env:HTTPS_PROXY=\"http://127.0.0.1:<端口>\"\n")
	} else {
		sb.WriteString("     export HTTPS_PROXY=http://127.0.0.1:<端口>\n")
	}
	sb.WriteString("  3. 修好网络后重试；不必为此重装 Dec")
	return sb.String()
}

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

// FormatUpdateHint formats an update hint for stderr before TUI starts.
func FormatUpdateHint(result *CheckResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("新版本可用: %s -> %s\n", result.CurrentVersion, result.LatestVersion))
	sb.WriteString("打开 TUI → Run 页按 u 更新到最新版本")
	return sb.String()
}

// describeRequestError keeps legacy helper for tests that may still reference patterns.
func describeRequestError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return "请求超时"
		}
		if urlErr.Err != nil {
			return urlErr.Err.Error()
		}
	}
	return err.Error()
}
