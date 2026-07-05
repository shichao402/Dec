package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/shichao402/Dec/pkg/secrets"
)

// secretsClientFactory 供测试注入 stub Client。
var secretsClientFactory = secrets.DefaultClient

type secretsPullSummary struct {
	SkippedReason string
	NoteCount     int
	LandingPaths  []string
}

func pullEnabledSecretsBundles(ctx context.Context, projectRoot string, enabledBundles []string, reporter Reporter) (*secretsPullSummary, error) {
	reporter = defaultReporter(reporter)
	summary := &secretsPullSummary{}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		summary.SkippedReason = "Bitwarden 未配置"
		emit(reporter, EventInfo, "pull.secrets", "Bitwarden 未配置，跳过 secrets bundle 同步", nil)
		return summary, nil
	}
	if len(enabledBundles) == 0 {
		summary.SkippedReason = "无已启用 bundle"
		emit(reporter, EventInfo, "pull.secrets", "无已启用 bundle，跳过 secrets bundle 同步", nil)
		return summary, nil
	}

	if !secrets.HasSession() {
		emit(reporter, EventInfo, "pull.secrets", "Bitwarden 未解锁，启动 web unlock…", nil)
		if err := secrets.EnsureSession(ctx, &secrets.EnsureSessionOpts{
			OnUnlockURL: func(url string) {
				emit(reporter, EventInfo, "pull.secrets", fmt.Sprintf("Bitwarden 解锁页: %s", url), nil)
				emit(reporter, EventInfo, "pull.secrets", "若浏览器未自动打开，请复制上方链接到浏览器访问", nil)
			},
			OnBrowserError: func(err error) {
				emit(reporter, EventWarn, "pull.secrets", fmt.Sprintf("无法自动打开浏览器: %v", err), nil)
			},
		}); err != nil {
			return nil, fmt.Errorf("Bitwarden 解锁失败: %w", err)
		}
		emit(reporter, EventInfo, "pull.secrets", "Bitwarden 已解锁", nil)
	}

	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}

	client := secretsClientFactory()
	total := len(enabledBundles)
	emit(reporter, EventInfo, "pull.secrets", fmt.Sprintf("同步 %d 个 secrets bundle", total), &Progress{Phase: "secrets", Current: 0, Total: total})

	for idx, bundleName := range enabledBundles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress := &Progress{Phase: "secrets", Current: idx + 1, Total: total}
		binding := cfg.ResolveBinding(bundleName)
		emit(reporter, EventInfo, "pull.secrets",
			fmt.Sprintf("拉取 secrets bundle %q (folder: %s)", bundleName, binding.BitwardenFolder), progress)

		paths, pullErr := secrets.PullBundle(ctx, client, secrets.PullBundleRequest{
			ProjectRoot:   projectRoot,
			DecBundleName: bundleName,
			Binding:       binding,
		})
		if pullErr != nil {
			return nil, fmt.Errorf("拉取 secrets bundle %q 失败: %w", bundleName, pullErr)
		}
		summary.NoteCount += len(paths)
		summary.LandingPaths = append(summary.LandingPaths, paths...)
		if len(paths) > 0 {
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  落地 %d 个 Secure Note: %s", len(paths), strings.Join(paths, ", ")), progress)
		}
	}

	if summary.NoteCount == 0 {
		emit(reporter, EventInfo, "pull.secrets", "secrets bundle 同步完成（无变更）", &Progress{Phase: "secrets", Current: total, Total: total})
	} else {
		emit(reporter, EventInfo, "pull.secrets",
			fmt.Sprintf("secrets bundle 同步完成：%d 个文件", summary.NoteCount),
			&Progress{Phase: "secrets", Current: total, Total: total})
	}
	return summary, nil
}

func validateSecretsPathOverlap(projectRoot string, landingPaths []string, reporter Reporter) error {
	if len(landingPaths) == 0 {
		return nil
	}
	emit(reporter, EventInfo, "pull.validate", "校验 .dec/ 与 secrets 落地路径零重叠", nil)
	if err := secrets.ValidateNoOverlap(projectRoot, landingPaths); err != nil {
		emit(reporter, EventError, "pull.validate", err.Error(), nil)
		return err
	}
	emit(reporter, EventInfo, "pull.validate", "零重叠校验通过", nil)
	return nil
}
