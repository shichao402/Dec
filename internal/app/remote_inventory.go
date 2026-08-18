package app

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
)

// ListRemoteInventory 列出 Remote 页完整库存（ADR 0004 修订）：
// 远端分区不按当前 project / user 平面过滤；本地分区只含当前工作区残留清理项。
func ListRemoteInventory(ctx context.Context, workspace Workspace, includeRemote bool, reporter Reporter) ([]DeleteCandidate, error) {
	reporter = defaultReporter(reporter)
	projectRoot := strings.TrimSpace(workspace.Root)
	if projectRoot == "" && workspace.EffectivePlane() == WorkspaceProject {
		return nil, fmt.Errorf("项目根目录不能为空")
	}

	projectConfig, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return nil, err
	}

	var candidates []DeleteCandidate
	seenDecRemote := make(map[string]struct{})
	seenDecLocal := make(map[string]struct{})
	groupCtx := newDeleteGroupContext(workspace, projectConfig)
	scopeByBundle := resolveVaultScopeTags(reporter)
	enabledBundles := config.NormalizeBundleNames(projectConfig.EnabledBundles)

	addDec := func(kind DeleteItemKind, itemType, name, vault string, orphan bool, partition RemotePartition, scopeTag string) {
		key := itemType + ":" + vault + ":" + name
		seen := seenDecRemote
		if partition == PartitionLocal {
			seen = seenDecLocal
		}
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		tag := ""
		if orphan && partition == PartitionRemote {
			tag = " · 仅远端"
		}
		if scopeTag != "" {
			tag += " · scope:" + scopeTag
		}
		if partition == PartitionLocal {
			tag += " · 只清本机"
		}
		groupBundle, groupOrder := groupCtx.forDecVault(vault)
		treeRoot := ".dec"
		if partition == PartitionLocal {
			treeRoot = localTreeRootDec
		}
		candidates = append(candidates, DeleteCandidate{
			Kind:       kind,
			Type:       itemType,
			Name:       name,
			Vault:      vault,
			Label:      fmt.Sprintf("[dec/%s] %s / %s%s", itemType, name, vault, tag),
			Orphan:     orphan,
			TreeRoot:   treeRoot,
			TreeBranch: groupBundle,
			GroupOrder: groupOrder,
			GroupTitle: groupCtx.groupTitleWithScope(groupBundle, scopeTag),
			Partition:  partition,
			ScopeTag:   scopeTag,
		})
	}

	walkCacheDecLocal := func(vault, itemType, name, scopeTag string) {
		cachePath := getWorkspaceCachePath(workspace, vault, itemType, name)
		if cachePath == "" {
			return
		}
		if _, err := os.Stat(cachePath); err != nil {
			return
		}
		addDec(DeleteKindDecAsset, itemType, name, vault, false, PartitionLocal, scopeTag)
	}

	// 本地分区：当前工作区 cache 里实际存在的残留（含已停用 bundle 留下的目录），只清本机。
	localCacheBundles := listLocalCacheBundleNames(workspace)
	for _, spec := range []struct {
		dir   string
		typ   string
		trim  func(string) string
		isDir bool
	}{
		{"skills", "skill", func(s string) string { return s }, true},
		{"commands", "command", func(s string) string { return s }, true},
		{"rules", "rule", func(s string) string { return strings.TrimSuffix(s, ".mdc") }, false},
		{"mcp", "mcp", func(s string) string { return strings.TrimSuffix(s, ".json") }, false},
	} {
		for _, bundleName := range localCacheBundles {
			dir := filepath.Join(workspaceCacheDir(workspace), bundleName, spec.dir)
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				continue
			}
			for _, entry := range entries {
				if entry.Name() == ".gitkeep" {
					continue
				}
				scopeTag := scopeByBundle[bundleName]
				if spec.isDir {
					if !entry.IsDir() {
						continue
					}
					walkCacheDecLocal(bundleName, spec.typ, entry.Name(), scopeTag)
					continue
				}
				if entry.IsDir() {
					continue
				}
				walkCacheDecLocal(bundleName, spec.typ, spec.trim(entry.Name()), scopeTag)
			}
		}
	}

	// 远端分区：Git vault 全量 bundles（scope 仅作分组标签，enabled 与否都展示）。
	_ = withAppReadRepo(func(tx *repo.Transaction) error {
		repoDir := tx.WorkDir()
		vaultBundles, _, scanErr := scanVaultBundles(repoDir, reporter)
		if scanErr != nil {
			emit(reporter, EventWarn, "delete.list", "扫描 vault bundles 失败（仅展示本地 cache）："+scanErr.Error(), nil)
			return nil
		}
		bundleNames := make([]string, 0, len(vaultBundles)+len(enabledBundles))
		seenBundle := make(map[string]struct{})
		for name, matches := range vaultBundles {
			seenBundle[name] = struct{}{}
			bundleNames = append(bundleNames, name)
			for _, match := range matches {
				if match.bundle.Scope != "" {
					scopeByBundle[name] = string(match.bundle.Scope)
					break
				}
			}
		}
		for _, name := range enabledBundles {
			if _, ok := seenBundle[name]; ok {
				continue
			}
			seenBundle[name] = struct{}{}
			bundleNames = append(bundleNames, name)
		}
		sort.Strings(bundleNames)
		for _, bundleName := range bundleNames {
			scopeTag := scopeByBundle[bundleName]
			members := make([]AssetSelectionItem, 0)
			for _, member := range listBundleAssetMembers(repoDir, bundleName) {
				parts := strings.SplitN(member, "/", 2)
				if len(parts) != 2 {
					continue
				}
				itemType := memberPrefixToAssetType(parts[0])
				if itemType == "" {
					continue
				}
				name := parts[1]
				cachePath := getWorkspaceCachePath(workspace, bundleName, itemType, name)
				_, cacheErr := os.Stat(cachePath)
				localExists := cacheErr == nil
				if !localExists {
					vaultPath := resolveAssetFile(repoDir, bundleName, itemType, name)
					if vaultPath == "" {
						continue
					}
					if _, err := os.Stat(vaultPath); err != nil {
						continue
					}
				}
				members = append(members, AssetSelectionItem{Type: itemType, Name: name, Vault: bundleName})
				addDec(DeleteKindDecAsset, itemType, name, bundleName, !localExists, PartitionRemote, scopeTag)
			}
			// 整包删除项跟着 vault 里真实存在的 bundle 走，不看当前平面启用列表。
			if _, inVault := vaultBundles[bundleName]; !inVault {
				continue
			}
			groupBundle, groupOrder := groupCtx.forDecBundle(bundleName)
			candidates = append(candidates, DeleteCandidate{
				Kind:       DeleteKindBundle,
				BundleName: bundleName,
				Vault:      vaultDirForBundle(vaultBundles, bundleName),
				Members:    members,
				Label: fmt.Sprintf("[bundle] %s / %s · %d 成员",
					bundleName, vaultDirForBundle(vaultBundles, bundleName), len(members)),
				TreeRoot:   ".dec",
				TreeBranch: groupBundle,
				GroupOrder: groupOrder,
				GroupTitle: groupCtx.groupTitleWithScope(groupBundle, scopeTag),
				Partition:  PartitionRemote,
				ScopeTag:   scopeTag,
			})
		}
		return nil
	})

	seenSecretRemote := make(map[string]struct{})
	seenSecretLocal := make(map[string]struct{})
	addSecret := func(secretsBundle, localRoot string, plane secrets.SyncPlane, notePath string, localExists bool, partition RemotePartition, unmanaged bool, scopeTag string) {
		notePath = strings.TrimSpace(notePath)
		if notePath == "" {
			return
		}
		key := secretsBundle + "\x00" + notePath
		seen := seenSecretRemote
		if partition == PartitionLocal {
			seen = seenSecretLocal
		}
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		tag := ""
		if partition == PartitionRemote && !localExists {
			tag = " · 仅远端"
		}
		if unmanaged {
			tag += " · 非Dec管理"
		}
		if scopeTag != "" {
			tag += " · scope:" + scopeTag
		}
		if partition == PartitionLocal {
			tag += " · 只清本机"
		}
		groupBundle, groupOrder := groupCtx.forSecretsBundle(secretsBundle)
		treeRoot := secretsTreeRoot
		if partition == PartitionLocal {
			treeRoot = localTreeRootSecrets
		}
		candidates = append(candidates, DeleteCandidate{
			Kind:          DeleteKindSecret,
			SecretPath:    notePath,
			LocalRoot:     localRoot,
			Plane:         plane,
			SecretsBundle: secretsBundle,
			Label:         fmt.Sprintf("[secret] %s%s", notePath, tag),
			Orphan:        partition == PartitionRemote && !localExists,
			TreeRoot:      treeRoot,
			TreeBranch:    groupBundle,
			GroupOrder:    groupOrder,
			GroupTitle:    groupCtx.secretsGroupTitle(groupBundle),
			Partition:     partition,
			ScopeTag:      scopeTag,
			Unmanaged:     unmanaged,
		})
	}
	seenSSHRemote := make(map[string]struct{})
	seenSSHLocal := make(map[string]struct{})
	addSSHKey := func(secretsBundle, decBundleName, keyName string, localExists bool, partition RemotePartition, unmanaged bool, scopeTag string, plane secrets.SyncPlane) {
		keyName = strings.TrimSpace(keyName)
		if keyName == "" {
			return
		}
		key := secretsBundle + "\x00ssh\x00" + keyName
		seen := seenSSHRemote
		if partition == PartitionLocal {
			seen = seenSSHLocal
		}
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		tag := ""
		if partition == PartitionRemote && !localExists {
			tag = " · 仅远端"
		}
		if unmanaged {
			tag += " · 非Dec管理"
		}
		if scopeTag != "" {
			tag += " · scope:" + scopeTag
		}
		if partition == PartitionLocal {
			tag += " · 只清本机"
		}
		groupBundle, groupOrder := groupCtx.forSecretsBundle(secretsBundle)
		treeRoot := secretsTreeRoot
		if partition == PartitionLocal {
			treeRoot = localTreeRootSecrets
		}
		candidates = append(candidates, DeleteCandidate{
			Kind:          DeleteKindSSHKey,
			SSHKeyName:    keyName,
			DecBundleName: decBundleName,
			SecretsBundle: secretsBundle,
			Plane:         plane,
			Label:         fmt.Sprintf("[ssh] %s%s", sshKeyDisplayName(keyName), tag),
			Orphan:        partition == PartitionRemote && !localExists,
			TreeRoot:      treeRoot,
			TreeBranch:    groupBundle,
			GroupOrder:    groupOrder,
			GroupTitle:    groupCtx.secretsGroupTitle(groupBundle),
			Partition:     partition,
			ScopeTag:      scopeTag,
			Unmanaged:     unmanaged,
		})
	}

	appendLocalSecretCandidates(workspace, projectConfig, func(secretsBundle, localRoot string, plane secrets.SyncPlane, notePath string, localExists bool) {
		addSecret(secretsBundle, localRoot, plane, notePath, localExists, PartitionLocal, false, "")
	}, reporter)

	if includeRemote {
		if err := appendRemoteSecretCandidates(ctx, workspace, projectConfig, func(secretsBundle, localRoot string, plane secrets.SyncPlane, notePath string, localExists bool) {
			unmanaged := !strings.HasPrefix(secretsBundle, secrets.BundleFolderPrefix) && strings.TrimSpace(localRoot) == ""
			scopeTag := ""
			if strings.HasPrefix(secretsBundle, secrets.BundleFolderPrefix) {
				name := strings.TrimPrefix(secretsBundle, secrets.BundleFolderPrefix)
				scopeTag = scopeByBundle[name]
			}
			addSecret(secretsBundle, localRoot, plane, notePath, localExists, PartitionRemote, unmanaged, scopeTag)
		}, func(secretsBundle, decBundleName, keyName string, localExists bool) {
			unmanaged := !strings.HasPrefix(secretsBundle, secrets.BundleFolderPrefix)
			scopeTag := ""
			plane := secrets.SyncPlane("")
			if strings.HasPrefix(secretsBundle, secrets.BundleFolderPrefix) {
				name := strings.TrimPrefix(secretsBundle, secrets.BundleFolderPrefix)
				scopeTag = scopeByBundle[name]
				if secrets.IsMachinePlane(secrets.SyncPlane(scopeTag)) || scopeTag == "user" {
					plane = secrets.SyncPlaneMachine
				} else if scopeTag == "project" {
					plane = secrets.SyncPlaneProject
				}
			}
			addSSHKey(secretsBundle, decBundleName, keyName, localExists, PartitionRemote, unmanaged, scopeTag, plane)
		}, reporter); err != nil {
			return nil, err
		}
		appendUnfiledRemoteCandidates(ctx, &candidates, reporter)
	}

	sortDeleteCandidates(candidates)
	return candidates, nil
}

