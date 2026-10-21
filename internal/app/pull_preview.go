package app

import (
	"context"
	"fmt"

	"github.com/shichao402/Dec/internal/secrets"
)

// PreviewPullProjectAssets 预览 project 平面 pull 将执行的剪枝（无副作用）。
func PreviewPullProjectAssets(projectRoot string) (*PullPreview, error) {
	return PreviewPullWorkspaceAssets(NewWorkspace(WorkspaceProject, projectRoot))
}

// PreviewPullWorkspaceAssets 预览给定工作区 pull 将执行的剪枝（无副作用）。
func PreviewPullWorkspaceAssets(workspace Workspace) (*PullPreview, error) {
	enabledBundles := consumedProjectNamesForWorkspace(workspace)
	ctx := context.Background()
	return previewPullForWorkspace(ctx, workspace, enabledBundles, nil)
}

// consumedProjectNamesForWorkspace 读取工作区配置的启用 bundle 名单。
func consumedProjectNamesForWorkspace(workspace Workspace) []string {
	projectConfig, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return nil
	}
	return consumedProjectNames(projectConfig, workspace.EffectivePlane())
}

// PullPreviewChange 描述一次 pull 预览里的一条「将执行」的剪枝明细。
type PullPreviewChange struct {
	Op          string `json:"op"`            // "quarantine"（移入隔离区）
	Path        string `json:"path"`           // 给人看的展示路径
	Target      string `json:"target"`         // 归属产品/目标
	Kind        string `json:"kind"`           // note / ssh_key / dir
	Reason      string `json:"reason"`         // 判定理由（可读）
	Recoverable bool   `json:"recoverable"`    // 是否可从远端重新拉回
	Plane       string `json:"plane,omitempty"` // 来源平面
}

// PullPreview 是一次 pull 预览的结果：准备删什么、保留什么，不产生任何写操作。
//
// 预览必须显式声明「无副作用」，并严格区分「判定为可删」与「真正执行」。
// 下游（Agent/Console）只据其结果确认，执行走真正的 pull 流程。
type PullPreview struct {
	// Removable 是判定为可移入隔离区的条目。
	Removable []PullPreviewChange `json:"removable"`
	// Kept 是判定为保留的条目及其理由（含本地独有、来源不明、无法确认远端等）。
	Kept []PullPreviewChange `json:"kept"`
	// Warnings 是扫描/拉取过程的告警。
	Warnings []string `json:"warnings"`
	// BitwardenConfigured 表示本机是否已配置 Bitwarden（影响拉取可用性）。
	BitwardenConfigured bool `json:"bitwarden_configured"`
	// SkippedReason 非空表示本次预览未完整执行（如未配置 Bitwarden）。
	SkippedReason string `json:"skipped_reason,omitempty"`
}

// HasRemovals 报告预览是否存在待执行条目。
func (p *PullPreview) HasRemovals() bool {
	return len(p.Removable) > 0
}

// previewPullForWorkspace 在给定工作区与启用 bundle 上计算 pull 预览。
//
// 为复用「远端权威判定」而不修订本地文件，本函数使用只读的远端列表快照：
// 直接调用 DecideTargetPrune（纯函数），不触发任何落盘写操作。
// 注意：调用方需自行保证「本机已配置 Bitwarden 且已成功拉取远端」——
// 若远端不可用，preview 无法给出可信的待删结论，应拒绝预览。
func previewPullForWorkspace(ctx context.Context, workspace Workspace, enabledBundles []string, reporter Reporter) (*PullPreview, error) {
	reporter = defaultReporter(reporter)
	preview := &PullPreview{}

	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, err
	}
	preview.BitwardenConfigured = configured
	if !configured {
		preview.SkippedReason = "本机未配置 Bitwarden，无法获得权威远端列表以判定可删条目"
		return preview, nil
	}

	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}
	plan, err := planWorkspaceSecretsSync(ctx, workspace, enabledBundles, cfg)
	if err != nil {
		return nil, err
	}
	if plan.Total == 0 {
		preview.SkippedReason = "无已启用 bundle"
		return preview, nil
	}

	for _, target := range plan.Targets {
		if err := ctx.Err(); err != nil {
			break
		}
		// 只读远端列表：不落盘、不修改本地。
		remoteNotes, remoteKeys, err := listRemoteTargetSecrets(ctx, workspace.Root, target)
		if err != nil {
			preview.Warnings = append(preview.Warnings,
				fmt.Sprintf("bundle %s 远端不可用（不列入可删判定）: %v", target.Name, err))
			preview.SkippedReason = "远端不可用，预览不完整"
			continue
		}
		decision := DecideTargetPrune(workspace.Root, target, remoteNotes, remoteKeys, PruneScope{})
		preview.Warnings = append(preview.Warnings, decision.Warnings...)

		for _, item := range decision.Removable() {
			preview.Removable = append(preview.Removable, item.toChange())
		}
		for _, item := range decision.Kept() {
			preview.Kept = append(preview.Kept, item.toChange())
		}
	}

	// 需求 1 验收 3：无待删时必须返回明确结论，而非空列表。
	// 这里的「无待删」可能是真无孤儿，也可能全部被判定为保留（kept 非空）。
	if !preview.HasRemovals() && preview.SkippedReason == "" {
		if len(preview.Warnings) > 0 {
			preview.SkippedReason = "存在扫描告警，未得出可信的待删结论"
		} else if len(preview.Kept) > 0 {
			preview.SkippedReason = fmt.Sprintf("无待删条目（保留 %d 项）", len(preview.Kept))
		} else {
			preview.SkippedReason = "无待删条目"
		}
	}
	return preview, nil
}

// listRemoteTargetSecrets 只读地列出远端目标下的 Note 与 SSH Key（不写本地）。
func listRemoteTargetSecrets(ctx context.Context, projectRoot string, target secrets.SyncTarget) ([]secrets.SecureNote, []secrets.SSHKeyLanding, error) {
	client := secretsClientFactory()
	if client == nil {
		return nil, nil, fmt.Errorf("secrets 客户端不可用")
	}
	remoteNotes, err := client.ListNotes(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	// RemoteNote.Name 即同步根相对路径（还有正文时 Content 待 pull 取回）。
	// 判定只看路径形态与远端存在性，Content 置空不影响。
	notes := make([]secrets.SecureNote, 0, len(remoteNotes))
	for _, rn := range remoteNotes {
		notes = append(notes, secrets.SecureNote{RelativePath: rn.Name})
	}

	remoteKeys, err := client.ListSSHKeys(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	keys := make([]secrets.SSHKeyLanding, 0, len(remoteKeys))
	for _, rk := range remoteKeys {
		keys = append(keys, secrets.SSHKeyLanding{Name: rk.Name})
	}
	return notes, keys, nil
}

func (item PruneItem) toChange() PullPreviewChange {
	return PullPreviewChange{
		Op:          "quarantine",
		Path:        item.DisplayPath,
		Target:      item.Bundle,
		Kind:        item.Kind,
		Reason:      item.Detail,
		Recoverable: item.RemoteRecoverable,
		Plane:       item.Plane,
	}
}
