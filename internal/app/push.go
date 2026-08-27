package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

type PushProjectAssetsResult struct {
	DecPushedCount       int
	DecSkippedReason     string
	VersionCommit        string
	SecretsCreatedCount  int
	SecretsUpdatedCount  int
	SecretsSkippedReason string
	Model                string
	HomeProject          string
	WritableProjects     []string
}

// PushProjectAssets 将本地 .dec/cache/ 与 secrets 落地文件推送到远端（Dec Git vault + Bitwarden）。
func PushProjectAssets(ctx context.Context, projectRoot string, reporter Reporter) (*PushProjectAssetsResult, error) {
	return PushWorkspaceAssets(ctx, NewWorkspace(WorkspaceProject, projectRoot), reporter)
}

// PushWorkspaceAssets 把当前平面的本地缓存与 secrets 落地文件推回远端。
// 用户平面读 ~/.dec/cache 与 ~/.dec/secrets，只涉及 scope: user 的 bundle。
func PushWorkspaceAssets(ctx context.Context, workspace Workspace, reporter Reporter) (*PushProjectAssetsResult, error) {
	reporter = defaultReporter(reporter)
	result := &PushProjectAssetsResult{}
	if usesP, _ := connectedRepositoryUsesPModel(); usesP {
		result.Model = "p"
		if cfg, loadErr := loadWorkspaceBundleConfig(workspace); loadErr == nil {
			if workspace.EffectivePlane() == WorkspaceProject {
				result.HomeProject = strings.TrimSpace(cfg.ProjectName)
				if result.HomeProject != "" {
					result.WritableProjects = []string{result.HomeProject}
				}
			} else {
				result.WritableProjects = append([]string(nil), cfg.EnabledBundles...)
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	decPushed, decSkipped, commit, err := pushDecBundles(ctx, workspace, reporter)
	if err != nil {
		return nil, fmt.Errorf("push.dec 失败: %w", err)
	}
	result.DecPushedCount = decPushed
	result.DecSkippedReason = decSkipped
	result.VersionCommit = commit

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if result.Model == "p" && workspace.EffectivePlane() == WorkspaceProject &&
		result.HomeProject != "" && !connectedPExists(result.HomeProject) {
		result.SecretsSkippedReason = fmt.Sprintf("家 P %q 已不存在，跳过 private/project 推送", result.HomeProject)
		emit(reporter, EventWarn, "push.secrets", result.SecretsSkippedReason, nil)
		return result, nil
	}

	secretsResult, err := PushWorkspaceSecretsBundles(ctx, workspace, reporter)
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

func connectedPExists(name string) bool {
	exists := false
	_ = withAppReadRepo(func(tx *repo.Transaction) error {
		projects, err := pmodel.Scan(tx.WorkDir())
		if err != nil {
			return err
		}
		_, exists = projects[name]
		return nil
	})
	return exists
}

func connectedRepositoryUsesPModel() (bool, error) {
	tx, err := repo.NewLocalReadTransaction()
	if err != nil {
		return false, err
	}
	defer tx.Close()
	return repositoryUsesPModel(tx.WorkDir()), nil
}

func pushDecBundles(ctx context.Context, workspace Workspace, reporter Reporter) (pushedCount int, skippedReason, versionCommit string, err error) {
	projectConfig, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return 0, "", "", err
	}

	if len(projectConfig.EnabledBundles) == 0 && strings.TrimSpace(projectConfig.ProjectName) == "" {
		skippedReason = "无已启用 bundle"
		emit(reporter, EventInfo, "push.dec", "无已启用 bundle，跳过 Dec 推送", nil)
		return 0, skippedReason, "", nil
	}

	emit(reporter, EventInfo, "push.dec", fmt.Sprintf("检查 %s 变更…", displayCacheDir(workspace)), nil)

	err = withAppWriteRepo(func(tx *repo.Transaction) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		repoDir := tx.WorkDir()
		if repositoryHasLegacyLayout(repoDir) {
			return fmt.Errorf("检测到旧 projects/ 或 bundles/ 结构，Push 已拒绝；远端尚未完成一次性 P 迁移")
		}
		resolved, resolveErr := resolveDesiredAssetsForPlane(projectConfig, repoDir, workspace.EffectivePlane(), reporter)
		if resolveErr != nil {
			return resolveErr
		}

		assets := writableResolvedAssets(workspace, projectConfig, resolved.Assets)
		resolvedForPush := *resolved
		resolvedForPush.Assets = assets
		if len(assets) == 0 && len(projectConfig.EnabledBundles) == 0 {
			skippedReason = "没有可推送的有效资产"
			emit(reporter, EventInfo, "push.dec", skippedReason, nil)
			return nil
		}

		if len(assets) > 0 {
			emit(reporter, EventInfo, "push.dec", fmt.Sprintf("同步 %d 个 Dec 资产", len(assets)), &Progress{Phase: "dec", Current: 0, Total: len(assets)})
		}

		synced, pruned, syncErr := syncDecVaultFromCache(workspace, repoDir, projectConfig, &resolvedForPush, reporter)
		if syncErr != nil {
			return syncErr
		}

		if !hasPAssets(resolved.Assets) {
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

func writableResolvedAssets(workspace Workspace, cfg *types.ProjectConfig, assets []types.TypedAssetRef) []types.TypedAssetRef {
	if workspace.EffectivePlane() == WorkspaceUser || cfg == nil || strings.TrimSpace(cfg.ProjectName) == "" {
		return assets
	}
	out := make([]types.TypedAssetRef, 0, len(assets))
	for _, asset := range assets {
		if asset.Visibility != "" && asset.Vault != cfg.ProjectName {
			continue
		}
		out = append(out, asset)
	}
	return out
}

func pushBundleYAMLFromCache(workspace Workspace, repoDir, bundleName string, reporter Reporter) (bool, error) {
	cacheDir := filepath.Join(workspaceCacheDir(workspace), bundleName)
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
