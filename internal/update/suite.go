package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cnb.cool/shichao402/relkit/sdk"
	"github.com/shichao402/Dec/internal/config"
)

// SuiteComponents is the four-piece Dec runtime installed under ~/.dec/bin.
var SuiteComponents = []string{"dec", "dec-server", "dec-mcp", "dec-exec"}

// DownloadSuite downloads the signed runtime suite for goos/goarch into destDir.
//
// Selectors use audience=runtime so user-facing Console packages are never chosen.
// destDir receives bare component names (with .exe on Windows targets).
func DownloadSuite(ctx context.Context, version, goos, goarch, destDir string) error {
	version = normalizeSuiteVersion(version)
	goos = strings.TrimSpace(goos)
	goarch = strings.TrimSpace(goarch)
	if version == "" || goos == "" || goarch == "" {
		return fmt.Errorf("version/os/arch 不能为空")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	for _, component := range SuiteComponents {
		// relkit 当前会选择最高可达版本，并不提供按历史版本直接解析的 API。
		// code-1 只保证钉死版本仍是候选；若渠道已前移，下面会明确拒绝 head。
		updater, err := newUpdaterFor(version, component, goos, goarch)
		if err != nil {
			return err
		}
		result := updater.CheckForce(ctx, true)
		if result.Err != nil {
			return fmt.Errorf("检查 %s/%s/%s 失败: %w", component, goos, goarch, result.Err)
		}
		if result.Available == nil || result.Available.Target == nil {
			return fmt.Errorf("发布缺少 %s（%s/%s）", component, goos, goarch)
		}
		targetVer := result.Available.Target.Version
		if !strings.HasPrefix(targetVer, "v") {
			targetVer = "v" + targetVer
		}
		if err := validatePinnedSuiteVersion(version, targetVer); err != nil {
			return fmt.Errorf("%s: %w", component, err)
		}
		dest := filepath.Join(destDir, component+ext)
		if err := updater.Download(ctx, result.Available, dest); err != nil {
			return fmt.Errorf("下载 %s 失败: %w", component, err)
		}
		if err := os.Chmod(dest, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func validatePinnedSuiteVersion(requested, resolved string) error {
	requested = normalizeSuiteVersion(requested)
	resolved = normalizeSuiteVersion(resolved)
	if strings.EqualFold(strings.TrimPrefix(requested, "v"), strings.TrimPrefix(resolved, "v")) {
		return nil
	}
	requestedCode, requestedErr := sdk.SemverCode(requested)
	resolvedCode, resolvedErr := sdk.SemverCode(resolved)
	if requestedErr == nil && resolvedErr == nil && resolvedCode > requestedCode {
		return fmt.Errorf(
			"渠道已有更高版本 %s，Console 钉死版本为 %s；请先更新 Console，或预先准备该版本缓存",
			resolved, requested)
	}
	return fmt.Errorf("RUP 解析版本 %s 与 Console 钉死版本 %s 不一致", resolved, requested)
}

func newUpdaterFor(targetVersion, component, goos, goarch string) (*sdk.Updater, error) {
	keys, err := trustedKeys()
	if err != nil {
		return nil, err
	}
	code, err := sdk.SemverCode(targetVersion)
	if err != nil {
		return nil, fmt.Errorf("无效目标版本 %q: %w", targetVersion, err)
	}
	if code <= 0 {
		return nil, fmt.Errorf("无效目标版本 code: %d", code)
	}
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	cfg := config.GetSystemConfig()
	channel := cfg.Channel
	if channel == "" {
		channel = channelName
	}
	return &sdk.Updater{
		Product:     productName,
		Channel:     channel,
		CurrentCode: code - 1,
		EntryURLs:   entryURLs(),
		TrustedKeys: keys,
		ClientSelectors: map[string]string{
			"os":        goos,
			"arch":      goarch,
			"component": component,
			"audience":  "runtime",
		},
		// 独立状态避免用户在普通自更新里 skip 某版本后阻断 Console 钉死的套件，
		// 也避免不同组件/平台并行下载时争写同一个状态文件。
		StateStore: sdk.NewFileStateStore(
			dir, strings.Join([]string{productName, "suite", goos, goarch, component}, "-"), channel),
		Policy: sdk.DefaultPolicy(),
	}, nil
}

func normalizeSuiteVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}
