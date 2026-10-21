package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/prune"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
)

// OrphanCleanupReport 汇总 pull 时确认「远端已无」后的本地孤儿清理结果。
type OrphanCleanupReport struct {
	RemovedSecretPaths []string
	RemovedSSHKeys     []string
	ClearedBundles     []string // vault 已无、本轮从 enabled/known 摘掉的包
	ReportedOnly       []string // 无法确认远端时只报告不删
}

func (r *OrphanCleanupReport) merge(other OrphanCleanupReport) {
	r.RemovedSecretPaths = append(r.RemovedSecretPaths, other.RemovedSecretPaths...)
	r.RemovedSSHKeys = append(r.RemovedSSHKeys, other.RemovedSSHKeys...)
	r.ClearedBundles = append(r.ClearedBundles, other.ClearedBundles...)
	r.ReportedOnly = append(r.ReportedOnly, other.ReportedOnly...)
}

func (r OrphanCleanupReport) totalRemoved() int {
	return len(r.RemovedSecretPaths) + len(r.RemovedSSHKeys) + len(r.ClearedBundles)
}

// pruneOrphanSecretsForTarget 在一次成功的 Bitwarden PullBundle 之后，
// 以远端 Note/SSH 列表为权威，把本 SyncTarget 下多余的本地落地移入隔离区。
//
// 安全边界：
//   - 判定「可移走」要求路径形态属于远端派生类型且本机有派生登记，杜绝误删手工内容
//   - 只处理 AbsolutePath(LocalRoot, noteRel) 落点（ScanSyncRoot 已约束在同步根内）
//   - SSH 只处理 ~/.ssh/dec_<owner>_* 命名约定的 Dec 托管密钥
//   - 调用方必须已成功取回远端列表；取回失败时不得调用
//   - 隔离区不可用时中止剪枝并显式报错，绝不退化为永久删除
func pruneOrphanSecretsForTarget(
	ctx context.Context,
	projectRoot string,
	target secrets.SyncTarget,
	remoteNotes []secrets.SecureNote,
	remoteKeys []secrets.SSHKeyLanding,
	reporter Reporter,
	strict bool,
) OrphanCleanupReport {
	reporter = defaultReporter(reporter)
	executor, execErr := DefaultPruneExecutor()
	if execErr != nil {
		emit(reporter, EventError, "pull.prune",
			fmt.Sprintf("无法进入剪枝流程（隔离区不可用），已中止以保护数据: %v", execErr), nil)
		return OrphanCleanupReport{}
	}
	report := OrphanCleanupReport{}

	decision := DecideTargetPrune(projectRoot, target, remoteNotes, remoteKeys, PruneScope{Strict: strict})
	if len(decision.Warnings) > 0 {
		for _, w := range decision.Warnings {
			report.ReportedOnly = append(report.ReportedOnly, w)
			emit(reporter, EventWarn, "pull.reconcile", w, nil)
		}
	}
	// 仍存在的 handler 惰性条目：先 revoke，再进隔离区。
	for _, item := range decision.Removable() {
		if handler.Default().Find(handler.SourceNote, item.RelPath) != nil {
			if !revokeDerivedHandler(ctx, projectRoot, target, item, reporter) {
				report.ReportedOnly = append(report.ReportedOnly, item.DisplayPath+"（revoke 失败）")
				continue
			}
		}
	}

	result := executor.Execute(ctx, decision.Removable(), reporter)
	report.RemovedSecretPaths = append(report.RemovedSecretPaths, result.RemovedSecretPaths...)
	report.RemovedSSHKeys = append(report.RemovedSSHKeys, result.RemovedSSHKeys...)
	report.ReportedOnly = append(report.ReportedOnly, result.Warnings...)
	for _, w := range result.Warnings {
		emit(reporter, EventWarn, "pull.prune", w, nil)
	}

	if len(result.RemovedSecretPaths) > 0 {
		_ = removeEmptySecretDirs(projectRoot, target)
	}

	sort.Strings(report.RemovedSecretPaths)
	sort.Strings(report.RemovedSSHKeys)
	return report
}

// revokeDerivedHandler 撤销一条惰性 handler 条目的本地落地（数据库/环境注入）。
// 返回是否成功撤销。
func revokeDerivedHandler(ctx context.Context, projectRoot string, target secrets.SyncTarget, item PruneItem, reporter Reporter) bool {
	content, readErr := os.ReadFile(item.AbsPath)
	if readErr != nil {
		emit(reporter, EventWarn, "pull.prune",
			fmt.Sprintf("读取 revoke 正文失败 %s: %v", item.DisplayPath, readErr), nil)
		return false
	}
	if _, revokeErr := handler.RevokeNotes(ctx, nil, []handler.Item{{
		Source:        handler.SourceNote,
		Name:          item.RelPath,
		NoteContent:   string(content),
		ProjectRoot:   item.ProjectRoot,
		BundleName:    item.Bundle,
		ProjectScoped: item.Plane != string(secrets.SyncPlaneMachine),
	}}); revokeErr != nil {
		emit(reporter, EventWarn, "pull.prune",
			fmt.Sprintf("撤销孤儿 handler %s 失败: %v", item.DisplayPath, revokeErr), nil)
		return false
	}
	return true
}

