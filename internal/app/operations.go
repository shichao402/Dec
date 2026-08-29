package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/ide"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
	"github.com/shichao402/Dec/internal/vars"
)

type PullProjectAssetsResult struct {
	ProjectRoot        string
	RequestedCount     int
	PulledCount        int
	FailedCount        int
	SkippedReason      string
	EffectiveIDEs      []string
	IDEWarnings        []string
	ValidationWarnings []string
	MigrationNotes     []string
	CleanedAssets      []string
	VersionCommit      string
	NonFatalWarnings   []string
	// BundleOverviews 记录本轮解析时发现的所有 bundle（含未启用的），供 CLI / TUI 呈现。
	BundleOverviews []BundleOverview
	// MissingBundles 是 enabled_bundles 里引用了、但当前平面的 vault 中已找不到声明的 bundle。
	MissingBundles []string
	// AssetSources 以 "type:vault:name" 为 key，值是每个目标资产的来源 bundle 列表
	// （例如 ["bundle/vikunja"]）。供多来源追溯使用。
	AssetSources         map[string][]string
	SecretsSkippedReason string
	SecretsNoteCount     int
	SecretsSSHKeyCount   int
	// 孤儿清理：Git 资产见 CleanedAssets；secrets/SSH/登记见下列字段。
	OrphanSecretPaths    []string
	OrphanSSHKeys        []string
	OrphanClearedBundles []string
	OrphanReportedOnly   []string
	// ADR 0016 structured result. Bundle fields remain for compatible clients.
	Model            string
	HomeProject      string
	SelectedProjects []string
	RequiredProjects []string
	MissingProjects  []string
	Quadrants        map[string]int
}

func PullProjectAssets(ctx context.Context, projectRoot, version string, reporter Reporter) (*PullProjectAssetsResult, error) {
	return PullWorkspaceAssets(ctx, NewWorkspace(WorkspaceProject, projectRoot), version, reporter)
}

