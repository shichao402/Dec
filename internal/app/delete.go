package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
	"github.com/shichao402/Dec/internal/types"
)

type DeleteItemKind string

const (
	DeleteKindDecAsset DeleteItemKind = "dec"
	DeleteKindSecret   DeleteItemKind = "secret"
	DeleteKindSSHKey   DeleteItemKind = "ssh"
	DeleteKindBundle   DeleteItemKind = "bundle"
)

// secretsTreeRoot 是 Delete 树中 secrets 分支的根标识。
// 不再是 ".secrets"：落地路径散在项目根，没有一个共同的目录前缀，
// 这里表达的是「归 Bitwarden 管的密文件」这个逻辑分组。
const secretsTreeRoot = "secrets"

// DeleteCandidate 描述 Remote 页可选项。
type DeleteCandidate struct {
	Kind          DeleteItemKind
	Label         string
	Type          string
	Name          string
	Vault         string
	SecretPath    string            // secrets：相对 SyncTarget.LocalRoot（= Bitwarden Note 名）
	LocalRoot     string            // secrets：.secrets/project 或 .secrets/bundles/<name>（machine 平面为 bundles/<name>）
	Plane         secrets.SyncPlane // secrets：本地同步根所在平面（project / machine），用于正确解析绝对路径
	SecretsBundle string            // secrets / ssh：Bitwarden folder
	SSHKeyName    string            // ssh：逻辑名
	DecBundleName string            // ssh：用于本地 ~/.ssh/dec_<bundle>_<name>
	BundleName    string
	Members       []AssetSelectionItem
	Orphan        bool
	TreeRoot      string // ".dec" 或 secretsTreeRoot
	TreeBranch    string // 根下分组名（dec cache bundle 或 Bitwarden folder）
	GroupOrder    int
	GroupTitle    string
}

// DeleteSelectionItem 为一次删除操作选中的候选项。
type DeleteSelectionItem struct {
	Kind          DeleteItemKind
	Type          string
	Name          string
	Vault         string
	SecretPath    string
	LocalRoot     string
	Plane         secrets.SyncPlane
	SecretsBundle string
	SSHKeyName    string
	DecBundleName string
	BundleName    string
	Members       []AssetSelectionItem
}

// DeleteProjectInput 描述 Remote 页批量删除输入。
// Plane 为空视为项目平面，保持旧调用语义。
type DeleteProjectInput struct {
	ProjectRoot string
	Plane       WorkspacePlane
	Items       []DeleteSelectionItem
	Confirmed   bool
}

func (in DeleteProjectInput) workspace() Workspace {
	return NewWorkspace(in.Plane, in.ProjectRoot)
}

// DeleteProjectResult 汇总 Remote 页批量删除结果。
type DeleteProjectResult struct {
	DecDeleted     int
	SecretsDeleted int
	SSHKeysDeleted int
	BundlesDeleted int
	VersionCommit  string
	LastCommit     string
	SkippedReason  string
}

// ErrDeleteNotConfirmed 调用方没有完成二次确认。
var ErrDeleteNotConfirmed = fmt.Errorf("delete 未确认")

// ListDeleteCandidates 列出当前项目可删除的 Dec 资产、secrets 文件与 bundle。
// includeRemote 为 true 且 Bitwarden 已配置时，会按需触发 web unlock 并补充远端 Secure Note 候选项。
func ListDeleteCandidates(ctx context.Context, projectRoot string, includeRemote bool, reporter Reporter) ([]DeleteCandidate, error) {
	return ListWorkspaceDeleteCandidates(ctx, NewWorkspace(WorkspaceProject, projectRoot), includeRemote, reporter)
}