// listLocalCacheBundleNames 列出 cache/ 下真实存在的 bundle 目录名（不看启用列表）。
func listLocalCacheBundleNames(workspace Workspace) []string {
	cacheDir := workspaceCacheDir(workspace)
	if strings.TrimSpace(cacheDir) == "" {
		return nil
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// vaultDirForBundle 返回 bundle 声明所在的 vault 目录名；缺声明时退回 bundle 名。
func vaultDirForBundle(vaultBundles map[string][]vaultBundle, bundleName string) string {
	for _, match := range vaultBundles[bundleName] {
		if dir := strings.TrimSpace(match.vaultName); dir != "" {
			return dir
		}
	}
	return bundleName
}

func (g *deleteGroupContext) groupTitleWithScope(groupBundle, scopeTag string) string {
	base := g.groupTitle(groupBundle)
	scopeTag = strings.TrimSpace(scopeTag)
	if scopeTag == "" {
		return base
	}
	return fmt.Sprintf("%s · scope:%s", base, scopeTag)
}

// resolveVaultScopeTags 返回 vault 内全部 bundle 的 scope 标签（不按平面过滤）。
func resolveVaultScopeTags(reporter Reporter) map[string]string {
	out := make(map[string]string)
	_ = withAppReadRepo(func(tx *repo.Transaction) error {
		vaultBundles, _, scanErr := scanVaultBundles(tx.WorkDir(), reporter)
		if scanErr != nil {
			return nil
		}
		for name, matches := range vaultBundles {
			for _, match := range matches {
				if match.bundle.Scope != "" {
					out[name] = string(match.bundle.Scope)
					break
				}
			}
		}
		return nil
	})
	return out
}

const unfiledGroupTitle = "无文件夹 · 非Dec管理"

// appendUnfiledRemoteCandidates 把 Bitwarden 无 folder 条目加入只读远端分区（不读正文、不可删）。
func appendUnfiledRemoteCandidates(ctx context.Context, candidates *[]DeleteCandidate, reporter Reporter) {
	reporter = defaultReporter(reporter)
	if candidates == nil {
		return
	}
	configured, err := secrets.IsConfigured()
	if err != nil || !configured || !secrets.HasSession() || !secrets.HasUserKey() {
		return
	}
	client := secretsClientFactory()
	items, listErr := client.ListUnfiledItems(ctx)
	if listErr != nil {
		emit(reporter, EventWarn, "delete.secrets", "列出无文件夹条目失败: "+listErr.Error(), nil)
		return
	}
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		typ := strings.TrimSpace(item.Type)
		if typ == "" {
			typ = "other"
		}
		*candidates = append(*candidates, DeleteCandidate{
			Kind:       DeleteKindSecret,
			Type:       typ,
			Name:       name,
			SecretPath: name,
			Label:      fmt.Sprintf("[%s] %s · 非Dec管理 · 请到 Bitwarden Web", typ, name),
			TreeRoot:   secretsTreeRoot,
			TreeBranch: "__unfiled__",
			GroupOrder: 9999,
			GroupTitle: unfiledGroupTitle,
			Partition:  PartitionRemote,
			Unmanaged:  true,
			ReadOnly:   true,
		})
	}
	if len(items) > 0 {
		emit(reporter, EventInfo, "delete.secrets",
			fmt.Sprintf("发现 %d 个无文件夹条目（只读，非 Dec 管理）", len(items)), nil)
	}
}

// InferDeleteMode 从选中项 Partition 推断删除模式；混选返回错误。
func InferDeleteMode(items []DeleteSelectionItem, explicit DeleteMode) (DeleteMode, error) {
	if explicit == DeleteModeRemote || explicit == DeleteModeLocal {
		return explicit, nil
	}
	var seen RemotePartition
	for _, item := range items {
		p := item.Partition
		if p == "" {
			p = PartitionRemote
		}
		if seen == "" {
			seen = p
			continue
		}
		if seen != p {
			return "", fmt.Errorf("不能同时选择远端分区与本地分区；请分开删除（远端只改远端，本地只清本机）")
		}
	}
	if seen == PartitionLocal {
		return DeleteModeLocal, nil
	}
	return DeleteModeRemote, nil
}

func sshKeyDisplayName(keyName string) string {
	keyName = strings.TrimSpace(strings.ReplaceAll(keyName, "\\", "/"))
	if keyName == "" {
		return keyName
	}
	return path.Base(keyName)
}

// DeleteRemoteOnly 只改远端（Bitwarden / Git vault），不碰本地同步根与 cache。
func DeleteRemoteOnly(ctx context.Context, input DeleteProjectInput, reporter Reporter) (*DeleteProjectResult, error) {
	input.Mode = DeleteModeRemote
	return DeleteProjectItems(ctx, input, reporter)
}

// CleanupLocal 只清本机，不写 Bitwarden / 不写 vault。
func CleanupLocal(ctx context.Context, input DeleteProjectInput, reporter Reporter) (*DeleteProjectResult, error) {
	input.Mode = DeleteModeLocal
	return DeleteProjectItems(ctx, input, reporter)
}