// PullWorkspaceAssets 拉取并安装当前工作空间平面的公开资产与 secrets。
func PullWorkspaceAssets(ctx context.Context, workspace Workspace, version string, reporter Reporter) (*PullProjectAssetsResult, error) {
	reporter = defaultReporter(reporter)
	projectRoot := workspace.Root
	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return nil, err
	}

	result := &PullProjectAssetsResult{
		ProjectRoot:  projectRoot,
		AssetSources: make(map[string][]string),
	}

	ideSelection, err := config.ResolveEffectiveIDEs(projectConfig)
	if err != nil {
		return nil, fmt.Errorf("解析有效 IDE 失败: %w", err)
	}
	result.IDEWarnings = append(result.IDEWarnings, ideSelection.Warnings...)
	for _, warning := range ideSelection.Warnings {
		emit(reporter, EventWarn, "pull.ide", warning, nil)
	}
	projectIDEs := uniqueWorkspaceIDEs(workspace, ideSelection.IDEs)
	result.EffectiveIDEs = projectIDENames(projectIDEs)

	var migrationNotes []string
	if workspace.EffectivePlane() == WorkspaceProject {
		migrationNotes, err = migrateLegacyProjectLayouts(projectRoot, projectIDEs)
		if err != nil {
			return nil, fmt.Errorf("迁移旧版项目布局失败: %w", err)
		}
	}
	result.MigrationNotes = append(result.MigrationNotes, migrationNotes...)
	for _, note := range migrationNotes {
		emit(reporter, EventInfo, "pull.migrate", note, nil)
	}

	// 平面隔离（ADR 0009）：project 上下文只处理项目启用列表，不再并入用户平面。
	projectEnabled := config.NormalizeBundleNames(projectConfig.EnabledBundles)
	pullConfig := *projectConfig
	pullConfig.EnabledBundles = projectEnabled

	if len(projectEnabled) == 0 {
		usesP := false
		if workspace.EffectivePlane() == WorkspaceProject && strings.TrimSpace(projectConfig.ProjectName) != "" {
			usesP, _ = connectedRepositoryUsesPModel()
		}
		if !usesP {
			result.SkippedReason = "未启用 bundle"
			emit(reporter, EventInfo, "pull.prepare", "请先在 Bundles 页勾选并保存", nil)
			applyAssetCleanup(result, workspace, nil, projectIDEs, reporter)
			return result, nil
		}
	}

	if err := migrateRemotePlanes(ctx, reporter); err != nil {
		return nil, fmt.Errorf("迁移远端平面目录失败: %w", err)
	}

	createTx := func() (*repo.Transaction, error) {
		if strings.TrimSpace(version) != "" {
			return repo.NewReadTransactionAt(version)
		}
		return repo.NewReadTransaction()
	}

	tx, err := createTx()
	if err != nil {
		return nil, err
	}
	defer tx.Close()

	repoDir := tx.WorkDir()

	resolved, err := resolveDesiredAssetsForPlane(&pullConfig, repoDir, workspace.EffectivePlane(), reporter)
	if err != nil {
		return nil, err
	}
	result.BundleOverviews = resolved.Bundles
	pRepository := repositoryUsesPModel(repoDir)
	if pRepository {
		result.Model = "p"
		result.Quadrants = map[string]int{
			"public/global": 0, "private/global": 0,
			"public/local": 0, "private/local": 0,
		}
		for _, overview := range resolved.Bundles {
			if overview.Home {
				result.HomeProject = overview.Name
			}
			if overview.Enabled {
				result.SelectedProjects = append(result.SelectedProjects, overview.Name)
			}
			if overview.Required {
				result.RequiredProjects = append(result.RequiredProjects, overview.Name)
			}
		}
		for _, asset := range resolved.Assets {
			result.Quadrants[string(asset.Visibility)+"/"+string(types.CanonicalAssetPlane(asset.Plane))]++
		}
		result.MissingProjects = append([]string(nil), resolved.MissingProjects...)
		if len(result.MissingProjects) > 0 {
			result.NonFatalWarnings = append(result.NonFatalWarnings,
				fmt.Sprintf("直接 requires 中有 %d 个项目不存在：%s",
					len(result.MissingProjects), strings.Join(result.MissingProjects, ", ")))
		}
	}
	effectiveEnabled := projectEnabled
	if pRepository && workspace.EffectivePlane() == WorkspaceProject {
		effectiveEnabled = []string{strings.TrimSpace(projectConfig.ProjectName)}
	}

	// 只发 reporter 事件不够：事件区只留最近几条，「引用的 bundle 已不在仓库」这类
	// 开头就发出的告警会被后续 secrets 事件挤掉，用户只看到一排 0 却不知道为什么。
	if missing := missingEnabledBundleNames(effectiveEnabled, resolved.Bundles); len(missing) > 0 {
		result.MissingBundles = missing
		if pRepository {
			for _, name := range missing {
				result.MissingProjects = appendUniqueSource(result.MissingProjects, name)
			}
		}
		result.NonFatalWarnings = append(result.NonFatalWarnings, fmt.Sprintf(
			"%s里有 %d 个项目在仓库中已不存在：%s（本次忽略；到 Bundles 页重新保存即可清掉）",
			func() string {
				if pRepository {
					return "项目选择"
				}
				return "enabled_bundles "
			}(), len(missing), strings.Join(missing, ", ")))
	}

	// bundle 解析阶段已校验过成员文件存在性，这里无需再做一次白名单过滤。
	validAssets := resolved.Assets

	// 对照最终目标集缩减 AssetSources，避免把被过滤掉的资产的来源带出。
	finalSources := make(map[string][]string, len(validAssets))
	for _, asset := range validAssets {
		key := assetKey(asset)
		finalSources[key] = append([]string(nil), resolved.Sources[key]...)
	}
	result.AssetSources = finalSources

	applyAssetCleanup(result, workspace, validAssets, projectIDEs, reporter)

	enabledBundleNames := append([]string(nil), effectiveEnabled...)
	if len(validAssets) == 0 {
		result.SkippedReason = "没有有效的已启用 Git 资产可拉取（仍尝试同步 secrets）"
		emit(reporter, EventInfo, "pull.prepare", result.SkippedReason, nil)
		if err := applySecretsPull(ctx, result, workspace, enabledBundleNames, reporter); err != nil {
			result.NonFatalWarnings = append(result.NonFatalWarnings, err.Error())
			emit(reporter, EventWarn, "pull.secrets", "Secrets 未同步（无公开资产可拉取）", nil)
			return result, nil
		}
		return result, nil
	}
	result.RequestedCount = len(validAssets)

	emit(reporter, EventInfo, "pull.start", fmt.Sprintf("📥 拉取 %d 个已启用资产", len(validAssets)), &Progress{Phase: "pull", Current: 0, Total: len(validAssets)})

	// 阶段 1：Dec Git 资产写入 .dec/cache/
	for idx, asset := range validAssets {
		progress := &Progress{Phase: "pull", Current: idx + 1, Total: len(validAssets)}
		fullPath := resolveTypedAssetFile(repoDir, asset)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			result.FailedCount++
			emit(reporter, EventWarn, "pull.asset", fmt.Sprintf("⚠️  [%-5s] %s (vault: %s) — 远程不存在", asset.Type, asset.Name, asset.Vault), progress)
			continue
		}

		cachePath := getWorkspaceTypedCachePath(workspace, asset)
		switch asset.Type {
		case "skill", "command":
			if err := copyDir(fullPath, cachePath); err != nil {
				result.FailedCount++
				emit(reporter, EventWarn, "pull.asset", fmt.Sprintf("⚠️  [%-5s] %s 缓存失败: %v", asset.Type, asset.Name, err), progress)
				continue
			}
		case "rule", "mcp":
			if err := copyFile(fullPath, cachePath); err != nil {
				result.FailedCount++
				emit(reporter, EventWarn, "pull.asset", fmt.Sprintf("⚠️  [%-5s] %s 缓存失败: %v", asset.Type, asset.Name, err), progress)
				continue
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 阶段 2：从 cache 渲染安装到 IDE，并执行非敏感 vars 替换
	for idx, asset := range validAssets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress := &Progress{Phase: "install", Current: idx + 1, Total: len(validAssets)}
		fullPath := resolveTypedAssetFile(repoDir, asset)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			continue
		}
		cachePath := getWorkspaceTypedCachePath(workspace, asset)
		if _, err := os.Stat(cachePath); err != nil {
			continue
		}

		if err := installAssetToIDEs(asset.Type, asset.Name, asset.Vault, fullPath, workspace, projectIDEs); err != nil {
			result.FailedCount++
			emit(reporter, EventWarn, "pull.asset", fmt.Sprintf("⚠️  [%-5s] %s (%v)", asset.Type, asset.Name, err), progress)
			continue
		}

		if workspace.EffectivePlane() == WorkspaceProject {
			substituteAssetVars(asset.Type, asset.Name, projectRoot, projectIDEs, mgr, reporter)
		}

		result.PulledCount++
		emit(reporter, EventInfo, "pull.asset", fmt.Sprintf("✅ [%-5s] %s (vault: %s)", asset.Type, asset.Name, asset.Vault), progress)
	}

	// 阶段 3：secrets 放在公开资产之后。Bitwarden 不可用（未解锁、网络故障）
	// 不应连累已经就绪的 skill / rule / mcp，否则一次解锁失败会让整个项目看起来没装过资产。
	// 契约：公开资产已落地时 secrets 失败 → result + NonFatalWarnings，error 为 nil。
	if err := applySecretsPull(ctx, result, workspace, enabledBundleNames, reporter); err != nil {
		emit(reporter, EventWarn, "pull.secrets",
			fmt.Sprintf("已安装 %d 个公开资产；Secrets 未同步", result.PulledCount), nil)
		result.NonFatalWarnings = append(result.NonFatalWarnings, err.Error())
		commitHash := tx.CommitHash()
		if commitHash != "" {
			result.VersionCommit = commitHash
			saveVersionMeta(workspaceCacheRoot(workspace), commitHash)
		}
		return result, nil
	}

	commitHash := tx.CommitHash()
	if commitHash != "" {
		result.VersionCommit = commitHash
		saveVersionMeta(workspaceCacheRoot(workspace), commitHash)
	}

	summary := fmt.Sprintf("✅ 完成：%d 个资产已拉取", result.PulledCount)
	if result.FailedCount > 0 {
		summary += fmt.Sprintf("，%d 个失败", result.FailedCount)
	}
	orphanN := len(result.CleanedAssets) + len(result.OrphanSecretPaths) + len(result.OrphanSSHKeys)
	if orphanN > 0 {
		summary += fmt.Sprintf("，清理 %d 项孤儿", orphanN)
	}
	if len(result.EffectiveIDEs) > 0 {
		summary += fmt.Sprintf(" (IDE: %s)", strings.Join(result.EffectiveIDEs, ", "))
	}
	emit(reporter, EventInfo, "pull.finish", summary, &Progress{Phase: "done", Current: len(validAssets), Total: len(validAssets)})

	return result, nil
}