// ListWorkspaceDeleteCandidates 按平面列出可删除项。
// 用户平面扫 ~/.dec/cache 与 ~/.dec/secrets，只暴露 scope: user 的 bundle。
func ListWorkspaceDeleteCandidates(ctx context.Context, workspace Workspace, includeRemote bool, reporter Reporter) ([]DeleteCandidate, error) {
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
	seenDec := make(map[string]struct{})
	groupCtx := newDeleteGroupContext(workspace, projectConfig)

	addDec := func(kind DeleteItemKind, itemType, name, vault string, orphan bool) {
		key := itemType + ":" + vault + ":" + name
		if _, dup := seenDec[key]; dup {
			return
		}
		seenDec[key] = struct{}{}
		tag := ""
		if orphan {
			tag = " · 仅远端"
		}
		groupBundle, groupOrder := groupCtx.forDecVault(vault)
		candidates = append(candidates, DeleteCandidate{
			Kind:       kind,
			Type:       itemType,
			Name:       name,
			Vault:      vault,
			Label:      fmt.Sprintf("[dec/%s] %s / %s%s", itemType, name, vault, tag),
			Orphan:     orphan,
			TreeRoot:   ".dec",
			TreeBranch: groupBundle,
			GroupOrder: groupOrder,
			GroupTitle: groupCtx.groupTitle(groupBundle),
		})
	}

	walkCacheDec := func(vault, itemType, name string) {
		cachePath := getWorkspaceCachePath(workspace, vault, itemType, name)
		if cachePath == "" {
			return
		}
		if _, err := os.Stat(cachePath); err != nil {
			return
		}
		addDec(DeleteKindDecAsset, itemType, name, vault, false)
	}

	// 平面隔离（ADR 0009）：project 上下文的删除候选只看项目启用列表。
	enabledBundles := config.NormalizeBundleNames(projectConfig.EnabledBundles)

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
		for _, bundleName := range enabledBundles {
			dir := filepath.Join(workspaceCacheDir(workspace), bundleName, spec.dir)
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				continue
			}
			for _, entry := range entries {
				if entry.Name() == ".gitkeep" {
					continue
				}
				if spec.isDir {
					if !entry.IsDir() {
						continue
					}
					walkCacheDec(bundleName, spec.typ, entry.Name())
					continue
				}
				if entry.IsDir() {
					continue
				}
				walkCacheDec(bundleName, spec.typ, spec.trim(entry.Name()))
			}
		}
	}

	// LocalRead 浏览 vault：启用包 ∪ vault 内同平面 bundle（不 Fetch）。
	_ = withAppReadRepo(func(tx *repo.Transaction) error {
		repoDir := tx.WorkDir()
		vaultBundles, _, scanErr := scanVaultBundles(repoDir, reporter)
		if scanErr != nil {
			emit(reporter, EventWarn, "delete.list", "扫描 vault bundles 失败（仅展示本地 cache）："+scanErr.Error(), nil)
			return nil
		}
		wantScope := bundleScopeForPlane(workspace.EffectivePlane())
		bundleNames := make([]string, 0, len(vaultBundles)+len(enabledBundles))
		seenBundle := make(map[string]struct{})
		for name, matches := range vaultBundles {
			// 平面隔离（ADR 0009）：不把另一平面的 bundle 列进删除候选。
			inPlane := false
			for _, match := range matches {
				if match.bundle.Scope == wantScope {
					inPlane = true
					break
				}
			}
			if !inPlane {
				continue
			}
			seenBundle[name] = struct{}{}
			bundleNames = append(bundleNames, name)
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
				addDec(DeleteKindDecAsset, itemType, name, bundleName, !localExists)
			}
		}
		return nil
	})

	// secrets：本地 SyncTarget 扫描 ∪（可选）远端 orphan。
	seenSecret := make(map[string]struct{})
	addSecret := func(secretsBundle, localRoot string, plane secrets.SyncPlane, notePath string, localExists bool) {
		notePath = strings.TrimSpace(notePath)
		if notePath == "" {
			return
		}
		key := secretsBundle + "\x00" + notePath
		if _, dup := seenSecret[key]; dup {
			return
		}
		seenSecret[key] = struct{}{}
		tag := ""
		if !localExists {
			tag = " · 仅远端"
		}
		groupBundle, groupOrder := groupCtx.forSecretsBundle(secretsBundle)
		candidates = append(candidates, DeleteCandidate{
			Kind:          DeleteKindSecret,
			SecretPath:    notePath,
			LocalRoot:     localRoot,
			Plane:         plane,
			SecretsBundle: secretsBundle,
			Label:         fmt.Sprintf("[secret] %s%s", notePath, tag),
			Orphan:        !localExists,
			TreeRoot:      secretsTreeRoot,
			TreeBranch:    groupBundle,
			GroupOrder:    groupOrder,
			GroupTitle:    groupCtx.secretsGroupTitle(groupBundle),
		})
	}
	seenSSH := make(map[string]struct{})
	addSSHKey := func(secretsBundle, decBundleName, keyName string, localExists bool) {
		keyName = strings.TrimSpace(keyName)
		if keyName == "" {
			return
		}
		key := secretsBundle + "\x00ssh\x00" + keyName
		if _, dup := seenSSH[key]; dup {
			return
		}
		seenSSH[key] = struct{}{}
		tag := ""
		if !localExists {
			tag = " · 仅远端"
		}
		groupBundle, groupOrder := groupCtx.forSecretsBundle(secretsBundle)
		candidates = append(candidates, DeleteCandidate{
			Kind:          DeleteKindSSHKey,
			SSHKeyName:    keyName,
			DecBundleName: decBundleName,
			SecretsBundle: secretsBundle,
			Label:         fmt.Sprintf("[ssh] %s%s", keyName, tag),
			Orphan:        !localExists,
			TreeRoot:      secretsTreeRoot,
			TreeBranch:    groupBundle,
			GroupOrder:    groupOrder,
			GroupTitle:    groupCtx.secretsGroupTitle(groupBundle),
		})
	}

	appendLocalSecretCandidates(workspace, projectConfig, addSecret, reporter)

	if includeRemote {
		if err := appendRemoteSecretCandidates(ctx, workspace, projectConfig, addSecret, addSSHKey, reporter); err != nil {
			return nil, err
		}
	}

	if state, loadErr := LoadWorkspaceAssetSelection(workspace, reporter); loadErr == nil {
		for _, bo := range ListEnabledBundles(state) {
			groupBundle, groupOrder := groupCtx.forDecBundle(bo.Name)
			candidates = append(candidates, DeleteCandidate{
				Kind:       DeleteKindBundle,
				BundleName: bo.Name,
				Vault:      bo.Vault,
				Members:    append([]AssetSelectionItem(nil), bo.Members...),
				Label:      fmt.Sprintf("[bundle] %s / %s · %d 成员", bo.Name, fallbackVaultName(bo), len(bo.Members)),
				TreeRoot:   ".dec",
				TreeBranch: groupBundle,
				GroupOrder: groupOrder,
				GroupTitle: groupCtx.groupTitle(groupBundle),
			})
		}
	}

	sortDeleteCandidates(candidates)
	return candidates, nil
}