// secretsOwnerForTarget 返回 SSH Key 落地文件名里的归属段：项目名。
func secretsOwnerForTarget(target secrets.SyncTarget) string {
	return target.Name
}

// removeEmptySecretDirs 递归清空同步根下的空目录，只动子树内部。
func removeEmptySecretDirs(projectRoot string, target secrets.SyncTarget) error {
	rootAbs, err := secrets.ResolveAbsDir(projectRoot, target)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(rootAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) == 0 {
		return os.Remove(rootAbs)
	}
	// 自下而上清空目录（仅 LocalRoot 子树）。
	return filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || !d.IsDir() || path == rootAbs {
			return walkErr
		}
		_ = removeDirIfEmpty(path)
		return nil
	})
}

// reconcileMissingVaultBundles 处理「本次启用、但 Git vault 已无目录」的包。
// vault 缺失已确认 → 摘两平面 enabled + known；secrets/SSH 仅在 Bitwarden 本轮已确认空时删除，否则只报告。
func reconcileMissingVaultBundles(
	ctx context.Context,
	workspace Workspace,
	enabledBundles []string,
	vaultPresent map[string]struct{},
	secretsConfirmed map[string]bool, // bundle 名 → 本轮是否成功拉取并对照过远端
	reporter Reporter,
) OrphanCleanupReport {
	reporter = defaultReporter(reporter)
	report := OrphanCleanupReport{}

	for _, name := range configNormalize(enabledBundles) {
		if name == "" {
			continue
		}
		if _, ok := vaultPresent[name]; ok {
			continue
		}

		emit(reporter, EventWarn, "pull.reconcile",
			fmt.Sprintf("enabled bundle %q 在 vault 中已不存在，开始收敛本地登记", name), nil)

		if changed, err := removeWorkspaceEnabledBundle(workspace, name); err != nil {
			emit(reporter, EventWarn, "pull.reconcile", fmt.Sprintf("摘除当前平面 enabled 失败: %v", err), nil)
		} else if changed {
			report.ClearedBundles = append(report.ClearedBundles, name)
		}
		other := NewWorkspace(WorkspaceProject, workspace.Root)
		if workspace.EffectivePlane() == WorkspaceProject {
			other = NewWorkspace(WorkspaceUser, workspace.Root)
		}
		if changed, err := removeWorkspaceEnabledBundle(other, name); err != nil {
			emit(reporter, EventWarn, "pull.reconcile", fmt.Sprintf("摘除另一平面订阅失败: %v", err), nil)
		} else if changed {
			emit(reporter, EventInfo, "pull.reconcile",
				fmt.Sprintf("已从另一平面 requires 移除 %q", name), nil)
		}

		if err := secrets.ForgetSecretBundles([]string{name}); err != nil {
			emit(reporter, EventWarn, "pull.reconcile", fmt.Sprintf("清除 known_secret_bundles 失败: %v", err), nil)
		}

		if secretsConfirmed[name] {
			// 远端已对照过：整目录清扫走隔离区（幂等；单项孤儿可能已在 prune 阶段搬走）。
			executor, execErr := DefaultPruneExecutor()
			if execErr != nil {
				emit(reporter, EventError, "pull.reconcile",
					fmt.Sprintf("隔离区不可用，已中止清理 %q 以保护数据: %v", name, execErr), nil)
				for _, dir := range localSecretBundleDirsForPlane(workspace, name) {
					report.ReportedOnly = append(report.ReportedOnly, dir+"（隔离区不可用，未清理）")
				}
				continue
			}
			for _, dir := range localSecretBundleDirsForPlane(workspace, name) {
				info, statErr := os.Stat(dir)
				if statErr != nil || !info.IsDir() {
					continue
				}
				item := PruneItem{
					Kind:        "dir",
					AbsPath:     dir,
					DisplayPath: dir,
					RelPath:     filepath.Base(dir),
					Bundle:      name,
					Plane:       string(workspace.EffectivePlane()),
					ProjectRoot: workspace.Root,
					IsDir:       true,
					Class:       PruneClassRebuildable,
					Reason:      prune.ReasonDerivedBundle,
					Detail:      "vault+Bitwarden 均已确认无该包，本地目录为派生落地",
				}
				if workspace.EffectivePlane() == WorkspaceUser {
					item.ProjectRoot = ""
					item.DisplayPath = "~/.dec/secrets/" + filepath.Base(dir)
				}
				result := executor.Execute(ctx, []PruneItem{item}, reporter)
				if len(result.Warnings) > 0 {
					report.ReportedOnly = append(report.ReportedOnly, result.Warnings...)
					continue
				}
				report.RemovedSecretPaths = append(report.RemovedSecretPaths, result.RemovedSecretPaths...)
			}
			for _, keyName := range listLocalSSHKeyNamesForBundle(name) {
				// 逐条走隔离区，保持密钥对可恢复。
				item := PruneItem{
					Kind:        "ssh_key",
					RelPath:     keyName,
					DisplayPath: "~/.ssh/dec_" + name + "_" + keyName,
					Bundle:      name,
					Plane:       string(workspace.EffectivePlane()),
					ProjectRoot: workspace.Root,
					Class:       PruneClassRebuildable,
					Reason:      prune.ReasonSSHLanding,
					Detail:      "vault+Bitwarden 均已确认，本地为 Dec 托管落地",
				}
				if workspace.EffectivePlane() == WorkspaceUser {
					item.ProjectRoot = ""
					if priv, pErr := secrets.MachineSSHKeyPrivatePath(name, keyName); pErr == nil {
						item.AbsPath = priv
					}
				} else if priv, pErr := secrets.ProjectSSHKeyPrivatePath(workspace.Root, name, keyName); pErr == nil {
					item.AbsPath = priv
				}
				if item.AbsPath == "" {
					report.ReportedOnly = append(report.ReportedOnly, "~/.ssh/dec_"+name+"_"+keyName+"（路径解析失败）")
					continue
				}
				result := executor.Execute(ctx, []PruneItem{item}, reporter)
				if len(result.RemovedSSHKeys) == 0 {
					report.ReportedOnly = append(report.ReportedOnly, result.Warnings...)
					continue
				}
				report.RemovedSSHKeys = append(report.RemovedSSHKeys, result.RemovedSSHKeys...)
			}
			continue
		}

		// 未能确认 Bitwarden：只报告残留，不删敏感落地。
		for _, dir := range localSecretBundleDirsForPlane(workspace, name) {
			if _, err := os.Stat(dir); err != nil {
				continue
			}
			msg := dir + "（vault 已无，但未能确认 Bitwarden，未删除）"
			report.ReportedOnly = append(report.ReportedOnly, msg)
			emit(reporter, EventWarn, "pull.reconcile", "保留敏感落地: "+msg, nil)
		}
		for _, keyName := range listLocalSSHKeyNamesForBundle(name) {
			msg := "~/.ssh/dec_" + name + "_" + keyName + "（未能确认 Bitwarden，未删除）"
			report.ReportedOnly = append(report.ReportedOnly, msg)
			emit(reporter, EventWarn, "pull.reconcile", "保留 SSH 落地: "+msg, nil)
		}
	}

	sort.Strings(report.ClearedBundles)
	sort.Strings(report.RemovedSecretPaths)
	sort.Strings(report.RemovedSSHKeys)
	sort.Strings(report.ReportedOnly)
	return report
}

