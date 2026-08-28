package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/secrets"
)

type PushSecretsResult struct {
	SkippedReason string
	CreatedCount  int
	UpdatedCount  int
	PushedPaths   []string
	MissingLocal  []string
}

func PushSecretsBundles(ctx context.Context, projectRoot string, reporter Reporter) (*PushSecretsResult, error) {
	return PushWorkspaceSecretsBundles(ctx, NewWorkspace(WorkspaceProject, projectRoot), reporter)
}

// PushWorkspaceSecretsBundles 把当前平面的 secrets 落地文件推回 Bitwarden。
// 用户平面扫描 ~/.dec/secrets/bundles/<name>/，不触碰项目内 .secrets。
func PushWorkspaceSecretsBundles(ctx context.Context, workspace Workspace, reporter Reporter) (*PushSecretsResult, error) {
	reporter = defaultReporter(reporter)
	result := &PushSecretsResult{}
	projectRoot := workspace.Root

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

	projectConfig, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return nil, err
	}
	enabledBundles := append([]string(nil), projectConfig.EnabledBundles...)

	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}

	plan, err := planWorkspaceSecretsSync(workspace, enabledBundles, cfg)
	if err != nil {
		return nil, err
	}
	if plan.Total == 0 {
		result.SkippedReason = "无已启用 bundle"
		emit(reporter, EventInfo, "push.secrets", "无已启用 bundle，跳过 secrets 推送", nil)
		return result, nil
	}

	syncRootLabel := ".secrets"
	if workspace.EffectivePlane() == WorkspaceUser {
		syncRootLabel = "~/.dec/secrets"
	}

	if !secrets.HasSession() {
		if err := ensureBitwardenSession(ctx, reporter, "push.secrets"); err != nil {
			return nil, err
		}
	}

	client := secretsClientFactory()
	total := plan.Total
	emit(reporter, EventInfo, "push.secrets", fmt.Sprintf("推送 %d 个 secrets 目标（扫描 %s 同步根）", total, syncRootLabel), &Progress{Phase: "secrets", Current: 0, Total: total})

	for i, target := range plan.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress := &Progress{Phase: "secrets", Current: i + 1, Total: total}
		label := formatSyncTargetLabel(target)
		emit(reporter, EventInfo, "push.secrets",
			fmt.Sprintf("推送 %s (folder: %s ← %s)", label, target.Address, target.LocalRoot), progress)

		localNotes, scanErr := secrets.ScanSyncRoot(projectRoot, target)
		if scanErr != nil {
			return nil, scanErr
		}
		if overlapErr := validateNoPPrivateGitOverlap(
			[]secrets.SyncTarget{target},
			[][]secrets.SecureNote{localNotes},
			nil,
		); overlapErr != nil {
			return nil, overlapErr
		}
		pushResult, pushErr := secrets.PushBundle(ctx, client, secrets.PushBundleRequest{
			ProjectRoot: projectRoot,
			Target:      target,
		})
		if pushErr != nil {
			return nil, fmt.Errorf("推送 %s 失败: %w", label, pushErr)
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
				fmt.Sprintf("  %s 无可推送内容（同步根为空或无变更）", label), progress)
		}
		if len(pushResult.MissingLocal) > 0 {
			emit(reporter, EventWarn, "push.secrets",
				fmt.Sprintf("  ⚠️  %d 个 Note 在本地找不到对应文件，已跳过（不会删除远端）: %s",
					len(pushResult.MissingLocal), strings.Join(pushResult.MissingLocal, ", ")), progress)
			emit(reporter, EventInfo, "push.secrets", "  确实要删除请到 Remote 页逐条确认", progress)
		}
	}

	totalPushed := result.CreatedCount + result.UpdatedCount
	if totalPushed == 0 {
		emit(reporter, EventInfo, "push.secrets", "secrets 推送完成（无变更；未删除多余项）", &Progress{Phase: "done", Current: total, Total: total})
	} else {
		emit(reporter, EventInfo, "push.secrets",
			fmt.Sprintf("secrets 推送完成：新建 %d · 更新 %d（未删除多余项）", result.CreatedCount, result.UpdatedCount),
			&Progress{Phase: "done", Current: total, Total: total})
	}
	return result, nil
}