type deleteGroupContext struct {
	bundleOrder    map[string]int
	secretsToDec   map[string]string
	projectSecrets string
	projectName    string
}

func newDeleteGroupContext(workspace Workspace, projectConfig *types.ProjectConfig) *deleteGroupContext {
	projectRoot := workspace.Root
	userPlane := workspace.EffectivePlane() == WorkspaceUser
	ctx := &deleteGroupContext{
		bundleOrder:  make(map[string]int),
		secretsToDec: make(map[string]string),
	}
	for i, name := range projectConfig.EnabledBundles {
		ctx.bundleOrder[name] = i
	}
	if configured, err := secrets.IsConfigured(); err == nil && configured {
		if cfg, loadErr := secrets.LoadConfig(); loadErr == nil && cfg != nil {
			for _, decBundle := range projectConfig.EnabledBundles {
				binding := cfg.ResolveBinding(decBundle)
				secretsName := strings.TrimSpace(binding.SecretsBundleName)
				if secretsName == "" {
					secretsName = decBundle
				}
				ctx.secretsToDec[secretsName] = decBundle
			}
			for _, binding := range cfg.Bundles {
				decBundle := strings.TrimSpace(binding.DecBundleName)
				secretsName := strings.TrimSpace(binding.SecretsBundleName)
				if decBundle == "" || secretsName == "" {
					continue
				}
				ctx.secretsToDec[secretsName] = decBundle
				if _, ok := ctx.bundleOrder[decBundle]; !ok {
					ctx.bundleOrder[decBundle] = len(ctx.bundleOrder) + 100
				}
			}
			// 用户平面没有 project secrets 归属，跳过这一组，避免把项目 folder 混进来。
			if !userPlane {
				projectName, _ := ResolveProjectName(projectRoot, projectConfig)
				ctx.projectName = projectName
				if name, enabled := cfg.ResolveProjectSecrets(projectName); enabled {
					ctx.projectSecrets = name
					ctx.secretsToDec[name] = secrets.ProjectSecretsDecBundleName
				}
			}
		}
	}
	if ctx.projectName == "" && !userPlane {
		ctx.projectName, _ = ResolveProjectName(projectRoot, projectConfig)
	}
	return ctx
}

func (g *deleteGroupContext) orderFor(bundleName string) int {
	if bundleName == secrets.ProjectSecretsDecBundleName {
		return -1
	}
	if order, ok := g.bundleOrder[bundleName]; ok {
		return order
	}
	return 1000
}

func (g *deleteGroupContext) forDecVault(vault string) (string, int) {
	vault = strings.TrimSpace(vault)
	if vault == "" {
		vault = "dec"
	}
	return vault, g.orderFor(vault)
}

func (g *deleteGroupContext) forDecBundle(bundleName string) (string, int) {
	bundleName = strings.TrimSpace(bundleName)
	if bundleName == "" {
		bundleName = "dec"
	}
	return bundleName, g.orderFor(bundleName)
}