// localSecretBundleDirsForPlane 只返回当前平面的 secrets 同步根，遵守 ADR 0009。
func localSecretBundleDirsForPlane(workspace Workspace, bundleName string) []string {
	bundleName = strings.TrimSpace(bundleName)
	if bundleName == "" {
		return nil
	}
	if workspace.EffectivePlane() == WorkspaceUser {
		machineRoot, err := secrets.MachineSecretsRoot()
		if err != nil {
			return nil
		}
		return []string{filepath.Join(machineRoot, bundleName)}
	}
	projectRoot := strings.TrimSpace(workspace.Root)
	if projectRoot == "" {
		return nil
	}
	return []string{filepath.Join(projectRoot, secrets.SecretsRootDir, bundleName)}
}

func configNormalize(names []string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func emitOrphanCleanupSummary(report OrphanCleanupReport, reporter Reporter) {
	reporter = defaultReporter(reporter)
	if report.totalRemoved() == 0 && len(report.ReportedOnly) == 0 {
		return
	}
	n := len(report.RemovedSecretPaths) + len(report.RemovedSSHKeys)
	emit(reporter, EventInfo, "pull.reconcile",
		fmt.Sprintf("🧹 已清理 %d 项孤儿（secrets/SSH），登记收敛 %d 个缺失 vault 包",
			n, len(report.ClearedBundles)), nil)
	for _, p := range report.ReportedOnly {
		emit(reporter, EventWarn, "pull.reconcile", "未删除: "+p, nil)
	}
}
