package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/version"
	"github.com/shichao402/relkit/sdk"
	"github.com/shichao402/relkit/sdk/apply"
)

const (
	checkInterval = 24 * time.Hour
	retryInterval = time.Hour
	stateFile     = "update_state.json"
	productName   = "dec"
	channelName   = "dev"
	keyID         = "dec-2026"
)

// Embedded trusted public key (base64). Must match relkit.json signing.publicKeys.
const embeddedPublicKeyB64 = "zVkesjz/3BhLrZ9qCvSJN0OdrIsePL4+v6AI9CCtio4="

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

func trustedKeys() (sdk.TrustedKeys, error) {
	raw, err := base64.StdEncoding.DecodeString(embeddedPublicKeyB64)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length %d", len(raw))
	}
	return sdk.TrustedKeys{keyID: ed25519.PublicKey(raw)}, nil
}

func stateDir() (string, error) {
	root, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return root, nil
}

func newUpdater(currentVersion string) (*sdk.Updater, error) {
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
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
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
	u, err := newUpdater(currentVersion)
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
	u, err := newUpdater(currentVersion)
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
	dest := filepath.Join(tmpDir, "dec"+ext)
	if err := u.Download(context.Background(), result.Available, dest); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	_ = apply.CleanupStaleExecutable()
	if err := apply.ReplaceExecutable(dest); err != nil {
		return err
	}
	now := time.Now()
	_ = saveState(&CheckState{LastCheck: now, LatestVersion: targetVer, LastAttempt: now})
	return nil
}

// ManualInstallCommand returns the direct install command.
func ManualInstallCommand() string {
	return manualInstallCommand(runtime.GOOS, false)
}

// MirrorInstallCommand returns a CDN-mirrored install command (legacy GitHub path).
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

// NetworkHelp returns troubleshooting hints.
func NetworkHelp() string {
	var sb strings.Builder
	sb.WriteString("网络不可达时可以尝试：\n")
	sb.WriteString("  1. 确认可访问 https://updates.firoyang.com/\n")
	sb.WriteString("  2. 走 CDN 镜像手动覆盖安装：\n")
	sb.WriteString("     " + MirrorInstallCommand() + "\n")
	sb.WriteString("  3. 直连 GitHub 手动覆盖安装：\n")
	sb.WriteString("     " + ManualInstallCommand() + "\n")
	sb.WriteString("  4. 若使用代理客户端，需显式导出环境变量（Dec 不读取系统代理/PAC 设置）：\n")
	sb.WriteString("     export HTTPS_PROXY=http://127.0.0.1:<端口>")
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

// FormatUpdateHint formats an update hint.
func FormatUpdateHint(result *CheckResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("新版本可用: %s -> %s\n", result.CurrentVersion, result.LatestVersion))
	sb.WriteString("运行 dec update 更新到最新版本")
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
