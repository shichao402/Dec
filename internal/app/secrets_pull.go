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

// planWorkspaceSecretsBrowse 为 Remote / secrets 元数据浏览规划 SyncTarget（ADR 0004）。
// 与 pull 不同：覆盖「包内外」——启用包 ∪ 本地同步根已有 bundle 目录 ∪ vault 同平面包
// ∪ 本机 known_secret_bundles。pull 仍只用 enabled，避免把未启用包的远端 Note 拉进工作区。
//
// known_secret_bundles 是「本机见过的 secrets 包」，里面混着两个平面的名字，
// 因此要减掉 vault 里明确属于另一平面的包（ADR 0009 平面隔离）。
func planWorkspaceSecretsBrowse(workspace Workspace, enabledBundles []string, cfg *secrets.Config, reporter Reporter) (*secretsSyncPlan, error) {
	reporter = defaultReporter(reporter)
	scopes := loadVaultBundleScopes(workspace, reporter)
	names := make(map[string]struct{})
	add := func(list []string) {
		for _, n := range config.NormalizeBundleNames(list) {
			if n == "" {
				continue
			}
			names[n] = struct{}{}
		}
	}
	addUnclaimed := func(list []string) {
		for _, n := range config.NormalizeBundleNames(list) {
			if n == "" || scopes.belongsToOtherPlane(n) {
				continue
			}
			names[n] = struct{}{}
		}
	}
	add(enabledBundles)
	add(listLocalSecretBundleNames(workspace))
	add(scopes.inPlaneNames())
	addUnclaimed(cfg.KnownSecretBundleNames())

	browse := make([]string, 0, len(names))
	for n := range names {
		browse = append(browse, n)
	}
	sort.Strings(browse)
	return planWorkspaceSecretsSync(workspace, browse, cfg)
}

// vaultBundleScopes 记录 vault 中 bundle 名的平面归属，用于 ADR 0009 平面隔离。
// 不在 vault 里的名字两边都不属于——它是「无主」的，当前平面可以看见并清理。
type vaultBundleScopes struct {
	inPlane    map[string]struct{}
	otherPlane map[string]struct{}
}

func (s vaultBundleScopes) belongsToOtherPlane(name string) bool {
	if _, ok := s.inPlane[name]; ok {
		return false
	}
	_, ok := s.otherPlane[name]
	return ok
}

