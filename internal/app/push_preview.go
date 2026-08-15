package app

import (
	"context"

	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
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
	return PreviewPushWorkspaceAssets(NewWorkspace(WorkspaceProject, projectRoot))
}

// PreviewPushWorkspaceAssets 按平面预览 Push 影响面。
func PreviewPushWorkspaceAssets(workspace Workspace) (*PushProjectAssetsPreview, error) {
	preview := &PushProjectAssetsPreview{}

	projectConfig, err := loadWorkspaceBundleConfig(workspace)
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
	plan, err := planWorkspaceSecretsSync(workspace, preview.EnabledBundleNames, cfg)
	if err != nil {
		return nil, err
	}
	preview.SecretsTargetCount = plan.Total
	for _, target := range plan.Targets {
		if target.Kind == secrets.SyncKindProject {
			preview.ProjectSecretsName = target.Folder
			break
		}
	}

	candidate, hasChanges, skipped, decErr := previewDecPushChanges(context.Background(), workspace, projectConfig, nil)
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

func previewDecPushChanges(ctx context.Context, workspace Workspace, projectConfig *types.ProjectConfig, reporter Reporter) (candidateCount int, hasChanges bool, skippedReason string, err error) {
	if len(projectConfig.EnabledBundles) == 0 {
		return 0, false, "无已启用 bundle", nil
	}

	err = withAppWriteRepo(func(tx *repo.Transaction) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		repoDir := tx.WorkDir()
		resolved, resolveErr := resolveDesiredAssetsForPlane(projectConfig, repoDir, workspace.EffectivePlane(), reporter)
		if resolveErr != nil {
			return resolveErr
		}

		assets := resolved.Assets
		if len(assets) == 0 && len(projectConfig.EnabledBundles) == 0 {
			skippedReason = "没有可推送的有效资产"
			return nil
		}

		synced, pruned, syncErr := syncDecVaultFromCache(workspace, repoDir, projectConfig, resolved, reporter)
		if syncErr != nil {
			return syncErr
		}

		for _, bundleName := range projectConfig.EnabledBundles {
			if err := ctx.Err(); err != nil {
				return err
			}
			ok, pushErr := pushBundleYAMLFromCache(workspace, repoDir, bundleName, reporter)
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