func (g *deleteGroupContext) forSecretsBundle(secretsBundle string) (string, int) {
	secretsBundle = strings.TrimSpace(secretsBundle)
	if secretsBundle == g.projectSecrets {
		return secretsBundle, g.orderFor(secrets.ProjectSecretsDecBundleName)
	}
	if decBundle, ok := g.secretsToDec[secretsBundle]; ok {
		return secretsBundle, g.orderFor(decBundle)
	}
	return secretsBundle, g.orderFor(secretsBundle)
}

func (g *deleteGroupContext) secretsGroupTitle(secretsBundle string) string {
	if secretsBundle == g.projectSecrets {
		name := strings.TrimSpace(g.projectName)
		if name == "" {
			name = "?"
		}
		return fmt.Sprintf("%s (project)", name)
	}
	return secretsBundle
}

func (g *deleteGroupContext) groupTitle(groupBundle string) string {
	if groupBundle == secrets.ProjectSecretsDecBundleName {
		name := strings.TrimSpace(g.projectName)
		if name == "" {
			name = "?"
		}
		return fmt.Sprintf("%s (project)", name)
	}
	return fmt.Sprintf("%s (bundle)", groupBundle)
}

func deleteKindOrder(kind DeleteItemKind) int {
	switch kind {
	case DeleteKindDecAsset:
		return 0
	case DeleteKindSecret:
		return 1
	case DeleteKindSSHKey:
		return 2
	case DeleteKindBundle:
		return 3
	default:
		return 9
	}
}

func sortDeleteCandidates(candidates []DeleteCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].TreeRoot != candidates[j].TreeRoot {
			return candidates[i].TreeRoot < candidates[j].TreeRoot
		}
		if candidates[i].GroupOrder != candidates[j].GroupOrder {
			return candidates[i].GroupOrder < candidates[j].GroupOrder
		}
		if candidates[i].TreeBranch != candidates[j].TreeBranch {
			return candidates[i].TreeBranch < candidates[j].TreeBranch
		}
		if deleteKindOrder(candidates[i].Kind) != deleteKindOrder(candidates[j].Kind) {
			return deleteKindOrder(candidates[i].Kind) < deleteKindOrder(candidates[j].Kind)
		}
		return candidates[i].Label < candidates[j].Label
	})
}

func appendLocalSecretCandidates(
	workspace Workspace,
	projectConfig *types.ProjectConfig,
	addSecret func(secretsBundle, localRoot string, plane secrets.SyncPlane, notePath string, localExists bool),
	reporter Reporter,
) {
	reporter = defaultReporter(reporter)
	projectRoot := workspace.Root
	cfg, err := secrets.LoadConfig()
	if err != nil {
		emit(reporter, EventWarn, "delete.secrets", "读取 secrets 配置失败，跳过本地 secrets 扫描: "+err.Error(), nil)
		return
	}
	plan, err := planWorkspaceSecretsSync(workspace, projectConfig.EnabledBundles, cfg)
	if err != nil {
		emit(reporter, EventWarn, "delete.secrets", "规划 SyncTarget 失败，跳过本地 secrets 扫描: "+err.Error(), nil)
		return
	}
	if len(plan.Targets) == 0 {
		emit(reporter, EventInfo, "delete.secrets", "无 SyncTarget（当前平面未启用 secrets 归属）", nil)
		return
	}
	for _, target := range plan.Targets {
		notes, scanErr := secrets.ScanSyncRoot(projectRoot, target)
		if scanErr != nil {
			emit(reporter, EventWarn, "delete.secrets",
				fmt.Sprintf("扫描本地 %s 失败: %v", target.LocalRoot, scanErr), nil)
			continue
		}
		for _, note := range notes {
			addSecret(target.Folder, target.LocalRoot, target.Plane, note.RelativePath, true)
		}
	}
}

