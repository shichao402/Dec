package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/secrets"
)

type secretsSyncPlan struct {
	EnabledBundles     []string
	ProjectSecretsName string
	Total              int
}

func planSecretsSync(projectRoot string, enabledBundles []string, cfg *secrets.Config) (*secretsSyncPlan, error) {
	plan := &secretsSyncPlan{
		EnabledBundles: append([]string(nil), enabledBundles...),
	}
	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	projectName, _ := ResolveProjectName(projectRoot, projectConfig)
	if cfg != nil {
		if name, enabled := cfg.ResolveProjectSecrets(projectName); enabled {
			plan.ProjectSecretsName = name
		}
	}
	plan.Total = len(plan.EnabledBundles)
	if plan.ProjectSecretsName != "" {
		plan.Total++
	}
	return plan, nil
}

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
		emit(reporter, EventInfo, "pull.secrets", "Bitwarden 未配置，跳过 secrets 同步", nil)
		return summary, nil
	}

	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}

	plan, err := planSecretsSync(projectRoot, enabledBundles, cfg)
	if err != nil {
		return nil, err
	}
	if plan.Total == 0 {
		summary.SkippedReason = "无已启用 bundle 或 project secrets"
		emit(reporter, EventInfo, "pull.secrets", "无已启用 bundle 或 project secrets，跳过 secrets 同步", nil)
		return summary, nil
	}

	if !secrets.HasSession() {
		emit(reporter, EventInfo, "pull.secrets", "[auth] pull: Bitwarden session required", nil)
		if err := ensureBitwardenSession(ctx, reporter, "pull.secrets"); err != nil {
			return nil, err
		}
	}
	if !secrets.HasUserKey() {
		return nil, fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}

	client := secretsClientFactory()
	total := plan.Total
	emit(reporter, EventInfo, "pull.secrets", fmt.Sprintf("同步 %d 个 secrets 目标（bundle + project）", total), &Progress{Phase: "secrets", Current: 0, Total: total})

	if err := migrateSecretsTargets(ctx, projectRoot, plan, reporter); err != nil {
		return nil, err
	}

	idx := 0
	for _, bundleName := range plan.EnabledBundles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		idx++
		progress := &Progress{Phase: "secrets", Current: idx, Total: total}
		binding := cfg.ResolveBinding(bundleName)
		emit(reporter, EventInfo, "pull.secrets",
			fmt.Sprintf("拉取 secrets bundle %q (Bitwarden folder: %s)", bundleName, binding.SecretsBundleName), progress)

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
		switch {
		case len(paths) > 0:
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  落地 %d 个 Secure Note: %s", len(paths), strings.Join(paths, ", ")), progress)
		case binding.SecretsBundleName != "":
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  Bitwarden folder %q 无 Secure Note 或 folder 不存在，跳过", binding.SecretsBundleName), progress)
		}
	}

	if plan.ProjectSecretsName != "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		idx++
		progress := &Progress{Phase: "secrets", Current: idx, Total: total}
		binding := secrets.ProjectSecretsBinding(plan.ProjectSecretsName)
		emit(reporter, EventInfo, "pull.secrets",
			fmt.Sprintf("拉取 project secrets %q (Bitwarden folder: %s)", plan.ProjectSecretsName, binding.SecretsBundleName), progress)

		paths, pullErr := secrets.PullBundle(ctx, client, secrets.PullBundleRequest{
			ProjectRoot:   projectRoot,
			DecBundleName: secrets.ProjectSecretsDecBundleName,
			Binding:       binding,
		})
		if pullErr != nil {
			return nil, fmt.Errorf("拉取 project secrets %q 失败: %w", plan.ProjectSecretsName, pullErr)
		}
		summary.NoteCount += len(paths)
		summary.LandingPaths = append(summary.LandingPaths, paths...)
		switch {
		case len(paths) > 0:
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  落地 %d 个 Secure Note: %s", len(paths), strings.Join(paths, ", ")), progress)
		default:
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  Bitwarden folder %q 无 Secure Note 或 folder 不存在，跳过", binding.SecretsBundleName), progress)
		}
	}

	if summary.NoteCount == 0 {
		emit(reporter, EventInfo, "pull.secrets", "secrets 同步完成（无变更）", &Progress{Phase: "secrets", Current: total, Total: total})
	} else {
		emit(reporter, EventInfo, "pull.secrets",
			fmt.Sprintf("secrets 同步完成：%d 个文件", summary.NoteCount),
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
