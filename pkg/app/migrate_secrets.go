package app

import (
	"context"
	"fmt"

	"github.com/shichao402/Dec/pkg/secrets"
)

func migrateSecretsConfig(reporter Reporter) error {
	changed, err := secrets.MigrateConfigIfNeeded()
	if err != nil {
		return fmt.Errorf("迁移 secrets 配置失败: %w", err)
	}
	if changed {
		emit(reporter, EventInfo, "migrate.secrets", "已将 ~/.dec/secrets/config.yaml 中废弃 folder 字段迁移为 secrets_bundle", nil)
	}
	return nil
}

func migrateSecretsTargets(ctx context.Context, projectRoot string, plan *secretsSyncPlan, reporter Reporter) error {
	reporter = defaultReporter(reporter)
	if plan == nil || plan.Total == 0 {
		return nil
	}
	if err := migrateSecretsConfig(reporter); err != nil {
		return err
	}

	cfg, err := secrets.LoadConfig()
	if err != nil {
		return err
	}
	client := secretsClientFactory()
	total := plan.Total
	idx := 0

	for _, bundleName := range plan.EnabledBundles {
		if err := ctx.Err(); err != nil {
			return err
		}
		idx++
		binding := cfg.ResolveBinding(bundleName)
		if err := emitMigrateBundleResult(ctx, client, projectRoot, bundleName, binding, idx, total, reporter); err != nil {
			return err
		}
	}

	if plan.ProjectSecretsName != "" {
		if err := ctx.Err(); err != nil {
			return err
		}
		idx++
		binding := secrets.ProjectSecretsBinding(plan.ProjectSecretsName)
		if err := emitMigrateBundleResult(ctx, client, projectRoot, secrets.ProjectSecretsDecBundleName, binding, idx, total, reporter); err != nil {
			return err
		}
	}
	return nil
}

func migrateEnabledSecretsBundles(ctx context.Context, projectRoot string, enabledBundles []string, reporter Reporter) error {
	cfg, err := secrets.LoadConfig()
	if err != nil {
		return err
	}
	plan, err := planSecretsSync(projectRoot, enabledBundles, cfg)
	if err != nil {
		return err
	}
	return migrateSecretsTargets(ctx, projectRoot, plan, reporter)
}

func emitMigrateBundleResult(ctx context.Context, client secrets.Client, projectRoot, decBundleName string, binding secrets.BundleBinding, idx, total int, reporter Reporter) error {
	result, migrateErr := secrets.MigrateBundle(ctx, client, secrets.MigrateBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: decBundleName,
		Binding:       binding,
	})
	if migrateErr != nil {
		label := decBundleName
		if decBundleName == secrets.ProjectSecretsDecBundleName {
			label = "project secrets " + binding.SecretsBundleName
		}
		return fmt.Errorf("迁移 secrets %q 失败: %w", label, migrateErr)
	}
	if result == nil {
		return nil
	}
	progress := &Progress{Phase: "secrets", Current: idx, Total: total}
	for _, note := range result.RenamedNotes {
		emit(reporter, EventInfo, "migrate.secrets",
			fmt.Sprintf("重命名 Bitwarden note %s", note), progress)
	}
	for _, move := range result.MovedLocal {
		emit(reporter, EventInfo, "migrate.secrets",
			fmt.Sprintf("移动本地 %s", move), progress)
	}
	for _, cipherID := range result.SkippedCiphers {
		emit(reporter, EventWarn, "migrate.secrets",
			fmt.Sprintf("跳过无法解密的 cipher (id=%s)", cipherID), progress)
	}
	return nil
}
