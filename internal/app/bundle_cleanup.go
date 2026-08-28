package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

// BundleCleanupReport 汇总删除 bundle 后本机/登记侧清理结果，供 reporter 与调用方展示残留。
type BundleCleanupReport struct {
	ForgotKnown          bool
	ClearedProjectEnable bool
	ClearedUserEnable    bool
	RemovedSecretDirs    []string
	RemovedSSHKeys       []string
	PrunedVaultProjects  []string
	Remnants             []string
}

// pruneBundleFromVaultProjects 从 vault projects/*.yaml 的 bundles 列表摘掉已删包。
// 避免 Home 自动应用 / 新机器初始化再次把该包写回 enabled_bundles。
func pruneBundleFromVaultProjects(repoDir, bundleName string) ([]string, error) {
	bundleName = strings.TrimSpace(bundleName)
	if repoDir == "" || bundleName == "" {
		return nil, nil
	}
	projectsDir := filepath.Join(repoDir, types.VaultProjectsDir)
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 vault projects 失败: %w", err)
	}

	var pruned []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), types.VaultProjectFileExt) {
			continue
		}
		path := filepath.Join(projectsDir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return pruned, fmt.Errorf("读取 %s 失败: %w", path, readErr)
		}
		var project types.LegacyVaultProject
		if unmarshalErr := yaml.Unmarshal(data, &project); unmarshalErr != nil {
			return pruned, fmt.Errorf("解析 %s 失败: %w", path, unmarshalErr)
		}
		updated, changed := removeEnabledBundle(project.Bundles, bundleName)
		if !changed {
			continue
		}
		project.Bundles = updated
		body, marshalErr := yaml.Marshal(&project)
		if marshalErr != nil {
			return pruned, marshalErr
		}
		if writeErr := os.WriteFile(path, body, 0644); writeErr != nil {
			return pruned, fmt.Errorf("写入 %s 失败: %w", path, writeErr)
		}
		pruned = append(pruned, strings.TrimSuffix(entry.Name(), types.VaultProjectFileExt))
	}
	return pruned, nil
}

// cleanupDeletedBundleLocalState 在远端 bundle 已删（或本就不存在）后，收敛清理本机登记与落地残留。
// 不触碰 Bitwarden folder 内容（需用户在 Remote 逐条删 secrets）；会在 Remnants 提示可能残留的远端 folder。
func cleanupDeletedBundleLocalState(workspace Workspace, bundleName string, reporter Reporter) BundleCleanupReport {
	reporter = defaultReporter(reporter)
	bundleName = strings.TrimSpace(bundleName)
	report := BundleCleanupReport{}
	if bundleName == "" {
		return report
	}

	if err := secrets.ForgetSecretBundles([]string{bundleName}); err != nil {
		emit(reporter, EventWarn, "remove.cleanup", fmt.Sprintf("清除 known_secret_bundles 失败: %v", err), nil)
	} else {
		report.ForgotKnown = true
		emit(reporter, EventInfo, "remove.cleanup", "已从 known_secret_bundles 移除", nil)
	}

	// 两平面启用列表都摘掉，避免另一平面仍启用 → push/secrets 再写回。
	if changed, err := removeWorkspaceEnabledBundle(workspace, bundleName); err != nil {
		emit(reporter, EventWarn, "remove.cleanup", fmt.Sprintf("当前平面启用列表更新失败: %v", err), nil)
	} else if changed {
		if workspace.EffectivePlane() == WorkspaceUser {
			report.ClearedUserEnable = true
		} else {
			report.ClearedProjectEnable = true
		}
	}
	other := NewWorkspace(WorkspaceProject, workspace.Root)
	if workspace.EffectivePlane() == WorkspaceProject {
		other = NewWorkspace(WorkspaceUser, workspace.Root)
	}
	if changed, err := removeWorkspaceEnabledBundle(other, bundleName); err != nil {
		emit(reporter, EventWarn, "remove.cleanup", fmt.Sprintf("另一平面启用列表更新失败: %v", err), nil)
	} else if changed {
		if other.EffectivePlane() == WorkspaceUser {
			report.ClearedUserEnable = true
		} else {
			report.ClearedProjectEnable = true
		}
		emit(reporter, EventInfo, "remove.cleanup", "已从另一平面 enabled_bundles 移除", nil)
	}

	for _, dir := range localSecretBundleDirs(workspace, bundleName) {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			emit(reporter, EventWarn, "remove.cleanup", fmt.Sprintf("删除本地 secrets 目录失败 %s: %v", dir, err), nil)
			report.Remnants = append(report.Remnants, dir)
			continue
		}
		report.RemovedSecretDirs = append(report.RemovedSecretDirs, dir)
		emit(reporter, EventInfo, "remove.cleanup", fmt.Sprintf("已删本地 secrets 同步根 %s", dir), nil)
	}

	for _, keyName := range listLocalSSHKeyNamesForBundle(bundleName) {
		if err := secrets.RemoveSSHKeyLanding(bundleName, keyName); err != nil {
			emit(reporter, EventWarn, "remove.cleanup", fmt.Sprintf("清理 SSH Key %s 失败: %v", keyName, err), nil)
			report.Remnants = append(report.Remnants, "~/.ssh/dec_"+bundleName+"_"+keyName)
			continue
		}
		report.RemovedSSHKeys = append(report.RemovedSSHKeys, keyName)
		emit(reporter, EventInfo, "remove.cleanup", fmt.Sprintf("已删本地 SSH Key %s", keyName), nil)
	}

	report.Remnants = append(report.Remnants,
		fmt.Sprintf("Bitwarden 上 P %s 的条目可能仍在（push 不会据此重建；请到 Remote 删 secrets）", bundleName))
	emit(reporter, EventInfo, "remove.cleanup",
		fmt.Sprintf("若远端仍有 %s，请到 Remote 页删除其中的 Secure Note / SSH Key", bundleName), nil)

	return report
}

// localSecretBundleDirs 返回一个 P 在两个平面上的本地同步根。
func localSecretBundleDirs(workspace Workspace, bundleName string) []string {
	var dirs []string
	projectRoot := strings.TrimSpace(workspace.Root)
	if projectRoot != "" {
		if target, err := secrets.NewPSyncTarget(bundleName, secrets.SyncPlaneProject); err == nil {
			if abs, absErr := secrets.ResolveAbsDir(projectRoot, target); absErr == nil {
				dirs = append(dirs, abs)
			}
		}
	}
	if target, err := secrets.NewPSyncTarget(bundleName, secrets.SyncPlaneMachine); err == nil {
		if abs, absErr := secrets.ResolveAbsDir("", target); absErr == nil {
			dirs = append(dirs, abs)
		}
	}
	return dirs
}

func listLocalSSHKeyNamesForBundle(bundleName string) []string {
	bundleName = strings.TrimSpace(bundleName)
	if bundleName == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	pattern := filepath.Join(home, ".ssh", "dec_"+bundleName+"_*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	prefix := "dec_" + bundleName + "_"
	seen := make(map[string]struct{})
	var names []string
	for _, match := range matches {
		base := filepath.Base(match)
		if strings.HasSuffix(base, ".pub") {
			base = strings.TrimSuffix(base, ".pub")
		}
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		keyName := strings.TrimPrefix(base, prefix)
		if keyName == "" {
			continue
		}
		if _, ok := seen[keyName]; ok {
			continue
		}
		seen[keyName] = struct{}{}
		names = append(names, keyName)
	}
	return names
}