func (s vaultBundleScopes) inPlaneNames() []string {
	names := make([]string, 0, len(s.inPlane))
	for name := range s.inPlane {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// loadVaultBundleScopes 一次扫描 vault，按 scope 把 bundle 名分到本平面 / 另一平面（LocalRead，不 Fetch）。
func loadVaultBundleScopes(workspace Workspace, reporter Reporter) vaultBundleScopes {
	scopes := vaultBundleScopes{
		inPlane:    make(map[string]struct{}),
		otherPlane: make(map[string]struct{}),
	}
	_ = withAppReadRepo(func(tx *repo.Transaction) error {
		vaultBundles, _, scanErr := scanVaultBundles(tx.WorkDir(), reporter)
		if scanErr != nil {
			emit(reporter, EventWarn, "secrets.browse", "扫描 vault bundles 失败（secrets 浏览不含 vault 包）: "+scanErr.Error(), nil)
			return nil
		}
		wantScope := bundleScopeForPlane(workspace.EffectivePlane())
		for name, matches := range vaultBundles {
			for _, match := range matches {
				if match.bundle.Scope == wantScope {
					scopes.inPlane[name] = struct{}{}
				} else {
					scopes.otherPlane[name] = struct{}{}
				}
			}
		}
		return nil
	})
	return scopes
}

// listLocalSecretBundleNames 扫描本机同步根下已有的 bundles/<name> 目录（停用后残留也要能在 Remote 里删）。
func listLocalSecretBundleNames(workspace Workspace) []string {
	var root string
	if workspace.EffectivePlane() == WorkspaceUser {
		machineRoot, err := secrets.MachineSecretsRoot()
		if err != nil {
			return nil
		}
		root = filepath.Join(machineRoot, filepath.FromSlash(secrets.MachineBundleSecretsRelPrefix))
	} else {
		projectRoot := strings.TrimSpace(workspace.Root)
		if projectRoot == "" {
			return nil
		}
		root = filepath.Join(projectRoot, filepath.FromSlash(secrets.BundleSecretsLocalRelPrefix))
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || name == "" || name[0] == '.' {
			continue
		}
		names = append(names, name)
	}
	return names
}

// listVaultInPlaneBundleNames 返回 vault 内当前平面的 bundle 名（LocalRead，不 Fetch）。
func listVaultInPlaneBundleNames(workspace Workspace, reporter Reporter) []string {
	return loadVaultBundleScopes(workspace, reporter).inPlaneNames()
}

// discoverRemoteSecretTargets 补上「Bitwarden 上有、但本机名单没有」的 folder。
//
// ADR 0004（修订）：Remote 是上下文无关的完整远端浏览器——枚举全部 folder
// （`bundle/*` + 裸名如 Dec / relkit），不再按当前平面 / scope 过滤可见性。
// LocalRoot 仅在能推断时填充（供「本地是否存在」标注）；裸 folder 可无 LocalRoot。
// 故意不 RememberSecretBundles：浏览不写回 known。
func discoverRemoteSecretTargets(
	ctx context.Context,
	client secrets.Client,
	workspace Workspace,
	scopes vaultBundleScopes,
	planned []secrets.SyncTarget,
	reporter Reporter,
) []secrets.SyncTarget {
	reporter = defaultReporter(reporter)
	folders, err := client.ListAllFolderNames(ctx)
	if err != nil {
		emit(reporter, EventWarn, "delete.secrets",
			fmt.Sprintf("枚举 Bitwarden folder 失败（仅展示已知包）: %v", err), nil)
		return nil
	}
	covered := make(map[string]struct{}, len(planned))
	for _, target := range planned {
		covered[target.Folder] = struct{}{}
	}

	var extra []secrets.SyncTarget
	for _, folder := range folders {
		folder = strings.TrimSpace(folder)
		if folder == "" {
			continue
		}
		if _, ok := covered[folder]; ok {
			continue
		}
		target, buildErr := remoteInventoryTarget(folder, workspace, scopes)
		if buildErr != nil {
			emit(reporter, EventWarn, "delete.secrets",
				fmt.Sprintf("跳过远端 folder %s: %v", folder, buildErr), nil)
			continue
		}
		covered[folder] = struct{}{}
		extra = append(extra, target)
	}
	if len(extra) > 0 {
		emit(reporter, EventInfo, "delete.secrets",
			fmt.Sprintf("发现 %d 个本机名单外的远端 folder", len(extra)), nil)
	}
	return extra
}

// remoteInventoryTarget 为 Remote 全量库存构造 SyncTarget。
// scope 只影响 LocalRoot 推断（元数据），不决定是否可见。
func remoteInventoryTarget(folder string, workspace Workspace, scopes vaultBundleScopes) (secrets.SyncTarget, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return secrets.SyncTarget{}, fmt.Errorf("folder 为空")
	}
	if strings.HasPrefix(folder, secrets.BundleFolderPrefix) {
		name := strings.TrimSpace(strings.TrimPrefix(folder, secrets.BundleFolderPrefix))
		if name == "" {
			return secrets.SyncTarget{}, fmt.Errorf("非法 bundle folder %q", folder)
		}
		// 按 vault scope 选 LocalRoot 平面；无主名字跟当前工作区平面走。
		useMachine := workspace.EffectivePlane() == WorkspaceUser
		if scopes.belongsToOtherPlane(name) {
			useMachine = !useMachine
		}
		if useMachine {
			return secrets.NewMachineBundleSyncTarget(name, folder)
		}
		return secrets.NewBundleSyncTarget(name, folder)
	}
	// 裸 folder：project 级远端节点；不强制 LocalRoot（其它项目 / 非当前工作区）。
	return secrets.SyncTarget{
		Kind:   secrets.SyncKindProject,
		Name:   folder,
		Folder: folder,
	}, nil
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
	Orphans       OrphanCleanupReport
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
// 停用包不清理；对本次成功对照过远端的启用 SyncTarget，会 prune 本地孤儿 Note/SSH。
func pullEnabledSecretsBundles(ctx context.Context, projectRoot string, enabledBundles []string, reporter Reporter) (*secretsPullSummary, error) {
	return pullEnabledSecretsBundlesForWorkspace(ctx, NewWorkspace(WorkspaceProject, projectRoot), enabledBundles, reporter)
}

func pullEnabledSecretsBundlesForWorkspace(ctx context.Context, workspace Workspace, enabledBundles []string, reporter Reporter) (*secretsPullSummary, error) {
	reporter = defaultReporter(reporter)
	projectRoot := workspace.Root
	summary := &secretsPullSummary{}
	secretsConfirmed := make(map[string]bool)
	vaultPresent, vaultScanOK := vaultPresentInPlane(workspace, reporter)

	finishMissingVault := func() {
		if !vaultScanOK {
			if len(enabledBundles) > 0 {
				emit(reporter, EventInfo, "pull.reconcile",
					"跳过 vault 缺失包收敛（本轮未能确认 vault 目录）", nil)
			}
			emitOrphanCleanupSummary(summary.Orphans, reporter)
			return
		}
		missing := reconcileMissingVaultBundles(workspace, enabledBundles, vaultPresent, secretsConfirmed, reporter)
		summary.Orphans.merge(missing)
		emitOrphanCleanupSummary(summary.Orphans, reporter)
	}

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
		finishMissingVault()
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
		finishMissingVault()
		return summary, nil
	}

	if !secrets.HasSession() {
		if err := ensureBitwardenSession(ctx, reporter, "pull.secrets"); err != nil {
			finishMissingVault()
			return summary, err
		}
	}
	if !secrets.HasUserKey() {
		finishMissingVault()
		return summary, fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
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

		if mig, migErr := secrets.MigrateTypeDirNames(ctx, client, projectRoot, target); migErr != nil {
			return nil, fmt.Errorf("迁移 %s 点类型目录标识失败: %w", label, migErr)
		} else if mig != nil && (len(mig.RenamedNotes) > 0 || len(mig.RenamedSSH) > 0) {
			for _, line := range mig.RenamedNotes {
				emit(reporter, EventInfo, "pull.secrets", "  迁移 Note "+line, progress)
			}
			for _, line := range mig.RenamedSSH {
				emit(reporter, EventInfo, "pull.secrets", "  迁移 SSH "+line, progress)
			}
		}

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
			finishMissingVault()
			return summary, fmt.Errorf("拉取 %s 失败: %w", label, pullErr)
		}
		fetchedNotes[i] = notes
		if err := secrets.ValidateNoteTypePaths(noteRels(notes)); err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
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
		finishMissingVault()
		return summary, err
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
				fmt.Sprintf("  落地 %d 个 SSH Key: %s", len(names), strings.Join(names, ", ")), progress)
		}

		// 远端列表已成功取回 → 以远端为权威 prune 本地孤儿（停用包不在 plan 内，不会误清）。
		pruned := pruneOrphanSecretsForTarget(projectRoot, target, fetchedNotes[i], fetchedKeys[i], reporter)
		summary.Orphans.merge(pruned)
		if target.Kind == secrets.SyncKindBundle {
			secretsConfirmed[target.Name] = true
		}

		if len(paths) == 0 && len(fetchedKeys[i]) == 0 && target.Folder != "" {
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  Bitwarden folder %q 无 Secure Note / SSH Key 或 folder 不存在", target.Folder), progress)
		}
	}

	if workspace.EffectivePlane() == WorkspaceProject {
		warnUnignoredSecrets(projectRoot, summary.LandingPaths, reporter)
	}

	finishMissingVault()

	orphanN := len(summary.Orphans.RemovedSecretPaths) + len(summary.Orphans.RemovedSSHKeys)
	if summary.NoteCount == 0 && summary.SSHKeyCount == 0 && summary.HandlerCount == 0 && orphanN == 0 {
		emit(reporter, EventInfo, "pull.secrets", "secrets 同步完成（无变更）", &Progress{Phase: "secrets", Current: total, Total: total})
	} else {
		msg := fmt.Sprintf("secrets 同步完成：%d 个文件 · %d 个 SSH Key", summary.NoteCount, summary.SSHKeyCount)
		if summary.HandlerCount > 0 {
			msg += fmt.Sprintf(" · %d 个 handler", summary.HandlerCount)
		}
		if orphanN > 0 {
			msg += fmt.Sprintf(" · 清理 %d 项孤儿", orphanN)
		}
		emit(reporter, EventInfo, "pull.secrets", msg,
			&Progress{Phase: "secrets", Current: total, Total: total})
	}
	return summary, nil
}

// vaultPresentInPlane 返回当前平面 vault 中存在的 bundle 名集合（LocalRead）。
// ok=false 表示本轮未能确认 vault（未连接 / 扫描失败），调用方不得据此做「远端已删」收敛。
func vaultPresentInPlane(workspace Workspace, reporter Reporter) (map[string]struct{}, bool) {
	present := make(map[string]struct{})
	ok := false
	err := withAppReadRepo(func(tx *repo.Transaction) error {
		ok = true
		vaultBundles, _, scanErr := scanVaultBundles(tx.WorkDir(), reporter)
		if scanErr != nil {
			emit(reporter, EventWarn, "pull.reconcile", "扫描 vault bundles 失败: "+scanErr.Error(), nil)
			ok = false
			return nil
		}
		wantScope := bundleScopeForPlane(workspace.EffectivePlane())
		for name, matches := range vaultBundles {
			for _, match := range matches {
				if match.bundle.Scope == wantScope {
					present[name] = struct{}{}
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		emit(reporter, EventWarn, "pull.reconcile",
			fmt.Sprintf("读取 vault 失败（跳过缺失包收敛）: %v", err), nil)
		return present, false
	}
	return present, ok
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
