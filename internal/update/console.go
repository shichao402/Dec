package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.firoyang.com/relkit/sdk"
)

const consoleComponent = "console"

// ConsoleCheckResult is the shell-owned update state shown by Dec Console.
type ConsoleCheckResult struct {
	CurrentVersion       string    `json:"currentVersion"`
	LatestVersion        string    `json:"latestVersion"`
	NeedUpdate           bool      `json:"needUpdate"`
	Mandatory            bool      `json:"mandatory"`
	ReleaseNotesMarkdown string    `json:"releaseNotesMarkdown,omitempty"`
	ReleaseNotesURL      string    `json:"releaseNotesUrl,omitempty"`
	CheckedAt            time.Time `json:"checkedAt"`
	FromCache            bool      `json:"fromCache,omitempty"`
	AutoCheckInterval    string    `json:"autoCheckInterval"`
	CanAutoInstall       bool      `json:"canAutoInstall"`
}

type consoleUpdateCache struct {
	ConsoleCheckResult
}

// CheckConsoleUpdate checks the signed user-facing Console artifact.
// A throttled automatic check returns the last successful result.
func CheckConsoleUpdate(ctx context.Context, currentVersion, dataDir string, force bool) (*ConsoleCheckResult, error) {
	currentVersion = normalizeSuiteVersion(currentVersion)
	updater, err := newConsoleUpdater(currentVersion, dataDir)
	if err != nil {
		return nil, err
	}
	result := updater.CheckForce(ctx, force)
	if result.Throttled {
		if cached, err := loadConsoleUpdateCache(dataDir); err == nil {
			cached.CurrentVersion = currentVersion
			cached.NeedUpdate = versionCode(cached.LatestVersion) > versionCode(currentVersion)
			cached.FromCache = true
			return cached, nil
		}
		return &ConsoleCheckResult{
			CurrentVersion:    currentVersion,
			LatestVersion:     currentVersion,
			CheckedAt:         time.Now(),
			FromCache:         true,
			AutoCheckInterval: checkInterval.String(),
			CanAutoInstall:    runtime.GOOS == "windows",
		}, nil
	}
	if result.Err != nil {
		return nil, fmt.Errorf("检查 Console 更新失败: %w", result.Err)
	}

	out := &ConsoleCheckResult{
		CurrentVersion:    currentVersion,
		LatestVersion:     currentVersion,
		CheckedAt:         time.Now(),
		AutoCheckInterval: checkInterval.String(),
		CanAutoInstall:    runtime.GOOS == "windows",
	}
	if result.Available != nil && result.Available.Target != nil {
		out.LatestVersion = normalizeSuiteVersion(result.Available.Target.Version)
		out.NeedUpdate = versionCode(out.LatestVersion) > versionCode(currentVersion)
		out.Mandatory = result.Available.Mandatory
		out.ReleaseNotesMarkdown = result.Available.Target.Notes
		out.ReleaseNotesURL = result.Available.Target.NotesUrl
	}
	if err := saveConsoleUpdateCache(dataDir, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DownloadConsoleUpdate downloads and verifies the latest Console installer.
func DownloadConsoleUpdate(ctx context.Context, currentVersion, dataDir string) (string, *ConsoleCheckResult, error) {
	status, err := CheckConsoleUpdate(ctx, currentVersion, dataDir, true)
	if err != nil {
		return "", nil, err
	}
	if !status.NeedUpdate {
		return "", status, fmt.Errorf("当前已是最新版本 %s", currentVersion)
	}
	updater, err := newConsoleUpdater(normalizeSuiteVersion(currentVersion), dataDir)
	if err != nil {
		return "", nil, err
	}
	check := updater.CheckForce(ctx, true)
	if check.Err != nil {
		return "", nil, check.Err
	}
	if check.Available == nil || check.Available.Artifact == nil {
		return "", nil, fmt.Errorf("更新包不可用")
	}
	name := filepath.Base(strings.TrimSpace(check.Available.Artifact.Filename))
	if name == "" || name == "." {
		return "", nil, fmt.Errorf("更新包文件名无效")
	}
	downloadDir := filepath.Join(dataDir, "downloads")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return "", nil, err
	}
	dest := filepath.Join(downloadDir, name)
	if err := updater.Download(ctx, check.Available, dest); err != nil {
		return "", nil, fmt.Errorf("下载 Console 更新失败: %w", err)
	}
	return dest, status, nil
}

func newConsoleUpdater(currentVersion, dataDir string) (*sdk.Updater, error) {
	keys, err := protoTrustedKeys()
	if err != nil {
		return nil, err
	}
	trusted := make(sdk.TrustedKeys, len(keys))
	for _, key := range keys {
		trusted[key.KeyId] = key.PublicKey
	}
	code, err := sdk.SemverCode(currentVersion)
	if err != nil {
		return nil, fmt.Errorf("无效 Console 版本 %q: %w", currentVersion, err)
	}
	return &sdk.Updater{
		Product:     productName,
		Channel:     channelName,
		CurrentCode: code,
		EntryURLs:   entryURLs(),
		TrustedKeys: trusted,
		ClientSelectors: map[string]string{
			"os":        runtime.GOOS,
			"arch":      runtime.GOARCH,
			"component": consoleComponent,
			"audience":  "user",
		},
		StateStore: sdk.NewFileStateStore(filepath.Join(dataDir, "rup"), productName, channelName),
	}, nil
}

func versionCode(value string) int {
	code, _ := sdk.SemverCode(normalizeSuiteVersion(value))
	return code
}

func consoleUpdateCachePath(dataDir string) string {
	return filepath.Join(dataDir, "console-update.json")
}

func loadConsoleUpdateCache(dataDir string) (*ConsoleCheckResult, error) {
	raw, err := os.ReadFile(consoleUpdateCachePath(dataDir))
	if err != nil {
		return nil, err
	}
	var cached consoleUpdateCache
	if err := json.Unmarshal(raw, &cached); err != nil {
		return nil, err
	}
	return &cached.ConsoleCheckResult, nil
}

func saveConsoleUpdateCache(dataDir string, result *ConsoleCheckResult) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(consoleUpdateCache{ConsoleCheckResult: *result}, "", "  ")
	if err != nil {
		return err
	}
	temp := consoleUpdateCachePath(dataDir) + ".tmp"
	if err := os.WriteFile(temp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temp, consoleUpdateCachePath(dataDir))
}
