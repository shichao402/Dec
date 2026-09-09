package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.firoyang.com/relkit/sdk"
)

// SuiteComponents is the Dec runtime installed under ~/.dec/bin.
// relkit-updater is the fifth fileSet member once ADR 0010 sidecar is published.
var SuiteComponents = []string{"dec-server", "dec-mcp", "dec-exec", "dec-host-setup"}

const UpdaterComponent = "relkit-updater"

// DownloadSuite downloads the signed runtime suite for goos/goarch into destDir.
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
	code, err := sdk.SemverCode(version)
	if err != nil {
		return fmt.Errorf("无效目标版本 %q: %w", version, err)
	}
	if code <= 0 {
		return fmt.Errorf("无效目标版本 code: %d", code)
	}
	for _, component := range SuiteComponents {
		rt := fileSetRuntime(component, goos, goarch, int64(code-1), destDir)
		u, err := openUpdater(ctx, rt)
		if err != nil {
			return err
		}
		got, err := checkAvailable(ctx, u, true, int64(code))
		if err != nil {
			return fmt.Errorf("检查 %s/%s/%s 失败: %w", component, goos, goarch, err)
		}
		if got == nil {
			return fmt.Errorf("发布缺少 %s（%s/%s）", component, goos, goarch)
		}
		targetVer := got.GetVersion()
		if !strings.HasPrefix(targetVer, "v") {
			targetVer = "v" + targetVer
		}
		if err := validatePinnedSuiteVersion(version, targetVer); err != nil {
			return fmt.Errorf("%s: %w", component, err)
		}
		if err := downloadAndApply(ctx, u, got.GetPlanId()); err != nil {
			return fmt.Errorf("下载 %s 失败: %w", component, err)
		}
		name := component
		if goos == "windows" {
			name += ".exe"
		}
		if err := os.Chmod(filepath.Join(destDir, name), 0o755); err != nil && !os.IsNotExist(err) {
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
