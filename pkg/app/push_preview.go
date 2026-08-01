package app

import (
	"context"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
)

// PushProjectAssetsPreview 描述 Push 前的本地摘要（不访问 Bitwarden、不提交 Git）。
//
// 无 secrets 文件数：待推文件由远端 folder 的 note 列表决定，数不出来又不联网，
// 这里只报会涉及几个 folder。
type PushProjectAssetsPreview struct {
	EnabledBundleCount  int
	EnabledBundleNames  []string
	ProjectSecretsName  string
	SecretsTargetCount  int
	DecCandidateCount   int
	DecHasChanges       bool
	DecSkippedReason    string
	BitwardenConfigured bool
}

// PreviewPushProjectAssets 轻量检测 Push 将涉及的内容，供 TUI 确认页展示。
func PreviewPushProjectAssets(projectRoot string) (*PushProjectAssetsPreview, error) {
	preview := &PushProjectAssetsPreview{}

	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}

	preview.EnabledBundleNames = append([]string(nil), projectConfig.EnabledBundles...)
	preview.EnabledBundleCount = len(preview.EnabledBundleNames)

	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, err
	}
	preview.BitwardenConfigured = configured

	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}
	plan, err := planSecretsSync(projectRoot, preview.EnabledBundleNames, cfg)
	if err != nil {
		return nil, err
	}
	preview.SecretsTargetCount = plan.Total
	preview.ProjectSecretsName = plan.ProjectSecretsName

	candidate, hasChanges, skipped, decErr := previewDecPushChanges(context.Background(), projectRoot, projectConfig, nil)
	if decErr != nil {
		preview.DecSkippedReason = decErr.Error()
	} else {
		preview.DecCandidateCount = candidate
		preview.DecHasChanges = hasChanges
		if skipped != "" {
			preview.DecSkippedReason = skipped
		}
	}
	return preview, nil
}

func previewDecPushChanges(ctx context.Context, projectRoot string, projectConfig *types.ProjectConfig, reporter Reporter) (candidateCount int, hasChanges bool, skippedReason string, err error) {
	if len(projectConfig.EnabledBundles) == 0 {
		return 0, false, "无已启用 bundle", nil
	}

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
			return nil
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
			return nil
		}
		candidateCount = synced + pruned
		hasChanges = true
		return nil
	})
	return candidateCount, hasChanges, skippedReason, err
}