func appendRemoteSecretCandidates(
	ctx context.Context,
	workspace Workspace,
	projectConfig *types.ProjectConfig,
	addSecret func(secretsBundle, localRoot string, plane secrets.SyncPlane, notePath string, localExists bool),
	addSSHKey func(secretsBundle, decBundleName, keyName string, localExists bool),
	reporter Reporter,
) error {
	reporter = defaultReporter(reporter)
	projectRoot := workspace.Root
	configured, err := secrets.IsConfigured()
	if err != nil {
		emit(reporter, EventWarn, "delete.secrets", "读取 Bitwarden 配置失败: "+err.Error(), nil)
		return nil
	}
	if !configured {
		localRootLabel := ".secrets"
		if workspace.EffectivePlane() == WorkspaceUser {
			localRootLabel = "~/.dec/secrets"
		}
		emit(reporter, EventInfo, "delete.secrets",
			fmt.Sprintf("Bitwarden 未配置：仅展示本地 %s（到 Settings 填写连接信息）", localRootLabel), nil)
		return nil
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		emit(reporter, EventWarn, "delete.secrets", "加载 secrets 配置失败: "+err.Error(), nil)
		return nil
	}
	if !secrets.HasSession() {
		emit(reporter, EventInfo, "delete.secrets", "[auth] remote scan: Bitwarden session required", nil)
		if err := ensureBitwardenSession(ctx, reporter, "delete.secrets"); err != nil {
			emit(reporter, EventWarn, "delete.secrets", "远端未检查（解锁失败，保留本地 secrets 列表）: "+err.Error(), nil)
			return nil
		}
	}
	if !secrets.HasUserKey() {
		emit(reporter, EventWarn, "delete.secrets", "Bitwarden vault 密钥未就绪，跳过远端补全", nil)
		return nil
	}

	client := secretsClientFactory()
	plan, err := planWorkspaceSecretsSync(workspace, projectConfig.EnabledBundles, cfg)
	if err != nil {
		emit(reporter, EventWarn, "delete.secrets", "规划 SyncTarget 失败: "+err.Error(), nil)
		return nil
	}

	for _, target := range plan.Targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		folder := target.Folder
		notes, listErr := client.ListFolderNotes(ctx, folder)
		if listErr != nil {
			emit(reporter, EventWarn, "delete.secrets",
				fmt.Sprintf("列出远端 %s 失败: %v", formatSyncTargetLabel(target), listErr), nil)
			continue
		}
		for _, note := range notes {
			abs, absErr := secrets.AbsolutePath(projectRoot, target, note.Name)
			localExists := false
			if absErr == nil {
				if _, statErr := os.Stat(abs); statErr == nil {
					localExists = true
				}
			}
			addSecret(folder, target.LocalRoot, target.Plane, note.Name, localExists)
		}
		owner := target.Name
		if target.Kind == secrets.SyncKindProject {
			owner = "project"
		}
		keys, listKeysErr := client.ListFolderSSHKeys(ctx, folder)
		if listKeysErr != nil {
			emit(reporter, EventWarn, "delete.secrets",
				fmt.Sprintf("列出远端 %s SSH Key 失败: %v", formatSyncTargetLabel(target), listKeysErr), nil)
			continue
		}
		for _, key := range keys {
			localExists, existsErr := secrets.LocalSSHKeyExists(owner, key.Name)
			if existsErr != nil {
				emit(reporter, EventWarn, "delete.secrets", existsErr.Error(), nil)
				continue
			}
			addSSHKey(folder, owner, key.Name, localExists)
		}
	}
	return nil
}

func fallbackVaultName(bo AssetBundleOption) string {
	if strings.TrimSpace(bo.Vault) != "" {
		return bo.Vault
	}
	return bo.Name
}