func repositoryUsesPModel(repoDir string) bool {
	projects, err := pmodel.Scan(repoDir)
	return err == nil && len(projects) > 0
}

// missingEnabledBundleNames 返回启用列表里在本平面 vault 中找不到声明的 bundle 名（保序）。
func missingEnabledBundleNames(enabled []string, resolved []BundleOverview) []string {
	if len(enabled) == 0 {
		return nil
	}
	known := make(map[string]struct{}, len(resolved))
	for _, bo := range resolved {
		known[bo.Name] = struct{}{}
	}
	var missing []string
	for _, name := range enabled {
		if _, ok := known[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

func applyAssetCleanup(result *PullProjectAssetsResult, workspace Workspace, enabledAssets []types.TypedAssetRef, projectIDEs []ide.IDE, reporter Reporter) {
	result.CleanedAssets = cleanupRemovedAssets(workspace, enabledAssets, projectIDEs)
	if len(result.CleanedAssets) == 0 {
		return
	}
	emit(reporter, EventInfo, "pull.cleanup",
		fmt.Sprintf("🧹 清理 %d 个本地孤儿 Dec 资产（不在本次目标集：远端已删或未启用）", len(result.CleanedAssets)), nil)
	for _, asset := range result.CleanedAssets {
		emit(reporter, EventInfo, "pull.cleanup", asset, nil)
	}
}

func applySecretsPull(ctx context.Context, result *PullProjectAssetsResult, workspace Workspace, enabledBundles []string, reporter Reporter) error {
	if workspace.EffectivePlane() == WorkspaceProject {
		if err := secrets.EnsureSecretsGitignore(workspace.Root); err != nil {
			emit(reporter, EventWarn, "pull.secrets", fmt.Sprintf("写入 .gitignore 失败: %v", err), nil)
		}
	}
	secretsSummary, err := pullEnabledSecretsBundlesForWorkspace(ctx, workspace, enabledBundles, reporter)
	if secretsSummary != nil {
		result.SecretsSkippedReason = secretsSummary.SkippedReason
		result.SecretsNoteCount = secretsSummary.NoteCount
		result.SecretsSSHKeyCount = secretsSummary.SSHKeyCount
		result.OrphanSecretPaths = append(result.OrphanSecretPaths, secretsSummary.Orphans.RemovedSecretPaths...)
		result.OrphanSSHKeys = append(result.OrphanSSHKeys, secretsSummary.Orphans.RemovedSSHKeys...)
		result.OrphanClearedBundles = append(result.OrphanClearedBundles, secretsSummary.Orphans.ClearedBundles...)
		result.OrphanReportedOnly = append(result.OrphanReportedOnly, secretsSummary.Orphans.ReportedOnly...)
	}
	return err
}

func uniqueWorkspaceIDEs(workspace Workspace, ideNames []string) []ide.IDE {
	result := make([]ide.IDE, 0, len(ideNames))
	seen := make(map[string]struct{}, len(ideNames))
	home, _ := os.UserHomeDir()

	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)
		key := strings.Join([]string{
			filepath.Clean(ideImpl.SkillsDirForPlane(workspace.IDEPlane(), workspace.Root, home)),
			filepath.Clean(ideImpl.CommandsDirForPlane(workspace.IDEPlane(), workspace.Root, home)),
			filepath.Clean(ideImpl.RulesDirForPlane(workspace.IDEPlane(), workspace.Root, home)),
			filepath.Clean(ideImpl.MCPConfigPathForPlane(workspace.IDEPlane(), workspace.Root, home)),
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, ideImpl)
	}

	return result
}

func uniqueProjectIDEs(projectRoot string, ideNames []string) []ide.IDE {
	return uniqueWorkspaceIDEs(NewWorkspace(WorkspaceProject, projectRoot), ideNames)
}

func projectIDENames(projectIDEs []ide.IDE) []string {
	names := make([]string, 0, len(projectIDEs))
	for _, ideImpl := range projectIDEs {
		names = append(names, ideImpl.Name())
	}
	return names
}

func migrateLegacyProjectLayouts(projectRoot string, projectIDEs []ide.IDE) ([]string, error) {
	var notes []string
	needClaude := false
	needCodex := false

	claudeMCPPath := filepath.Join(projectRoot, ".claude", "mcp.json")
	codexMCPPath := filepath.Join(projectRoot, ".codex", "config.toml")
	for _, ideImpl := range projectIDEs {
		switch filepath.Clean(ideImpl.MCPConfigPath(projectRoot)) {
		case claudeMCPPath:
			needClaude = true
		case codexMCPPath:
			needCodex = true
		}
	}

	if needClaude {
		migrated, err := ide.MigrateLegacyClaudeProject(projectRoot)
		if err != nil {
			return nil, err
		}
		notes = append(notes, migrated...)
	}
	if needCodex {
		migrated, err := ide.MigrateLegacyCodexProject(projectRoot)
		if err != nil {
			return nil, err
		}
		notes = append(notes, migrated...)
	}

	return notes, nil
}

func cleanupRemovedAssets(workspace Workspace, enabledAssets []types.TypedAssetRef, projectIDEs []ide.IDE) []string {
	cacheDir := workspaceCacheDir(workspace)
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return nil
	}
	if hasPAssets(enabledAssets) || cacheUsesPLayout(cacheDir) {
		return cleanupRemovedPAssets(workspace, cacheDir, enabledAssets, projectIDEs)
	}

	enabledSet := make(map[string]bool)
	for _, asset := range enabledAssets {
		enabledSet[asset.Vault+":"+asset.Type+":"+asset.Name] = true
	}

	vaultDirs, _ := os.ReadDir(cacheDir)
	var removed []string
	for _, vaultDir := range vaultDirs {
		if !vaultDir.IsDir() {
			continue
		}
		vaultName := vaultDir.Name()
		for _, kind := range bundle.VaultAssetKinds {
			subDir := filepath.Join(cacheDir, vaultName, kind.Dir)
			entries, err := os.ReadDir(subDir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				name := bundle.AssetEntryName(kind, entry.Name())
				assetType := kind.Type

				key := vaultName + ":" + assetType + ":" + name
				if enabledSet[key] {
					continue
				}

				for _, ideImpl := range projectIDEs {
					_, _ = removeAssetFromIDE(assetType, name, workspace, ideImpl)
				}
				_ = os.RemoveAll(filepath.Join(subDir, entry.Name()))
				removed = append(removed, fmt.Sprintf("[%-5s] %s (vault: %s)", assetType, name, vaultName))
			}
			// 清理空的类型子目录（skills/rules/...）
			_ = removeDirIfEmpty(subDir)
		}
		// 清理空的 vault/bundle 缓存目录
		_ = removeDirIfEmpty(filepath.Join(cacheDir, vaultName))
	}

	sort.Strings(removed)
	return removed
}

func cacheUsesPLayout(cacheDir string) bool {
	projects, _ := os.ReadDir(cacheDir)
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		for _, visibility := range []types.AssetVisibility{types.AssetVisibilityPublic, types.AssetVisibilityPrivate} {
			if info, err := os.Stat(filepath.Join(cacheDir, project.Name(), string(visibility))); err == nil && info.IsDir() {
				return true
			}
		}
	}
	return false
}

