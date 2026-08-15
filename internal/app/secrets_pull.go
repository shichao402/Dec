package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
)

type secretsSyncPlan struct {
	Targets []secrets.SyncTarget
	Total   int
}

func planSecretsSync(projectRoot string, enabledBundles []string, cfg *secrets.Config) (*secretsSyncPlan, error) {
	return planWorkspaceSecretsSync(NewWorkspace(WorkspaceProject, projectRoot), enabledBundles, cfg)
}

func planWorkspaceSecretsSync(workspace Workspace, enabledBundles []string, cfg *secrets.Config) (*secretsSyncPlan, error) {
	projectRoot := workspace.Root
	projectName := ""
	if workspace.EffectivePlane() == WorkspaceProject {
		mgr := config.NewProjectConfigManager(projectRoot)
		projectConfig, err := mgr.LoadProjectConfig()
		if err != nil {
			return nil, err
		}
		projectName, _ = ResolveProjectName(projectRoot, projectConfig)
	}
	if cfg == nil {
		cfg = &secrets.Config{}
	}
	// 平面隔离（ADR 0009）：project 上下文只解析项目平面 target。
	targets, err := cfg.ResolveSyncTargets(workspace.SecretsPlane(), enabledBundles, projectName)
	if err != nil {
		return nil, err
	}
	return &secretsSyncPlan{Targets: targets, Total: len(targets)}, nil
}

// secretsClientFactory 供测试注入 stub Client。
var secretsClientFactory = secrets.DefaultClient

type secretsPullSummary struct {
	SkippedReason string
	NoteCount     int
	SSHKeyCount   int
	HandlerCount  int
	HandlerNames  []string
	LandingPaths  []string
	SSHKeyNames   []string
}

func warnUnignoredSecrets(projectRoot string, landingPaths []string, reporter Reporter) {
	var projectPaths []string
	for _, p := range landingPaths {
		slash := filepath.ToSlash(p)
		if strings.HasPrefix(slash, secrets.SecretsRootDir+"/") || slash == secrets.SecretsRootDir {
			projectPaths = append(projectPaths, slash)
		}
	}
	unignored := secrets.UnignoredLandingPaths(projectRoot, projectPaths)
	if len(unignored) == 0 {
		return
	}
	emit(reporter, EventInfo, "pull.secrets",
		fmt.Sprintf("⚠️  %d 个密文件未被 .gitignore 忽略，建议在 .gitignore 中加入：", len(unignored)), nil)
	for _, rel := range unignored {
		emit(reporter, EventInfo, "pull.secrets", "  /"+rel, nil)
	}
}

// pullEnabledSecretsBundles 拉取全部 SyncTarget。
// 不做「停用即清理」：删除走 Remote 页显式确认。
func pullEnabledSecretsBundles(ctx context.Context, projectRoot string, enabledBundles []string, reporter Reporter) (*secretsPullSummary, error) {
	return pullEnabledSecretsBundlesForWorkspace(ctx, NewWorkspace(WorkspaceProject, projectRoot), enabledBundles, reporter)
}