// DeleteProjectItems 执行 Remote 页选中的删除（Dec vault + cache + IDE；secrets 本地 + Bitwarden）。
func DeleteProjectItems(ctx context.Context, input DeleteProjectInput, reporter Reporter) (*DeleteProjectResult, error) {
	reporter = defaultReporter(reporter)
	if strings.TrimSpace(input.ProjectRoot) == "" {
		return nil, fmt.Errorf("项目根目录不能为空")
	}
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("未选择任何删除项")
	}
	if !input.Confirmed {
		return nil, ErrDeleteNotConfirmed
	}

	result := &DeleteProjectResult{}
	var lastCommit string
	workspace := input.workspace()

	for _, item := range input.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch item.Kind {
		case DeleteKindBundle:
			bundleName := strings.TrimSpace(item.BundleName)
			if bundleName == "" {
				continue
			}
			emit(reporter, EventInfo, "delete.bundle", fmt.Sprintf("删除 bundle %s", bundleName), nil)
			bundleResult, err := RemoveBundle(RemoveBundleInput{
				ProjectRoot: input.ProjectRoot,
				Plane:       input.Plane,
				BundleName:  bundleName,
				Members:     append([]AssetSelectionItem(nil), item.Members...),
				Confirmed:   true,
			}, reporter)
			if err != nil {
				if !isVaultMissingErr(err) {
					return nil, err
				}
				emit(reporter, EventInfo, "delete.bundle", fmt.Sprintf("vault 无 bundle %s，仅清理本地", bundleName), nil)
				if localErr := deleteLocalBundleOnly(workspace, bundleName, item.Members, reporter); localErr != nil {
					return nil, localErr
				}
			} else if bundleResult != nil && bundleResult.VersionCommit != "" {
				lastCommit = bundleResult.VersionCommit
			}
			result.BundlesDeleted++
			pruneEmptyDecCacheBundle(workspace, bundleName, reporter)
		case DeleteKindDecAsset:
			emit(reporter, EventInfo, "delete.dec", fmt.Sprintf("删除 [%s] %s", item.Type, item.Name), nil)
			assetResult, err := RemoveAsset(RemoveAssetInput{
				ProjectRoot: input.ProjectRoot,
				Plane:       input.Plane,
				Type:        item.Type,
				Name:        item.Name,
				Vault:       item.Vault,
				Confirmed:   true,
			}, reporter)
			if err != nil {
				if !isVaultMissingErr(err) {
					return nil, err
				}
				emit(reporter, EventInfo, "delete.dec", fmt.Sprintf("vault 无 [%s] %s，仅清理本地", item.Type, item.Name), nil)
				if localErr := deleteLocalDecAssetOnly(workspace, item.Type, item.Name, item.Vault, reporter); localErr != nil {
					return nil, localErr
				}
			} else if assetResult != nil && assetResult.VersionCommit != "" {
				lastCommit = assetResult.VersionCommit
			}
			result.DecDeleted++
			pruneEmptyDecCacheBundle(workspace, item.Vault, reporter)
		case DeleteKindSecret:
			emit(reporter, EventInfo, "delete.secrets", fmt.Sprintf("删除 secret %s", item.SecretPath), nil)
			if err := deleteSecretItem(ctx, workspace, item.SecretsBundle, item.LocalRoot, item.Plane, item.SecretPath, reporter); err != nil {
				return nil, err
			}
			result.SecretsDeleted++
		case DeleteKindSSHKey:
			emit(reporter, EventInfo, "delete.ssh", fmt.Sprintf("删除 SSH Key %s", item.SSHKeyName), nil)
			if err := deleteSSHKeyItem(ctx, item.DecBundleName, item.SecretsBundle, item.SSHKeyName, reporter); err != nil {
				return nil, err
			}
			result.SSHKeysDeleted++
		default:
			return nil, fmt.Errorf("不支持的删除类型: %s", item.Kind)
		}
	}

	result.VersionCommit = lastCommit
	result.LastCommit = lastCommit
	summary := fmt.Sprintf("✅ 删除完成：Dec %d · secrets %d · ssh %d · bundle %d",
		result.DecDeleted, result.SecretsDeleted, result.SSHKeysDeleted, result.BundlesDeleted)
	emit(reporter, EventInfo, "delete.finish", summary, nil)
	return result, nil
}

func deleteSecretItem(ctx context.Context, workspace Workspace, secretsBundleName, localRoot string, plane secrets.SyncPlane, notePath string, reporter Reporter) error {
	projectRoot := workspace.Root
	notePath = strings.TrimSpace(notePath)
	if notePath == "" {
		return fmt.Errorf("Secure Note 路径不能为空")
	}
	localRoot = strings.TrimSpace(localRoot)

	// 平面隔离（ADR 0009）：machine 平面 LocalRoot 是 bundles/<name>（相对 ~/.dec/secrets），
	// 不能直接 Join 到项目根，否则会误落到 <project>/bundles/...；统一走 secrets.AbsolutePath 解析。
	var localPath string
	if localRoot != "" {
		abs, absErr := secrets.AbsolutePath(projectRoot, secrets.SyncTarget{LocalRoot: localRoot, Plane: plane}, notePath)
		if absErr != nil {
			return fmt.Errorf("解析 secret 绝对路径失败: %w", absErr)
		}
		localPath = abs
	} else {
		// 兼容旧调用：若未传 LocalRoot，把 notePath 当项目根相对路径处理。
		localPath = filepath.Join(projectRoot, filepath.FromSlash(notePath))
	}

	// gitgcm：删除前先撤销机器平面副作用（git credential reject + --unset provider）。
	// 优先读本地正文；本地缺失时尝试从远端拉正文再 revoke（能做最好，不阻塞删除）。
	if handler.Default().Find(handler.SourceNote, handler.NoteRouteName(notePath)) != nil {
		if content, ok := readSecretNoteContent(ctx, projectRoot, secretsBundleName, localRoot, plane, notePath, localPath, reporter); ok {
			if _, revErr := handler.RevokeNotes(ctx, nil, []handler.Item{{
				Source:      handler.SourceNote,
				Name:        notePath,
				NoteContent: content,
			}}); revErr != nil {
				emit(reporter, EventWarn, "delete.secrets", fmt.Sprintf("  撤销机器平面副作用失败（继续删除）: %v", revErr), nil)
			} else {
				emit(reporter, EventInfo, "delete.secrets", fmt.Sprintf("  已撤销 %s 的机器平面副作用", notePath), nil)
			}
		}
	}

	if _, err := os.Stat(localPath); err == nil {
		if rmErr := os.Remove(localPath); rmErr != nil {
			return fmt.Errorf("删除本地文件 %s 失败: %w", localPath, rmErr)
		}
		emit(reporter, EventInfo, "delete.secrets", fmt.Sprintf("  已删本地 %s", notePath), nil)
	}

	configured, err := secrets.IsConfigured()
	if err != nil {
		return fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		emit(reporter, EventInfo, "delete.secrets", "Bitwarden 未配置，跳过远端 Secure Note 删除", nil)
		return nil
	}
	if !secrets.HasSession() {
		emit(reporter, EventInfo, "delete.secrets", "[auth] delete: Bitwarden session required", nil)
		if err := ensureBitwardenSession(ctx, reporter, "delete.secrets"); err != nil {
			return err
		}
	}

	client := secretsClientFactory()
	if err := client.DeleteSecureNote(ctx, secrets.DeleteSecureNoteRequest{
		Binding:  secrets.BundleBinding{SecretsBundleName: secretsBundleName},
		NotePath: notePath,
	}); err != nil {
		return fmt.Errorf("删除 Bitwarden Secure Note %q 失败: %w", notePath, err)
	}
	emit(reporter, EventInfo, "delete.secrets", fmt.Sprintf("  已删 Bitwarden Note %s", notePath), nil)
	return nil
}