func cleanupRemovedPAssets(workspace Workspace, cacheDir string, enabledAssets []types.TypedAssetRef, projectIDEs []ide.IDE) []string {
	enabled := make(map[string]struct{}, len(enabledAssets))
	for _, asset := range enabledAssets {
		enabled[assetKey(asset)] = struct{}{}
	}
	var removed []string
	projects, _ := os.ReadDir(cacheDir)
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		for _, visibility := range []types.AssetVisibility{types.AssetVisibilityPublic, types.AssetVisibilityPrivate} {
			for _, plane := range []types.AssetPlane{types.AssetPlaneUser, types.AssetPlaneProject} {
				for _, kind := range bundle.VaultAssetKinds {
					dir := filepath.Join(cacheDir, project.Name(), string(visibility), string(plane), kind.Dir)
					entries, err := os.ReadDir(dir)
					if err != nil {
						continue
					}
					for _, entry := range entries {
						name := bundle.AssetEntryName(kind, entry.Name())
						asset := types.TypedAssetRef{
							Type: kind.Type, Visibility: visibility, Plane: plane,
							AssetRef: types.AssetRef{Name: name, Vault: project.Name()},
						}
						if _, ok := enabled[assetKey(asset)]; ok {
							continue
						}
						for _, ideImpl := range projectIDEs {
							_, _ = removeAssetFromIDE(kind.Type, name, workspace, ideImpl)
						}
						_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
						removed = append(removed, fmt.Sprintf("[%-5s] %s (项目: %s, %s/%s)", kind.Type, name, project.Name(), visibility, plane))
					}
				}
			}
		}
	}
	sort.Strings(removed)
	return removed
}

func removeDirIfEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	return os.Remove(dir)
}

func typeSubDir(itemType string) string {
	return bundle.TypeSubDir(itemType)
}

func resolveAssetFile(repoDir, vault, itemType, assetName string) string {
	kind, ok := bundle.KindByType(itemType)
	if !ok {
		return ""
	}
	base := filepath.Join(repoDir, types.VaultBundlesDir, vault, kind.Dir)
	return filepath.Join(base, bundle.AssetFileName(kind, assetName))
}

func planeDiskNames(plane types.AssetPlane) []string {
	switch types.CanonicalAssetPlane(plane) {
	case types.AssetPlaneGlobal:
		return []string{string(types.AssetPlaneGlobal), string(types.AssetPlaneUser)}
	default:
		return []string{string(types.AssetPlaneLocal), string(types.AssetPlaneProject)}
	}
}

func firstExistingPath(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
}

func resolveTypedAssetFile(repoDir string, asset types.TypedAssetRef) string {
	if asset.Visibility == "" || asset.Plane == "" {
		return resolveAssetFile(repoDir, asset.Vault, asset.Type, asset.Name)
	}
	kind, ok := bundle.KindByType(asset.Type)
	if !ok {
		return ""
	}
	var candidates []string
	for _, disk := range planeDiskNames(asset.Plane) {
		base := filepath.Join(repoDir, asset.Vault, string(asset.Visibility), disk, kind.Dir)
		candidates = append(candidates, filepath.Join(base, bundle.AssetFileName(kind, asset.Name)))
	}
	return firstExistingPath(candidates...)
}

func getCachePath(projectRoot, vault, itemType, assetName string) string {
	return getWorkspaceCachePath(NewWorkspace(WorkspaceProject, projectRoot), vault, itemType, assetName)
}

func workspaceCacheDir(workspace Workspace) string {
	if workspace.EffectivePlane() == WorkspaceUser {
		root, err := repo.GetRootDir()
		if err == nil {
			return filepath.Join(root, "cache")
		}
	}
	return filepath.Join(workspace.Root, ".dec", "cache")
}

// displayCacheDir 给日志用的缓存目录短名，避免把用户 home 绝对路径打进 TUI。
func displayCacheDir(workspace Workspace) string {
	if workspace.EffectivePlane() == WorkspaceUser {
		return "~/.dec/cache/"
	}
	return ".dec/cache/"
}

func workspaceCacheRoot(workspace Workspace) string {
	if workspace.EffectivePlane() == WorkspaceUser {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	return workspace.Root
}

func getWorkspaceCachePath(workspace Workspace, vault, itemType, assetName string) string {
	kind, ok := bundle.KindByType(itemType)
	if !ok {
		return ""
	}
	base := filepath.Join(workspaceCacheDir(workspace), vault, kind.Dir)
	return filepath.Join(base, bundle.AssetFileName(kind, assetName))
}

func getWorkspaceTypedCachePath(workspace Workspace, asset types.TypedAssetRef) string {
	if asset.Visibility == "" || asset.Plane == "" {
		return getWorkspaceCachePath(workspace, asset.Vault, asset.Type, asset.Name)
	}
	kind, ok := bundle.KindByType(asset.Type)
	if !ok {
		return ""
	}
	var candidates []string
	for _, disk := range planeDiskNames(asset.Plane) {
		base := filepath.Join(workspaceCacheDir(workspace), asset.Vault, string(asset.Visibility), disk, kind.Dir)
		candidates = append(candidates, filepath.Join(base, bundle.AssetFileName(kind, asset.Name)))
	}
	return firstExistingPath(candidates...)
}

func managedName(name string) string {
	if strings.HasPrefix(name, "dec-") {
		return name
	}
	return "dec-" + name
}

func installAssetToIDEs(itemType, assetName, vaultName, srcPath string, workspace Workspace, projectIDEs []ide.IDE) error {
	installed := make([]ide.IDE, 0, len(projectIDEs))

	for _, ideImpl := range projectIDEs {
		if err := installAssetToIDEForWorkspace(itemType, assetName, vaultName, srcPath, workspace, ideImpl); err != nil {
			rollbackErrors := rollbackInstalledAsset(itemType, assetName, workspace, installed)
			if len(rollbackErrors) > 0 {
				return fmt.Errorf("安装到 %s 失败: %v；回滚失败: %s", ideImpl.Name(), err, strings.Join(rollbackErrors, "; "))
			}
			return fmt.Errorf("安装到 %s 失败: %w", ideImpl.Name(), err)
		}
		installed = append(installed, ideImpl)
	}

	return nil
}

func rollbackInstalledAsset(itemType, assetName string, workspace Workspace, installed []ide.IDE) []string {
	var rollbackErrors []string
	for i := len(installed) - 1; i >= 0; i-- {
		ideImpl := installed[i]
		removed, err := removeAssetFromIDE(itemType, assetName, workspace, ideImpl)
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("%s: %v", ideImpl.Name(), err))
		} else if !removed {
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("%s: 未找到已安装资产", ideImpl.Name()))
		}
	}
	return rollbackErrors
}