func pullEnabledSecretsBundlesForWorkspace(ctx context.Context, workspace Workspace, enabledBundles []string, reporter Reporter) (*secretsPullSummary, error) {
	reporter = defaultReporter(reporter)
	projectRoot := workspace.Root
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

	cfg, err := loadSecretsConfigForPull()
	if err != nil {
		return nil, err
	}
	plan, err := planWorkspaceSecretsSync(workspace, enabledBundles, cfg)
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
	if names, listErr := client.ListSecretBundleNames(ctx); listErr != nil {
		emit(reporter, EventWarn, "pull.secrets",
			fmt.Sprintf("枚举 Bitwarden secret bundles 失败（不影响本次 pull）: %v", listErr), nil)
	} else if err := secrets.RememberSecretBundles(names); err != nil {
		emit(reporter, EventWarn, "pull.secrets",
			fmt.Sprintf("写入 known_secret_bundles 失败: %v", err), nil)
	}
	total := plan.Total
	emit(reporter, EventInfo, "pull.secrets", fmt.Sprintf("同步 %d 个 secrets 目标（bundle + project）", total), &Progress{Phase: "secrets", Current: 0, Total: total})

	fetchedNotes := make([][]secrets.SecureNote, len(plan.Targets))
	fetchedKeys := make([][]secrets.SSHKeyLanding, len(plan.Targets))
	var candidates []secrets.LandingCandidate
	seenSSHFiles := make(map[string]string)
	seenSSHHosts := make(map[string]string)

	for i, target := range plan.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress := &Progress{Phase: "secrets", Current: i + 1, Total: total}
		label := formatSyncTargetLabel(target)
		emit(reporter, EventInfo, "pull.secrets",
			fmt.Sprintf("拉取 %s (folder: %s → %s)", label, target.Folder, target.LocalRoot), progress)

		notes, keys, pullErr := secrets.ResolveBundle(ctx, client, secrets.PullBundleRequest{
			ProjectRoot:   projectRoot,
			Target:        target,
			DecBundleName: decBundleNameForTarget(target),
			Binding: secrets.BundleBinding{
				DecBundleName:     decBundleNameForTarget(target),
				SecretsBundleName: target.Folder,
			},
		})
		if pullErr != nil {
			return nil, fmt.Errorf("拉取 %s 失败: %w", label, pullErr)
		}
		fetchedNotes[i] = notes
		for _, note := range notes {
			candidates = append(candidates, secrets.LandingCandidate{
				Folder:       target.Folder,
				LocalRoot:    target.LocalRoot,
				RelativePath: note.RelativePath,
				Plane:        target.Plane,
			})
		}

		owner := target.Name
		if target.Kind == secrets.SyncKindProject {
			owner = "project"
		}
		landings, prepErr := secrets.PrepareSSHKeyLandings(owner, keys)
		if prepErr != nil {
			return nil, fmt.Errorf("校验 %s 的 SSH Key 失败: %w", label, prepErr)
		}
		for _, landing := range landings {
			base := filepath.Base(landing.PrivatePath)
			if prev, ok := seenSSHFiles[base]; ok {
				return nil, fmt.Errorf("SSH Key 文件名冲突: %s 同时由 %s 与 %s 产生", base, prev, label)
			}
			seenSSHFiles[base] = label
			for _, host := range landing.Hosts {
				if prev, ok := seenSSHHosts[host]; ok {
					return nil, fmt.Errorf("SSH host %q 冲突: 同时由 %s 与 %s 声明", host, prev, label)
				}
				seenSSHHosts[host] = label
			}
		}
		fetchedKeys[i] = landings

		// 一旦该 SyncTarget 上有密钥内容，记住 bundle 逻辑名供 Settings 候选。
		if target.Kind == secrets.SyncKindBundle && (len(notes) > 0 || len(keys) > 0) {
			if err := secrets.RememberSecretBundles([]string{target.Name}); err != nil {
				emit(reporter, EventWarn, "pull.secrets",
					fmt.Sprintf("写入 known_secret_bundles 失败: %v", err), nil)
			}
		}
	}

	emit(reporter, EventInfo, "pull.secrets", "校验落地路径边界与跨 folder 冲突", nil)
	if err := secrets.ValidateLandingPaths(projectRoot, candidates); err != nil {
		emit(reporter, EventError, "pull.secrets", err.Error(), nil)
		return nil, err
	}

	for i, target := range plan.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress := &Progress{Phase: "secrets", Current: i + 1, Total: total}
		label := formatSyncTargetLabel(target)
		paths, writeErr := secrets.WriteSecureNotes(projectRoot, target, fetchedNotes[i])
		if writeErr != nil {
			return nil, fmt.Errorf("落地 %s 失败: %w", label, writeErr)
		}
		summary.NoteCount += len(paths)
		summary.LandingPaths = append(summary.LandingPaths, paths...)
		if len(paths) > 0 {
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  落地 %d 个 Secure Note → %s: %s", len(paths), target.LocalRoot, strings.Join(noteRels(fetchedNotes[i]), ", ")), progress)
		}

		handlerItems := make([]handler.Item, 0, len(fetchedNotes[i]))
		for _, note := range fetchedNotes[i] {
			handlerItems = append(handlerItems, handler.Item{
				Source:      handler.SourceNote,
				Name:        note.RelativePath,
				NoteContent: note.Content,
				ProjectRoot: projectRoot,
				BundleName:  target.Name,
			})
		}
		applied, applyErr := handler.ApplyNotes(ctx, handler.Default(), handlerItems)
		if applyErr != nil {
			return nil, fmt.Errorf("执行 %s 的 secrets handler 失败: %w", label, applyErr)
		}
		if len(applied) > 0 {
			summary.HandlerCount += len(applied)
			summary.HandlerNames = append(summary.HandlerNames, applied...)
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  机器平面 handler %d 个: %s", len(applied), strings.Join(applied, ", ")), progress)
		}

		if len(fetchedKeys[i]) > 0 {
			if writeErr := secrets.WriteSSHKeyLandings(fetchedKeys[i]); writeErr != nil {
				return nil, fmt.Errorf("落地 %s 的 SSH Key 失败: %w", label, writeErr)
			}
			for _, landing := range fetchedKeys[i] {
				summary.SSHKeyCount++
				summary.SSHKeyNames = append(summary.SSHKeyNames, landing.Name)
			}
			names := make([]string, 0, len(fetchedKeys[i]))
			for _, landing := range fetchedKeys[i] {
				names = append(names, landing.Name)
			}
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  落地 %d 个 SSH Key: %s（未隐式删除本地/远端多余项）", len(names), strings.Join(names, ", ")), progress)
		}

		if len(paths) == 0 && len(fetchedKeys[i]) == 0 && target.Folder != "" {
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  Bitwarden folder %q 无 Secure Note / SSH Key 或 folder 不存在，跳过", target.Folder), progress)
		}
	}

	if workspace.EffectivePlane() == WorkspaceProject {
		warnUnignoredSecrets(projectRoot, summary.LandingPaths, reporter)
	}

	if summary.NoteCount == 0 && summary.SSHKeyCount == 0 && summary.HandlerCount == 0 {
		emit(reporter, EventInfo, "pull.secrets", "secrets 同步完成（无变更；未删除多余项）", &Progress{Phase: "secrets", Current: total, Total: total})
	} else {
		msg := fmt.Sprintf("secrets 同步完成：%d 个文件 · %d 个 SSH Key", summary.NoteCount, summary.SSHKeyCount)
		if summary.HandlerCount > 0 {
			msg += fmt.Sprintf(" · %d 个 handler", summary.HandlerCount)
		}
		msg += "（未删除多余项）"
		emit(reporter, EventInfo, "pull.secrets", msg,
			&Progress{Phase: "secrets", Current: total, Total: total})
	}
	return summary, nil
}

func formatSyncTargetLabel(t secrets.SyncTarget) string {
	switch {
	case t.Kind == secrets.SyncKindProject:
		return fmt.Sprintf("project secrets %q", t.Name)
	case secrets.IsMachinePlane(t.Plane):
		return fmt.Sprintf("machine secrets bundle %q", t.Name)
	default:
		return fmt.Sprintf("secrets bundle %q", t.Name)
	}
}

func decBundleNameForTarget(t secrets.SyncTarget) string {
	if t.Kind == secrets.SyncKindProject {
		return secrets.ProjectSecretsDecBundleName
	}
	return t.Name
}

func noteRels(notes []secrets.SecureNote) []string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.RelativePath)
	}
	return out
}

func loadSecretsConfigForPull() (*secrets.Config, error) {
	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		return &secrets.Config{}, nil
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &secrets.Config{}, nil
	}
	return cfg, nil
}
