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
	updaterv1 "go.firoyang.com/relkit/api/updater/v1"
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
	var cfg struct {
		Directory struct {
			EntryURLs []string `json:"entryUrls"`
		} `json:"directory"`
	}
	if err := json.Unmarshal(embeddedRelkitJSON, &cfg); err == nil && len(cfg.Directory.EntryURLs) > 0 {
		return append([]string(nil), cfg.Directory.EntryURLs...)
	}
	return []string{
		"https://raw.firoyang.com/rup/directory/dec.pb",
	}
}

func stateDir() (string, error) {
	root, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return root, nil
}

func newRuntime(currentVersion, component string) (*updaterv1.Runtime, error) {
	code := semverCodeOrZero(currentVersion)
	root, err := binInstallRoot()
	if err != nil {
		return nil, err
	}
	return fileSetRuntime(component, runtime.GOOS, runtime.GOARCH, code, root), nil
}

// Check checks whether a newer version is available via RUP.
func Check(currentVersion string) (*CheckResult, error) {
	ctx := context.Background()
	rt, err := newRuntime(currentVersion, "dec")
	if err != nil {
		recordFailedAttempt()
		return nil, err
	}
	u, err := openUpdater(ctx, rt)
	if err != nil {
		recordFailedAttempt()
		return nil, err
	}
	available, err := checkAvailable(ctx, u, true, 0)
	if err != nil {
		recordFailedAttempt()
		return nil, err
	}
	latest := currentVersion
	need := false
	if available != nil {
		latest = available.GetVersion()
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

// DoUpdate downloads and replaces the runtime suite via relkit-updater fileSet apply.
func DoUpdate(currentVersion, latestVersion string) error {
	_ = strings.TrimSpace(latestVersion)
	ctx := context.Background()
	rt, err := newRuntime(currentVersion, "dec")
	if err != nil {
		return err
	}
	u, err := openUpdater(ctx, rt)
	if err != nil {
		return err
	}
	available, err := checkAvailable(ctx, u, true, 0)
	if err != nil {
		return err
	}
	if available == nil {
		return fmt.Errorf("当前已是最新版本 %s", currentVersion)
	}
	targetVer := available.GetVersion()
	if !strings.HasPrefix(targetVer, "v") {
		targetVer = "v" + targetVer
	}
	if !version.NeedUpdate(currentVersion, targetVer) {
		return fmt.Errorf("当前已是最新版本 %s", currentVersion)
	}

	root, err := binInstallRoot()
	if err != nil {
		return err
	}
	for _, component := range SuiteComponents {
		crt := fileSetRuntime(component, runtime.GOOS, runtime.GOARCH, semverCodeOrZero(currentVersion), root)
		cu, err := openUpdater(ctx, crt)
		if err != nil {
			return err
		}
		got, err := checkAvailable(ctx, cu, true, 0)
		if err != nil {
			return err
		}
		if got == nil {
			return fmt.Errorf("发布缺少 %s 组件", component)
		}
		if err := downloadAndApply(ctx, cu, got.GetPlanId()); err != nil {
			return fmt.Errorf("更新 %s 失败: %w", component, err)
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
		branch = "main"
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

// FormatUpdateHint formats an update hint for stderr.
func FormatUpdateHint(result *CheckResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("新版本可用: %s -> %s\n", result.CurrentVersion, result.LatestVersion))
	sb.WriteString("打开 Dec Console 的同步页更新到最新版本")
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
