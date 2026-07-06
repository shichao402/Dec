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

	idx := 0
	for _, bundleName := range plan.EnabledBundles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		idx++
		progress := &Progress{Phase: "secrets", Current: idx, Total: total}
		binding := cfg.ResolveBinding(bundleName)
		emit(reporter, EventInfo, "push.secrets",
			fmt.Sprintf("推送 secrets bundle %q (Bitwarden folder: %s)", bundleName, binding.SecretsBundleName), progress)

		pushResult, pushErr := secrets.PushBundle(ctx, client, secrets.PushBundleRequest{
			ProjectRoot:   projectRoot,
			DecBundleName: bundleName,
			Binding:       binding,
		})
		if pushErr != nil {
			return nil, fmt.Errorf("推送 secrets bundle %q 失败: %w", bundleName, pushErr)
		}
		if pushResult == nil {
			continue
		}
		result.CreatedCount += pushResult.Created
		result.UpdatedCount += pushResult.Updated
		result.PushedPaths = append(result.PushedPaths, pushResult.Paths...)
		if len(pushResult.Paths) > 0 {
			emit(reporter, EventInfo, "push.secrets",
				fmt.Sprintf("  推送 %d 个 Secure Note（新建 %d · 更新 %d）: %s",
					len(pushResult.Paths), pushResult.Created, pushResult.Updated, strings.Join(pushResult.Paths, ", ")), progress)
		} else {
			emit(reporter, EventInfo, "push.secrets",
				fmt.Sprintf("  %s 本地无文件（%s）", bundleName, secrets.SecretsBundleDir(projectRoot, binding.SecretsBundleName)), progress)
		}
	}

	if plan.ProjectSecretsName != "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		idx++
		progress := &Progress{Phase: "secrets", Current: idx, Total: total}
		binding := secrets.ProjectSecretsBinding(plan.ProjectSecretsName)
		emit(reporter, EventInfo, "push.secrets",
			fmt.Sprintf("推送 project secrets %q (Bitwarden folder: %s)", plan.ProjectSecretsName, binding.SecretsBundleName), progress)

		pushResult, pushErr := secrets.PushBundle(ctx, client, secrets.PushBundleRequest{
			ProjectRoot:   projectRoot,
			DecBundleName: secrets.ProjectSecretsDecBundleName,
			Binding:       binding,
		})
		if pushErr != nil {
			return nil, fmt.Errorf("推送 project secrets %q 失败: %w", plan.ProjectSecretsName, pushErr)
		}
		if pushResult != nil {
			result.CreatedCount += pushResult.Created
			result.UpdatedCount += pushResult.Updated
			result.PushedPaths = append(result.PushedPaths, pushResult.Paths...)
			if len(pushResult.Paths) > 0 {
				emit(reporter, EventInfo, "push.secrets",
					fmt.Sprintf("  推送 %d 个 Secure Note（新建 %d · 更新 %d）: %s",
						len(pushResult.Paths), pushResult.Created, pushResult.Updated, strings.Join(pushResult.Paths, ", ")), progress)
			} else {
				emit(reporter, EventInfo, "push.secrets",
					fmt.Sprintf("  project secrets 本地无文件（%s）", secrets.SecretsBundleDir(projectRoot, binding.SecretsBundleName)), progress)
			}
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