func installAssetToIDE(itemType, assetName, vaultName, srcPath, projectRoot string, ideImpl ide.IDE) error {
	return installAssetToIDEForWorkspace(itemType, assetName, vaultName, srcPath, NewWorkspace(WorkspaceProject, projectRoot), ideImpl)
}

func installAssetToIDEForWorkspace(itemType, assetName, vaultName, srcPath string, workspace Workspace, ideImpl ide.IDE) error {
	managed := managedName(assetName)
	home, _ := os.UserHomeDir()
	plane := workspace.IDEPlane()
	projectRoot := workspace.Root

	switch itemType {
	case "skill":
		destDir := filepath.Join(ideImpl.SkillsDirForPlane(plane, projectRoot, home), managed)
		if err := copyDir(srcPath, destDir); err != nil {
			return err
		}
		return injectRenderedHeaderDir(destDir, vaultName)
	case "command":
		destDir := filepath.Join(ideImpl.CommandsDirForPlane(plane, projectRoot, home), managed)
		if err := copyDir(srcPath, destDir); err != nil {
			return err
		}
		return injectRenderedHeaderDir(destDir, vaultName)
	case "rule":
		destDir := ideImpl.RulesDirForPlane(plane, projectRoot, home)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return err
		}
		destPath := filepath.Join(destDir, managed+".mdc")
		if err := copyFile(srcPath, destPath); err != nil {
			return err
		}
		return injectRenderedHeaderFile(destPath, vaultName)
	case "mcp":
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("读取 MCP 配置失败: %w", err)
		}
		var server types.MCPServer
		if err := json.Unmarshal(data, &server); err != nil {
			return fmt.Errorf("解析 MCP 配置失败: %w", err)
		}
		cmd, args := stripExternalEnvLauncher(server.Command, server.Args)
		server.Command, server.Args, server.Env = WrapMCPServerWithExecForPlane(projectRoot, vaultName, workspace.SecretsPlane(), "dec-exec", cmd, args, server.Env)
		existingConfig, err := ideImpl.LoadMCPConfigForPlane(plane, projectRoot, home)
		if err != nil {
			return fmt.Errorf("加载 IDE MCP 配置失败: %w", err)
		}
		if existingConfig.MCPServers == nil {
			existingConfig.MCPServers = make(map[string]types.MCPServer)
		}
		existingConfig.MCPServers[managed] = server
		return ideImpl.WriteMCPConfigForPlane(plane, projectRoot, home, existingConfig)
	default:
		return nil
	}
}