// readSecretNoteContent 读取待删 Secure Note 正文，供 Handler.Revoke 使用。
// 优先读本地文件；本地缺失且已有 Bitwarden session 时尽力从远端拉正文（不触发 web unlock）。
func readSecretNoteContent(
	ctx context.Context,
	projectRoot, secretsBundleName, localRoot string,
	plane secrets.SyncPlane,
	notePath, localPath string,
	reporter Reporter,
) (string, bool) {
	if localPath != "" {
		if data, err := os.ReadFile(localPath); err == nil {
			return string(data), true
		}
	}
	configured, err := secrets.IsConfigured()
	if err != nil || !configured {
		return "", false
	}
	if !secrets.HasSession() || !secrets.HasUserKey() {
		return "", false
	}
	client := secretsClientFactory()
	result, err := client.PullBundle(ctx, secrets.PullBundleRequest{
		ProjectRoot: projectRoot,
		Target:      secrets.SyncTarget{Folder: secretsBundleName, LocalRoot: localRoot, Plane: plane},
		Binding:     secrets.BundleBinding{SecretsBundleName: secretsBundleName},
	})
	if err != nil || result == nil {
		if err != nil {
			emit(reporter, EventWarn, "delete.secrets", fmt.Sprintf("  拉取远端 %s 正文失败（跳过 revoke）: %v", notePath, err), nil)
		}
		return "", false
	}
	for _, note := range result.Notes {
		if note.RelativePath == notePath {
			return note.Content, true
		}
	}
	return "", false
}

func deleteSSHKeyItem(ctx context.Context, decBundleName, secretsBundleName, keyName string, reporter Reporter) error {
	keyName = strings.TrimSpace(keyName)
	if keyName == "" {
		return fmt.Errorf("SSH Key 名称不能为空")
	}
	decBundleName = strings.TrimSpace(decBundleName)
	if decBundleName == "" {
		return fmt.Errorf("Dec bundle 名称不能为空")
	}

	if err := secrets.RemoveSSHKeyLanding(decBundleName, keyName); err != nil {
		return err
	}
	emit(reporter, EventInfo, "delete.ssh", fmt.Sprintf("  已删本地 SSH Key %s", keyName), nil)

	configured, err := secrets.IsConfigured()
	if err != nil {
		return fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		emit(reporter, EventInfo, "delete.ssh", "Bitwarden 未配置，跳过远端 SSH Key 删除", nil)
		return nil
	}
	if !secrets.HasSession() {
		emit(reporter, EventInfo, "delete.ssh", "[auth] delete: Bitwarden session required", nil)
		if err := ensureBitwardenSession(ctx, reporter, "delete.ssh"); err != nil {
			return err
		}
	}

	client := secretsClientFactory()
	if err := client.DeleteSSHKey(ctx, secrets.DeleteSSHKeyRequest{
		Binding: secrets.BundleBinding{SecretsBundleName: secretsBundleName},
		KeyName: keyName,
	}); err != nil {
		return fmt.Errorf("删除 Bitwarden SSH Key %q 失败: %w", keyName, err)
	}
	emit(reporter, EventInfo, "delete.ssh", fmt.Sprintf("  已删 Bitwarden SSH Key %s", keyName), nil)
	return nil
}

