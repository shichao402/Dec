package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/bundle"
	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

type PushProjectAssetsResult struct {
	DecPushedCount       int
	DecSkippedReason     string
	VersionCommit        string
	SecretsCreatedCount  int
	SecretsUpdatedCount  int
	SecretsSkippedReason string
}

// PushProjectAssets 将本地 .dec/cache/ 与 secrets 落地文件推送到远端（Dec Git vault + Bitwarden）。
func PushProjectAssets(ctx context.Context, projectRoot string, reporter Reporter) (*PushProjectAssetsResult, error) {
	reporter = defaultReporter(reporter)
	result := &PushProjectAssetsResult{}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	decPushed, decSkipped, commit, err := pushDecBundles(ctx, projectRoot, reporter)
	if err != nil {
		return nil, fmt.Errorf("push.dec 失败: %w", err)
	}
	result.DecPushedCount = decPushed
	result.DecSkippedReason = decSkipped
	result.VersionCommit = commit

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	secretsResult, err := PushSecretsBundles(ctx, projectRoot, reporter)
	if err != nil {
		return nil, fmt.Errorf("push.secrets 失败: %w", err)
	}
	if secretsResult != nil {
		result.SecretsCreatedCount = secretsResult.CreatedCount
		result.SecretsUpdatedCount = secretsResult.UpdatedCount
		result.SecretsSkippedReason = secretsResult.SkippedReason
	}
	return result, nil
}

func pushDecBundles(ctx context.Context, projectRoot string, reporter Reporter) (pushedCount int, skippedReason, versionCommit string, err error) {
	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return 0, "", "", err
	}

	if len(projectConfig.EnabledBundles) == 0 {
		skippedReason = "无已启用 bundle"
		emit(reporter, EventInfo, "push.dec", "无已启用 bundle，跳过 Dec 推送", nil)
		return 0, skippedReason, "", nil
	}

	emit(reporter, EventInfo, "push.dec", "检查 .dec/cache/ 变更…", nil)

	err = withAppWriteRepo(func(tx *repo.Transaction) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		repoDir := tx.WorkDir()
		resolved, resolveErr := resolveDesiredAssets(projectConfig, repoDir, reporter)
		if resolveErr != nil {
			return resolveErr
		}

		assets := resolved.Assets
		if len(assets) == 0 && len(projectConfig.EnabledBundles) == 0 {
			skippedReason = "没有可推送的有效资产"
			emit(reporter, EventInfo, "push.dec", skippedReason, nil)
			return nil
		}

		if len(assets) > 0 {
			emit(reporter, EventInfo, "push.dec", fmt.Sprintf("同步 %d 个 Dec 资产", len(assets)), &Progress{Phase: "dec", Current: 0, Total: len(assets)})
		}

		synced, pruned, syncErr := syncDecVaultFromCache(projectRoot, repoDir, projectConfig, resolved, reporter)
		if syncErr != nil {
			return syncErr
		}

		for _, bundleName := range projectConfig.EnabledBundles {
			if err := ctx.Err(); err != nil {
				return err
			}
			ok, pushErr := pushBundleYAMLFromCache(projectRoot, repoDir, bundleName, reporter)
			if pushErr != nil {
				return pushErr
			}
			if ok {
				synced++
			}
		}

		git := repo.NewGitOps(repoDir)
		clean, cleanErr := git.IsClean()
		if cleanErr != nil {
			return cleanErr
		}
		if clean {
			skippedReason = "无本地变更"
			emit(reporter, EventInfo, "push.dec", "无本地变更，跳过 Dec 推送", nil)
			return nil
		}

		commitMsg := fmt.Sprintf("push: 更新 %d 项", synced)
		committed, commitErr := tx.CommitAndPush(commitMsg)
		if commitErr != nil {
			return commitErr
		}
		if !committed {
			skippedReason = "无本地变更"
			emit(reporter, EventInfo, "push.dec", "无本地变更，跳过 Dec 推送", nil)
			return nil
		}
		pushedCount = synced + pruned
		versionCommit = tx.CommitHash()
		if pruned > 0 {
			emit(reporter, EventInfo, "push.dec", fmt.Sprintf("Dec 推送完成：%d 项更新 · %d 项删除", synced, pruned), &Progress{Phase: "done", Current: pushedCount, Total: pushedCount})
		} else {
			emit(reporter, EventInfo, "push.dec", fmt.Sprintf("Dec 推送完成：%d 项", synced), &Progress{Phase: "done", Current: synced, Total: synced})
		}
		return nil
	})
	if err != nil {
		return 0, "", "", err
	}
	return pushedCount, skippedReason, versionCommit, nil
}

func pushBundleYAMLFromCache(projectRoot, repoDir, bundleName string, reporter Reporter) (bool, error) {
	cacheDir := filepath.Join(projectRoot, ".dec", "cache", bundleName)
	var cachePath string
	for _, name := range []string{"bundle.yaml", "bundle.yml"} {
		candidate := filepath.Join(cacheDir, name)
		if _, err := os.Stat(candidate); err == nil {
			cachePath = candidate
			break
		}
	}
	if cachePath == "" {
		return false, nil
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		emit(reporter, EventWarn, "push.dec", fmt.Sprintf("⚠️  读取 bundle %s 声明失败: %v", bundleName, err), nil)
		return false, nil
	}
	if _, err := bundle.Validate(data, cachePath); err != nil {
		emit(reporter, EventWarn, "push.dec", fmt.Sprintf("⚠️  bundle %s 校验失败: %v", bundleName, err), nil)
		return false, nil
	}

	destPath := filepath.Join(repoDir, types.VaultBundlesDir, bundleName, "bundle.yaml")
	if err := copyFile(cachePath, destPath); err != nil {
		return false, fmt.Errorf("推送 bundle %s 声明失败: %w", bundleName, err)
	}
	emit(reporter, EventInfo, "push.dec", fmt.Sprintf("  [bundle] %s", bundleName), nil)
	return true, nil
}