func removeAssetFromIDE(itemType, assetName string, workspace Workspace, ideImpl ide.IDE) (bool, error) {
	managed := managedName(assetName)
	home, _ := os.UserHomeDir()
	plane := workspace.IDEPlane()
	projectRoot := workspace.Root

	switch itemType {
	case "skill":
		destDir := filepath.Join(ideImpl.SkillsDirForPlane(plane, projectRoot, home), managed)
		if _, err := os.Stat(destDir); os.IsNotExist(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, os.RemoveAll(destDir)
	case "command":
		destDir := filepath.Join(ideImpl.CommandsDirForPlane(plane, projectRoot, home), managed)
		if _, err := os.Stat(destDir); os.IsNotExist(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, os.RemoveAll(destDir)
	case "rule":
		destPath := filepath.Join(ideImpl.RulesDirForPlane(plane, projectRoot, home), managed+".mdc")
		if err := os.Remove(destPath); os.IsNotExist(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, nil
	case "mcp":
		existingConfig, err := ideImpl.LoadMCPConfigForPlane(plane, projectRoot, home)
		if err != nil {
			return false, nil
		}
		if _, exists := existingConfig.MCPServers[managed]; !exists {
			return false, nil
		}
		delete(existingConfig.MCPServers, managed)
		return true, ideImpl.WriteMCPConfigForPlane(plane, projectRoot, home, existingConfig)
	default:
		return false, nil
	}
}

func substituteAssetVars(itemType, assetName, projectRoot string, projectIDEs []ide.IDE, mgr *config.ProjectConfigManager, reporter Reporter) {
	globalVars, err := config.LoadGlobalVars()
	if err != nil {
		emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("读取全局变量失败: %v", err), nil)
		globalVars = nil
	}
	projectVars, err := mgr.LoadVarsConfig()
	if err != nil {
		emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("解析 %s 失败: %v", mgr.GetVarsPath(), err), nil)
		projectVars = nil
	}
	projectVarsPath := mgr.GetVarsPath()
	globalVarsPath, _ := config.GetGlobalVarsPath()

	if (globalVars == nil || len(globalVars.Vars) == 0) && (projectVars == nil || len(projectVars.Vars) == 0) {
		if globalVars != nil && globalVars.Assets != nil {
			// 可能有资产级变量，继续。
		} else if projectVars != nil && projectVars.Assets != nil {
			// 可能有资产级变量，继续。
		} else {
			return
		}
	}

	for _, ideImpl := range projectIDEs {
		ideName := ideImpl.Name()

		switch itemType {
		case "skill":
			localPath := filepath.Join(ideImpl.SkillsDir(projectRoot), managedName(assetName))
			placeholders := vars.ExtractPlaceholdersFromDir(localPath)
			locations := vars.ExtractPlaceholderLocationsFromDir(localPath)
			if len(placeholders) == 0 {
				continue
			}
			resolved := vars.ResolveVars(globalVars, projectVars, itemType, assetName, placeholders)
			_, missing, err := vars.SubstituteDir(localPath, resolved)
			if err != nil {
				emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("变量替换失败 (%s): %v", ideName, err), nil)
				continue
			}
			emitMissingVars(reporter, itemType, assetName, missing, locations, projectVarsPath, globalVarsPath)
		case "command":
			localPath := filepath.Join(ideImpl.CommandsDir(projectRoot), managedName(assetName))
			placeholders := vars.ExtractPlaceholdersFromDir(localPath)
			locations := vars.ExtractPlaceholderLocationsFromDir(localPath)
			if len(placeholders) == 0 {
				continue
			}
			resolved := vars.ResolveVars(globalVars, projectVars, itemType, assetName, placeholders)
			_, missing, err := vars.SubstituteDir(localPath, resolved)
			if err != nil {
				emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("变量替换失败 (%s): %v", ideName, err), nil)
				continue
			}
			emitMissingVars(reporter, itemType, assetName, missing, locations, projectVarsPath, globalVarsPath)
		case "rule":
			localPath := filepath.Join(ideImpl.RulesDir(projectRoot), managedName(assetName)+".mdc")
			placeholders := vars.ExtractPlaceholdersFromFile(localPath)
			locations := vars.ExtractPlaceholderLocationsFromFile(localPath)
			if len(placeholders) == 0 {
				continue
			}
			resolved := vars.ResolveVars(globalVars, projectVars, itemType, assetName, placeholders)
			_, missing, err := vars.SubstituteFile(localPath, resolved)
			if err != nil {
				emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("变量替换失败 (%s): %v", ideName, err), nil)
				continue
			}
			emitMissingVars(reporter, itemType, assetName, missing, locations, projectVarsPath, globalVarsPath)
		case "mcp":
			_, missing, locations := substituteMCPVars(assetName, projectRoot, ideImpl, globalVars, projectVars, reporter)
			emitMissingVars(reporter, itemType, assetName, missing, locations, projectVarsPath, globalVarsPath)
		}
	}
}

func substituteMCPVars(assetName, projectRoot string, ideImpl ide.IDE, globalVars, projectVars *types.VarsConfig, reporter Reporter) (map[string]string, []string, map[string][]string) {
	managed := managedName(assetName)
	configPath := ideImpl.MCPConfigPath(projectRoot)

	existingConfig, err := ideImpl.LoadMCPConfig(projectRoot)
	if err != nil {
		emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("加载 MCP 配置失败: %v", err), nil)
		return nil, nil, nil
	}

	server, ok := existingConfig.MCPServers[managed]
	if !ok {
		return nil, nil, nil
	}

	var allContent string
	if server.Env != nil {
		for _, value := range server.Env {
			allContent += value + "\n"
		}
	}
	for _, arg := range server.Args {
		allContent += arg + "\n"
	}
	allContent += server.Command

	placeholders := vars.ExtractPlaceholders(allContent)
	if len(placeholders) == 0 {
		return nil, nil, nil
	}

	locations := make(map[string][]string, len(placeholders))
	for _, placeholder := range placeholders {
		locations[placeholder] = []string{configPath}
	}

	resolved := vars.ResolveVars(globalVars, projectVars, "mcp", assetName, placeholders)
	used := make(map[string]string)
	var missing []string

	if server.Env != nil {
		newEnv := make(map[string]string)
		for key, value := range server.Env {
			newVal, usedVars, missingVars := vars.Substitute(value, resolved)
			newEnv[key] = newVal
			for usedKey, usedValue := range usedVars {
				used[usedKey] = usedValue
			}
			missing = append(missing, missingVars...)
		}
		server.Env = newEnv
	}

	for idx, arg := range server.Args {
		newArg, usedVars, missingVars := vars.Substitute(arg, resolved)
		server.Args[idx] = newArg
		for usedKey, usedValue := range usedVars {
			used[usedKey] = usedValue
		}
		missing = append(missing, missingVars...)
	}

	newCommand, usedVars, missingVars := vars.Substitute(server.Command, resolved)
	server.Command = newCommand
	for usedKey, usedValue := range usedVars {
		used[usedKey] = usedValue
	}
	missing = append(missing, missingVars...)

	if len(used) > 0 {
		existingConfig.MCPServers[managed] = server
		if err := ideImpl.WriteMCPConfig(projectRoot, existingConfig); err != nil {
			emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("写入 MCP 配置失败: %v", err), nil)
		}
	}

	return used, missing, locations
}

