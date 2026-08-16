package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/secrets"
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
// 以远端 Note/SSH 列表为权威，删除本 SyncTarget 下多余的本地落地。
//
// 安全边界：
//   - 只删 AbsolutePath(LocalRoot, noteRel) 落点（ScanSyncRoot 已约束在同步根内）
//   - SSH 只删 ~/.ssh/dec_<owner>_* 命名约定的 Dec 托管密钥
//   - 调用方必须已成功取回远端列表；取回失败时不得调用
func pruneOrphanSecretsForTarget(
	projectRoot string,
	target secrets.SyncTarget,
	remoteNotes []secrets.SecureNote,
	remoteKeys []secrets.SSHKeyLanding,
	reporter Reporter,
) OrphanCleanupReport {
	reporter = defaultReporter(reporter)
	report := OrphanCleanupReport{}

	keepNotes := make(map[string]struct{}, len(remoteNotes))
	for _, note := range remoteNotes {
		rel := strings.TrimSpace(note.RelativePath)
		if rel == "" {
			continue
		}
		keepNotes[rel] = struct{}{}
	}

	localNotes, scanErr := secrets.ScanSyncRoot(projectRoot, target)
	if scanErr != nil {
		emit(reporter, EventWarn, "pull.reconcile",
			fmt.Sprintf("扫描本地 %s 失败（跳过 secrets 孤儿清理）: %v", target.LocalRoot, scanErr), nil)
		return report
	}
	for _, note := range localNotes {
		if _, ok := keepNotes[note.RelativePath]; ok {
			continue
		}
		display, err := removeLocalSecureNoteFile(projectRoot, target, note.RelativePath)
		if err != nil {
			emit(reporter, EventWarn, "pull.reconcile",
				fmt.Sprintf("删除孤儿 Secure Note 失败 %s: %v", note.RelativePath, err), nil)
			report.ReportedOnly = append(report.ReportedOnly, note.RelativePath+"（删除失败）")
			continue
		}
		report.RemovedSecretPaths = append(report.RemovedSecretPaths, display)
		emit(reporter, EventInfo, "pull.reconcile",
			fmt.Sprintf("已清理孤儿 Secure Note %s（远端已无）", display), nil)
	}
	if len(report.RemovedSecretPaths) > 0 {
		_ = removeEmptySecretDirs(projectRoot, target)
	}

	owner := secretsOwnerForTarget(target)
	keepKeys := make(map[string]struct{}, len(remoteKeys))
	for _, key := range remoteKeys {
		name := strings.TrimSpace(key.Name)
		if name == "" {
			continue
		}
		keepKeys[name] = struct{}{}
	}
	for _, keyName := range listLocalSSHKeyNamesForBundle(owner) {
		if _, ok := keepKeys[keyName]; ok {
			continue
		}
		if err := secrets.RemoveSSHKeyLanding(owner, keyName); err != nil {
			emit(reporter, EventWarn, "pull.reconcile",
				fmt.Sprintf("删除孤儿 SSH Key %s 失败: %v", keyName, err), nil)
			report.ReportedOnly = append(report.ReportedOnly, "~/.ssh/dec_"+owner+"_"+keyName+"（删除失败）")
			continue
		}
		label := owner + "/" + keyName
		report.RemovedSSHKeys = append(report.RemovedSSHKeys, label)
		emit(reporter, EventInfo, "pull.reconcile",
			fmt.Sprintf("已清理孤儿 SSH Key %s（远端已无）", label), nil)
	}

	sort.Strings(report.RemovedSecretPaths)
	sort.Strings(report.RemovedSSHKeys)
	return report
}

func secretsOwnerForTarget(target secrets.SyncTarget) string {
	if target.Kind == secrets.SyncKindProject {
		return "project"
	}
	return target.Name
}

func removeLocalSecureNoteFile(projectRoot string, target secrets.SyncTarget, noteRel string) (string, error) {
	display, err := secrets.RootRelPath(target, noteRel)
	if err != nil {
		return "", err
	}
	abs, err := secrets.AbsolutePath(projectRoot, target, noteRel)
	if err != nil {
		return "", err
	}
	rootAbs, err := secrets.ResolveAbsDir(projectRoot, target)
	if err != nil {
		return "", err
	}
	cleanAbs := filepath.Clean(abs)
	cleanRoot := filepath.Clean(rootAbs)
	rel, relErr := filepath.Rel(cleanRoot, cleanAbs)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("拒绝删除同步根外路径 %s", display)
	}
	if err := os.Remove(cleanAbs); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return display, nil
}

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
			emit(reporter, EventWarn, "pull.reconcile", fmt.Sprintf("摘除另一平面 enabled 失败: %v", err), nil)
		} else if changed {
			emit(reporter, EventInfo, "pull.reconcile",
				fmt.Sprintf("已从另一平面 enabled_bundles 移除 %q", name), nil)
		}

		if err := secrets.ForgetSecretBundles([]string{name}); err != nil {
			emit(reporter, EventWarn, "pull.reconcile", fmt.Sprintf("清除 known_secret_bundles 失败: %v", err), nil)
		}

		if secretsConfirmed[name] {
			// 远端已对照过：整目录清扫（幂等；单项孤儿可能已在 prune 阶段删掉）。
			for _, dir := range localSecretBundleDirsForPlane(workspace, name) {
				if _, err := os.Stat(dir); err != nil {
					continue
				}
				if err := os.RemoveAll(dir); err != nil {
					emit(reporter, EventWarn, "pull.reconcile",
						fmt.Sprintf("删除本地 secrets 目录失败 %s: %v", dir, err), nil)
					report.ReportedOnly = append(report.ReportedOnly, dir)
					continue
				}
				report.RemovedSecretPaths = append(report.RemovedSecretPaths, dir)
				emit(reporter, EventInfo, "pull.reconcile",
					fmt.Sprintf("已删本地 secrets 同步根 %s（vault+Bitwarden 均已确认）", dir), nil)
			}
			for _, keyName := range listLocalSSHKeyNamesForBundle(name) {
				if err := secrets.RemoveSSHKeyLanding(name, keyName); err != nil {
					emit(reporter, EventWarn, "pull.reconcile",
						fmt.Sprintf("清理 SSH Key %s 失败: %v", keyName, err), nil)
					report.ReportedOnly = append(report.ReportedOnly, "~/.ssh/dec_"+name+"_"+keyName)
					continue
				}
				report.RemovedSSHKeys = append(report.RemovedSSHKeys, name+"/"+keyName)
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
		return []string{filepath.Join(machineRoot, filepath.FromSlash(secrets.MachineBundleSecretsRelPrefix), bundleName)}
	}
	projectRoot := strings.TrimSpace(workspace.Root)
	if projectRoot == "" {
		return nil
	}
	return []string{filepath.Join(projectRoot, filepath.FromSlash(secrets.BundleSecretsLocalRelPrefix), bundleName)}
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
