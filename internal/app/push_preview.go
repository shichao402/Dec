package app

import (
	"context"
	"fmt"
	"strings"

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
	Model               string
	HomeProject         string
	WritableProjects    []string
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
	if usesP, _ := connectedRepositoryUsesPModel(); usesP {
		preview.Model = "p"
		if workspace.EffectivePlane() == WorkspaceProject {
			preview.HomeProject = projectConfig.ProjectName
			preview.WritableProjects = []string{projectConfig.ProjectName}
		} else {
			preview.WritableProjects = append([]string(nil), projectConfig.EnabledBundles...)
		}
	}

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
	if len(plan.Targets) > 0 {
		preview.ProjectSecretsName = plan.Targets[0].Address
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
	if len(projectConfig.EnabledBundles) == 0 &&
		(workspace.EffectivePlane() == WorkspaceUser || strings.TrimSpace(projectConfig.ProjectName) == "") {
		return 0, false, "无已启用 bundle", nil
	}

	err = withAppWriteRepo(func(tx *repo.Transaction) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		repoDir := tx.WorkDir()
		if repositoryHasLegacyLayout(repoDir) {
			return fmt.Errorf("检测到旧 projects/ 或 bundles/ 结构；远端尚未完成一次性 P 迁移")
		}
		resolved, resolveErr := resolveDesiredAssetsForPlane(projectConfig, repoDir, workspace.EffectivePlane(), reporter)
		if resolveErr != nil {
			return resolveErr
		}

		// 与真正 push 使用同一可写边界：项目平面只能回推家 P，
		// direct requires 的 public/project 副本只读，不能计入预览或被临时镜像。
		assets := writableResolvedAssets(workspace, projectConfig, resolved.Assets)
		resolvedForPreview := *resolved
		resolvedForPreview.Assets = assets
		if len(assets) == 0 && len(projectConfig.EnabledBundles) == 0 {
			skippedReason = "没有可推送的有效资产"
			return nil
		}

		synced, pruned, syncErr := syncDecVaultFromCache(workspace, repoDir, projectConfig, &resolvedForPreview, reporter)
		if syncErr != nil {
			return syncErr
		}

		if !hasPAssets(assets) {
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