func emitMissingVars(reporter Reporter, itemType, assetName string, missing []string, locations map[string][]string, projectVarsPath, globalVarsPath string) {
	seen := map[string]bool{}
	for _, placeholder := range missing {
		if seen[placeholder] {
			continue
		}
		lines := []string{
			fmt.Sprintf("变量 {{%s}} 未定义", placeholder),
			fmt.Sprintf("资产: [%s] %s", itemType, assetName),
		}
		for _, location := range formatPlaceholderLocations(locations[placeholder]) {
			lines = append(lines, "来源: "+location)
		}
		lines = append(lines, fmt.Sprintf("项目级: %s -> vars.%s 或 assets.%s.%s.vars.%s", projectVarsPath, placeholder, itemType, assetName, placeholder))
		if strings.TrimSpace(globalVarsPath) != "" {
			lines = append(lines, fmt.Sprintf("本机级: %s -> vars.%s 或 assets.%s.%s.vars.%s", globalVarsPath, placeholder, itemType, assetName, placeholder))
		}
		emit(reporter, EventWarn, "pull.vars", strings.Join(lines, "\n"), nil)
		seen[placeholder] = true
	}
}

func formatPlaceholderLocations(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	ordered := append([]string(nil), paths...)
	sort.Strings(ordered)

	formatted := make([]string, 0, len(ordered))
	for _, path := range ordered {
		formatted = append(formatted, filepath.Clean(path))
	}
	return formatted
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

// renderedHeaderMarker 是用来识别本文件顶部是否已注入过「勿编辑」注释的幂等标记。
// 只要顶部 Markdown 注释中包含这个子串，就视为已注入，不再重复。
const renderedHeaderMarker = "本文件由 `dec pull` 从"

// renderedHeader 生成写入 rule/skill 副本顶部的「勿编辑」Markdown 注释。
// vaultName 为空时退化为通用占位，避免误导读者。
func renderedHeader(vaultName string) string {
	vault := strings.TrimSpace(vaultName)
	if vault == "" {
		vault = "<vault>"
	}
	return fmt.Sprintf("<!-- 本文件由 `dec pull` 从 .dec/cache/%s/ 渲染生成，请勿直接编辑。\n"+
		"     修改流程：编辑 .dec/cache/%s/... → 在 Run 页 push → pull 验证 -->\n\n",
		vault, vault)
}

// shouldInjectHeader 只对 Markdown 类文本资产注入注释，避免破坏 JSON/TOML/其它格式。
func shouldInjectHeader(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".mdc":
		return true
	default:
		return false
	}
}

// injectRenderedHeaderFile 在单个 Markdown 副本顶部注入「勿编辑」注释。
// 如果目标文件已经包含注释标记，则保持幂等不重复注入。
func injectRenderedHeaderFile(path, vaultName string) error {
	if !shouldInjectHeader(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// 幂等：只检查文件前 512 字节，足够覆盖任何正常 header。
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	if strings.Contains(string(head), renderedHeaderMarker) {
		return nil
	}
	header := renderedHeader(vaultName)
	combined := append([]byte(header), data...)
	return os.WriteFile(path, combined, 0644)
}

// injectRenderedHeaderDir 递归为一个 skill 目录内所有 Markdown 副本注入注释。
func injectRenderedHeaderDir(dir, vaultName string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		return injectRenderedHeaderFile(path, vaultName)
	})
}

func enabledBundleNamesFromConfig(projectConfig *types.ProjectConfig, overviews []BundleOverview) []string {
	if projectConfig != nil && len(projectConfig.EnabledBundles) > 0 {
		return append([]string(nil), projectConfig.EnabledBundles...)
	}
	names := make([]string, 0, len(overviews))
	for _, overview := range overviews {
		if overview.Enabled {
			names = append(names, overview.Name)
		}
	}
	sort.Strings(names)
	return names
}

func saveVersionMeta(projectRoot, commitHash string) {
	versionPath := filepath.Join(projectRoot, ".dec", ".version")
	content := fmt.Sprintf("commit: %s\npulled_at: %q\n", commitHash, time.Now().Format(time.RFC3339))
	_ = os.MkdirAll(filepath.Dir(versionPath), 0755)
	_ = os.WriteFile(versionPath, []byte(content), 0644)
}

// stripExternalEnvLauncher 去掉历史外部启动器外壳（如 mise exec ... --），返回真实命令。
func stripExternalEnvLauncher(command string, args []string) (string, []string) {
	command = strings.TrimSpace(command)
	if command == "dec-exec" {
		for i := 0; i < len(args); i++ {
			if args[i] == "--" && i+1 < len(args) {
				return args[i+1], append([]string(nil), args[i+2:]...)
			}
		}
		return command, args
	}
	// 兼容旧配置：dec exec -- ...
	if command == "dec" && len(args) > 0 && args[0] == "exec" {
		for i := 0; i < len(args); i++ {
			if args[i] == "--" && i+1 < len(args) {
				return args[i+1], append([]string(nil), args[i+2:]...)
			}
		}
		return command, args
	}
	if command == "mise" && len(args) >= 2 && args[0] == "exec" {
		for i, a := range args {
			if a == "--" && i+1 < len(args) {
				return args[i+1], append([]string(nil), args[i+2:]...)
			}
		}
	}
	return command, args
}
