package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
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

// DeleteCandidate 描述 Delete 页可选项。
type DeleteCandidate struct {
	Kind          DeleteItemKind
	Label         string
	Type          string
	Name          string
	Vault         string
	SecretPath    string // secrets：项目根相对落地路径，同时就是 Bitwarden Note 名
	SecretsBundle string // secrets / ssh：Bitwarden folder
	SSHKeyName    string // ssh：逻辑名
	DecBundleName string // ssh：用于本地 ~/.ssh/dec_<bundle>_<name>
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
	SecretsBundle string
	SSHKeyName    string
	DecBundleName string
	BundleName    string
	Members       []AssetSelectionItem
}

// DeleteProjectInput 描述 Delete 页批量删除输入。
type DeleteProjectInput struct {
	ProjectRoot string
	Items       []DeleteSelectionItem
	Confirmed   bool
}

// DeleteProjectResult 汇总 Delete 页批量删除结果。
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
	reporter = defaultReporter(reporter)
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return nil, fmt.Errorf("项目根目录不能为空")
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}

	var candidates []DeleteCandidate
	seenDec := make(map[string]struct{})
	groupCtx := newDeleteGroupContext(projectRoot, projectConfig)

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
		cachePath := getCachePath(projectRoot, vault, itemType, name)
		if cachePath == "" {
			return
		}
		if _, err := os.Stat(cachePath); err != nil {
			return
		}
		addDec(DeleteKindDecAsset, itemType, name, vault, false)
	}

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
		for _, bundleName := range projectConfig.EnabledBundles {
			dir := filepath.Join(projectRoot, ".dec", "cache", bundleName, spec.dir)
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

	_ = withAppReadRepo(func(tx *repo.Transaction) error {
		repoDir := tx.WorkDir()
		resolved, resolveErr := resolveDesiredAssets(projectConfig, repoDir, reporter)
		if resolveErr != nil {
			return resolveErr
		}
		bundles := collectEnabledBundleNames(projectConfig, resolved.Assets)
		for bundleName := range bundles {
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
				cachePath := getCachePath(projectRoot, bundleName, itemType, name)
				if _, err := os.Stat(cachePath); os.IsNotExist(err) {
					vaultPath := resolveAssetFile(repoDir, bundleName, itemType, name)
					if vaultPath == "" {
						continue
					}
					if _, err := os.Stat(vaultPath); err == nil {
						addDec(DeleteKindDecAsset, itemType, name, bundleName, true)
					}
				}
			}
		}
		return nil
	})

	// secrets 候选项只能来自远端 folder 的 note 列表：落地路径就是消费者路径，
	// 散在项目根，无法靠扫目录区分「这是 dec 管的密文件」和「这是项目自己的文件」。
	// SSH Key 同理：以远端 folder 枚举为准，本地仅用于 Orphan 标记。
	seenSecret := make(map[string]struct{})
	addSecret := func(secretsBundle, notePath string, localExists bool) {
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

	if includeRemote {
		if err := appendRemoteSecretCandidates(ctx, projectRoot, projectConfig, addSecret, addSSHKey, reporter); err != nil {
			return nil, err
		}
	}

	if state, loadErr := LoadAssetSelection(projectRoot, reporter); loadErr == nil {
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

func newDeleteGroupContext(projectRoot string, projectConfig *types.ProjectConfig) *deleteGroupContext {
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
			projectName, _ := ResolveProjectName(projectRoot, projectConfig)
			ctx.projectName = projectName
			if name, enabled := cfg.ResolveProjectSecrets(projectName); enabled {
				ctx.projectSecrets = name
				ctx.secretsToDec[name] = secrets.ProjectSecretsDecBundleName
			}
		}
	}
	if ctx.projectName == "" {
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

func appendRemoteSecretCandidates(
	ctx context.Context,
	projectRoot string,
	projectConfig *types.ProjectConfig,
	addSecret func(secretsBundle, notePath string, localExists bool),
	addSSHKey func(secretsBundle, decBundleName, keyName string, localExists bool),
	reporter Reporter,
) error {
	reporter = defaultReporter(reporter)
	configured, err := secrets.IsConfigured()
	if err != nil {
		return fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		return nil
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		return err
	}
	if !secrets.HasSession() {
		emit(reporter, EventInfo, "delete.secrets", "[auth] delete scan: Bitwarden session required", nil)
		if err := ensureBitwardenSession(ctx, reporter, "delete.secrets"); err != nil {
			return err
		}
	}
	if !secrets.HasUserKey() {
		return fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}

	client := secretsClientFactory()
	plan, err := planSecretsSync(projectRoot, projectConfig.EnabledBundles, cfg)
	if err != nil {
		return err
	}

	scanRemote := func(decBundleName string, binding secrets.BundleBinding) error {
		folder := secrets.FolderNameFor(binding, decBundleName)
		notes, listErr := client.ListFolderNotes(ctx, folder)
		if listErr != nil {
			return listErr
		}
		for _, note := range notes {
			localExists := false
			if _, statErr := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(note.Name))); statErr == nil {
				localExists = true
			}
			addSecret(folder, note.Name, localExists)
		}
		keys, listKeysErr := client.ListFolderSSHKeys(ctx, folder)
		if listKeysErr != nil {
			return listKeysErr
		}
		for _, key := range keys {
			localExists, existsErr := secrets.LocalSSHKeyExists(decBundleName, key.Name)
			if existsErr != nil {
				return existsErr
			}
			addSSHKey(folder, decBundleName, key.Name, localExists)
		}
		return nil
	}

	for _, bundleName := range plan.EnabledBundles {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := scanRemote(bundleName, cfg.ResolveBinding(bundleName)); err != nil {
			return fmt.Errorf("列出远端 secrets bundle %q 失败: %w", bundleName, err)
		}
	}
	if plan.ProjectSecretsName != "" {
		if err := ctx.Err(); err != nil {
			return err
		}
		binding := secrets.ProjectSecretsBinding(plan.ProjectSecretsName)
		if err := scanRemote(secrets.ProjectSecretsDecBundleName, binding); err != nil {
			return fmt.Errorf("列出远端 project secrets %q 失败: %w", plan.ProjectSecretsName, err)
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

// DeleteProjectItems 执行 Delete 页选中的删除（Dec vault + cache + IDE；secrets 本地 + Bitwarden）。
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
				BundleName:  bundleName,
				Members:     append([]AssetSelectionItem(nil), item.Members...),
				Confirmed:   true,
			}, reporter)
			if err != nil {
				return nil, err
			}
			result.BundlesDeleted++
			if bundleResult.VersionCommit != "" {
				lastCommit = bundleResult.VersionCommit
			}
		case DeleteKindDecAsset:
			emit(reporter, EventInfo, "delete.dec", fmt.Sprintf("删除 [%s] %s", item.Type, item.Name), nil)
			assetResult, err := RemoveAsset(RemoveAssetInput{
				ProjectRoot: input.ProjectRoot,
				Type:        item.Type,
				Name:        item.Name,
				Vault:       item.Vault,
				Confirmed:   true,
			}, reporter)
			if err != nil {
				return nil, err
			}
			result.DecDeleted++
			if assetResult.VersionCommit != "" {
				lastCommit = assetResult.VersionCommit
			}
		case DeleteKindSecret:
			emit(reporter, EventInfo, "delete.secrets", fmt.Sprintf("删除 secret %s", item.SecretPath), nil)
			if err := deleteSecretItem(ctx, input.ProjectRoot, item.SecretsBundle, item.SecretPath, reporter); err != nil {
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

func deleteSecretItem(ctx context.Context, projectRoot, secretsBundleName, notePath string, reporter Reporter) error {
	notePath = strings.TrimSpace(notePath)
	if notePath == "" {
		return fmt.Errorf("Secure Note 路径不能为空")
	}
	localPath := filepath.Join(projectRoot, filepath.FromSlash(notePath))
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
	tx, err := repo.NewReadTransaction()
	if err != nil {
		return err
	}
	defer tx.Close()
	return fn(tx)
}
