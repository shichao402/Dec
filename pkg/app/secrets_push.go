package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/secrets"
)

type PushSecretsResult struct {
	SkippedReason string
	CreatedCount  int
	UpdatedCount  int
	PushedPaths   []string
	// MissingLocal 是远端有 note、本地缺文件的落地路径。push 只报告，删除走 Delete 页。
	MissingLocal []string
}

func PushSecretsBundles(ctx context.Context, projectRoot string, reporter Reporter) (*PushSecretsResult, error) {
	reporter = defaultReporter(reporter)
	result := &PushSecretsResult{}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		result.SkippedReason = "Bitwarden 未配置"
		emit(reporter, EventInfo, "push.secrets", "Bitwarden 未配置，跳过 secrets 推送", nil)
		return result, nil
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	enabledBundles := append([]string(nil), projectConfig.EnabledBundles...)

	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}

	plan, err := planSecretsSync(projectRoot, enabledBundles, cfg)
	if err != nil {
		return nil, err
	}
	if plan.Total == 0 {
		result.SkippedReason = "无已启用 bundle 或 project secrets"
		emit(reporter, EventInfo, "push.secrets", "无已启用 bundle 或 project secrets，跳过 secrets 推送", nil)
		return result, nil
	}

	if !secrets.HasSession() {
		if err := ensureBitwardenSession(ctx, reporter, "push.secrets"); err != nil {
			return nil, err
		}
	}

	client := secretsClientFactory()
	total := plan.Total
	emit(reporter, EventInfo, "push.secrets", fmt.Sprintf("推送 %d 个 secrets 目标（bundle + project）", total), &Progress{Phase: "secrets", Current: 0, Total: total})

	targets := make([]secretsPullTarget, 0, total)
	for _, bundleName := range plan.EnabledBundles {
		targets = append(targets, secretsPullTarget{
			Label:         fmt.Sprintf("secrets bundle %q", bundleName),
			DecBundleName: bundleName,
			Binding:       cfg.ResolveBinding(bundleName),
		})
	}
	if plan.ProjectSecretsName != "" {
		targets = append(targets, secretsPullTarget{
			Label:         fmt.Sprintf("project secrets %q", plan.ProjectSecretsName),
			DecBundleName: secrets.ProjectSecretsDecBundleName,
			Binding:       secrets.ProjectSecretsBinding(plan.ProjectSecretsName),
		})
	}

	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress := &Progress{Phase: "secrets", Current: i + 1, Total: total}
		emit(reporter, EventInfo, "push.secrets",
			fmt.Sprintf("推送 %s (Bitwarden folder: %s)", target.Label, target.Binding.SecretsBundleName), progress)

		pushResult, pushErr := secrets.PushBundle(ctx, client, secrets.PushBundleRequest{
			ProjectRoot:   projectRoot,
			DecBundleName: target.DecBundleName,
			Binding:       target.Binding,
		})
		if pushErr != nil {
			return nil, fmt.Errorf("推送 %s 失败: %w", target.Label, pushErr)
		}
		if pushResult == nil {
			continue
		}
		result.CreatedCount += pushResult.Created
		result.UpdatedCount += pushResult.Updated
		result.PushedPaths = append(result.PushedPaths, pushResult.Paths...)
		result.MissingLocal = append(result.MissingLocal, pushResult.MissingLocal...)

		if len(pushResult.Paths) > 0 {
			emit(reporter, EventInfo, "push.secrets",
				fmt.Sprintf("  推送 %d 个 Secure Note（新建 %d · 更新 %d）: %s",
					len(pushResult.Paths), pushResult.Created, pushResult.Updated, strings.Join(pushResult.Paths, ", ")), progress)
		} else {
			emit(reporter, EventInfo, "push.secrets",
				fmt.Sprintf("  %s 无可推送内容（Bitwarden folder %q 无 Note，或 folder 不存在）",
					target.Label, target.Binding.SecretsBundleName), progress)
		}
		if len(pushResult.MissingLocal) > 0 {
			emit(reporter, EventWarn, "push.secrets",
				fmt.Sprintf("  ⚠️  %d 个 Note 在本地找不到对应文件，已跳过（不会删除远端）: %s",
					len(pushResult.MissingLocal), strings.Join(pushResult.MissingLocal, ", ")), progress)
			emit(reporter, EventInfo, "push.secrets", "  确实要删除请到 Delete 页逐条确认", progress)
		}
	}

	totalPushed := result.CreatedCount + result.UpdatedCount
	if totalPushed == 0 {
		emit(reporter, EventInfo, "push.secrets", "secrets 推送完成（无变更）", &Progress{Phase: "done", Current: total, Total: total})
	} else {
		emit(reporter, EventInfo, "push.secrets",
			fmt.Sprintf("secrets 推送完成：新建 %d · 更新 %d", result.CreatedCount, result.UpdatedCount),
			&Progress{Phase: "done", Current: total, Total: total})
	}
	return result, nil
}