func withAppReadRepo(fn func(*repo.Transaction) error) error {
	tx, err := repo.NewLocalReadTransaction()
	if err != nil {
		return err
	}
	defer tx.Close()
	return fn(tx)
}

func isVaultMissingErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "未找到")
}

func deleteLocalDecAssetOnly(workspace Workspace, itemType, name, vault string, reporter Reporter) error {
	projectIDEs := resolveWorkspaceIDEs(workspace, reporter)
	for _, ideImpl := range projectIDEs {
		if _, err := removeAssetFromIDE(itemType, name, workspace, ideImpl); err != nil {
			emit(reporter, EventWarn, "delete.dec", fmt.Sprintf("IDE %s 清理失败: %v", ideImpl.Name(), err), nil)
		}
	}
	cachePath := getWorkspaceCachePath(workspace, vault, itemType, name)
	if cachePath == "" {
		return nil
	}
	if _, err := os.Stat(cachePath); err != nil {
		if os.IsNotExist(err) {
			emit(reporter, EventInfo, "delete.dec", "本地 cache 已不存在，跳过", nil)
			return nil
		}
		return fmt.Errorf("检查本地 cache 失败: %w", err)
	}
	if err := os.RemoveAll(cachePath); err != nil {
		return fmt.Errorf("删除本地 cache %s 失败: %w", cachePath, err)
	}
	emit(reporter, EventInfo, "delete.dec", fmt.Sprintf("已删本地 cache [%s] %s", itemType, name), nil)
	return nil
}

func deleteLocalBundleOnly(workspace Workspace, bundleName string, members []AssetSelectionItem, reporter Reporter) error {
	projectIDEs := resolveWorkspaceIDEs(workspace, reporter)
	for _, member := range members {
		for _, ideImpl := range projectIDEs {
			if _, err := removeAssetFromIDE(member.Type, member.Name, workspace, ideImpl); err != nil {
				emit(reporter, EventWarn, "delete.bundle", fmt.Sprintf("IDE %s 清理 %s 失败: %v", ideImpl.Name(), member.Name, err), nil)
			}
		}
	}
	cacheBundleDir := filepath.Join(workspaceCacheDir(workspace), bundleName)
	if _, err := os.Stat(cacheBundleDir); err == nil {
		if err := os.RemoveAll(cacheBundleDir); err != nil {
			return fmt.Errorf("删除本地 bundle cache 失败: %w", err)
		}
		emit(reporter, EventInfo, "delete.bundle", "已删本地 bundle 缓存", nil)
	}
	if changed, err := removeWorkspaceEnabledBundle(workspace, bundleName); err != nil {
		emit(reporter, EventWarn, "delete.bundle", fmt.Sprintf("启用列表更新失败: %v", err), nil)
	} else if changed {
		emit(reporter, EventInfo, "delete.bundle", "已从 enabled_bundles 移除", nil)
	}
	return nil
}

// pruneEmptyDecCacheBundle 删除 cache/<bundle> 下已空的类型目录，若整个 bundle 目录变空则删掉目录。
func pruneEmptyDecCacheBundle(workspace Workspace, bundleName string, reporter Reporter) {
	bundleName = strings.TrimSpace(bundleName)
	if bundleName == "" {
		return
	}
	cacheDir := workspaceCacheDir(workspace)
	if cacheDir == "" {
		return
	}
	bundleDir := filepath.Join(cacheDir, bundleName)
	if _, err := os.Stat(bundleDir); err != nil {
		return
	}
	entries, err := os.ReadDir(bundleDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sub := filepath.Join(bundleDir, entry.Name())
		if isDirEmpty(sub) {
			_ = os.RemoveAll(sub)
		}
	}
	if isDirEmpty(bundleDir) {
		if err := os.RemoveAll(bundleDir); err != nil {
			emit(reporter, EventWarn, "delete.prune", fmt.Sprintf("清理空目录 %s 失败: %v", bundleDir, err), nil)
			return
		}
		emit(reporter, EventInfo, "delete.prune",
			fmt.Sprintf("已移除空目录 %s%s", displayCacheDir(workspace), bundleName), nil)
	}
}

func isDirEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." || name == ".gitkeep" {
			continue
		}
		return false
	}
	return true
}
