package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
	"github.com/shichao402/Dec/internal/types"
)

// MigrateUnmanagedNoteInput 把非托管 folder 里的 Secure Note 迁到已声明的 bundle/<名>。
type MigrateUnmanagedNoteInput struct {
	SourceFolder string
	NotePath     string
	// DestBundle 是目标 Dec bundle 短名（会写成 Bitwarden folder bundle/<名>）。
	DestBundle string
	// Plane 决定补 vault manifest 的 scope 与 SyncTarget 平面；空则 user。
	Plane WorkspacePlane
	// Enable 为 true 时把 DestBundle 写入当前平面的 enabled_bundles。
	Enable bool
}

// MigrateUnmanagedNoteResult 汇报一次迁移结果。
type MigrateUnmanagedNoteResult struct {
	SourceFolder string
	DestFolder   string
	NotePath     string
	DestBundle   string
	Enabled      bool
	CreatedVault bool
}

// MigrateUnmanagedNoteToBundle 将裸 folder（或其它未声明 folder）中的 Note 复制到
// bundle/<DestBundle>，再删除源 Note。目标侧走声明型构造函数；源侧删除允许未声明（ADR 0013）。
func MigrateUnmanagedNoteToBundle(ctx context.Context, input MigrateUnmanagedNoteInput, reporter Reporter) (*MigrateUnmanagedNoteResult, error) {
	reporter = defaultReporter(reporter)
	srcFolder := strings.Trim(strings.TrimSpace(strings.ReplaceAll(input.SourceFolder, "\\", "/")), "/")
	notePath := handler.NormalizeNotePath(input.NotePath)
	destBundle := strings.TrimSpace(input.DestBundle)
	if srcFolder == "" || notePath == "" || destBundle == "" {
		return nil, fmt.Errorf("迁移需要 SourceFolder、NotePath 与 DestBundle")
	}
	if strings.HasPrefix(srcFolder, secrets.BundleFolderPrefix) {
		return nil, fmt.Errorf("源 folder %q 已是 bundle/*，无需按非托管迁移", srcFolder)
	}
	if err := validateRemoteOwnerName("bundle", destBundle); err != nil {
		return nil, err
	}
	destFolder := secrets.DefaultBundleFolder(destBundle)
	if srcFolder == destFolder {
		return nil, fmt.Errorf("源与目标 folder 相同: %s", srcFolder)
	}

	plane := input.Plane
	if plane == "" {
		plane = WorkspaceUser
	}
	workspace := NewWorkspace(plane, "")

	if err := ensureBitwardenSession(ctx, reporter, "remote.migrate"); err != nil {
		return nil, err
	}
	client := secretsClientFactory()

	note, err := client.GetSecureNote(ctx, srcFolder, notePath)
	if err != nil {
		return nil, fmt.Errorf("读取源 Note %s/%s: %w", srcFolder, notePath, err)
	}
	if note == nil || strings.TrimSpace(note.Content) == "" {
		return nil, fmt.Errorf("源 Note %s/%s 正文为空", srcFolder, notePath)
	}

	destTarget, err := resolveRemoteRegisterTarget(workspace, destFolder, true, reporter)
	if err != nil {
		return nil, err
	}
	binding := secrets.BundleBinding{
		DecBundleName:     destTarget.Name,
		SecretsBundleName: destTarget.Folder,
	}
	pushResult, err := client.PushBundle(ctx, secrets.PushBundleRequest{
		Target:                destTarget,
		Binding:               binding,
		CreateFolderIfMissing: true,
	}, []secrets.SecureNote{{RelativePath: notePath, Content: note.Content}})
	if err != nil {
		return nil, fmt.Errorf("写入目标 %s: %w", destFolder, err)
	}
	created := pushResult != nil && pushResult.Created > 0
	emit(reporter, EventInfo, "remote.migrate",
		fmt.Sprintf("已写入 %s/%s（created=%v）", destFolder, notePath, created), nil)

	srcTarget, err := secrets.NewBrowseFolder(srcFolder)
	if err != nil {
		return nil, err
	}
	if err := client.DeleteSecureNote(ctx, secrets.DeleteSecureNoteRequest{
		Target:   srcTarget,
		Binding:  secrets.BundleBinding{SecretsBundleName: srcFolder},
		NotePath: notePath,
	}); err != nil {
		return nil, fmt.Errorf("目标已写入，但删除源 Note 失败（请手工删 %s/%s）: %w", srcFolder, notePath, err)
	}
	emit(reporter, EventInfo, "remote.migrate",
		fmt.Sprintf("已删除源 %s/%s", srcFolder, notePath), nil)

	result := &MigrateUnmanagedNoteResult{
		SourceFolder: srcFolder,
		DestFolder:   destFolder,
		NotePath:     notePath,
		DestBundle:   destBundle,
		CreatedVault: true, // resolveRemoteRegisterTarget(ensure=true) 会补占位；精确值下面再判
	}

	if plane == WorkspaceUser {
		repair, repairErr := ensureVaultBundlesForUserEnable([]string{destBundle}, reporter)
		if repairErr != nil {
			emit(reporter, EventWarn, "remote.migrate",
				fmt.Sprintf("Note 已迁移，但补 vault 占位失败: %v", repairErr), nil)
		} else if repair != nil {
			result.CreatedVault = len(repair.Created) > 0
		}
	}

	if input.Enable {
		if err := enableBundleOnPlane(workspace, destBundle); err != nil {
			return result, fmt.Errorf("Note 已迁移，但启用 bundle 失败: %w", err)
		}
		result.Enabled = true
		emit(reporter, EventInfo, "remote.migrate",
			fmt.Sprintf("已在 %s 平面启用 bundle %q", plane, destBundle), nil)
	}

	emit(reporter, EventInfo, "remote.migrate",
		fmt.Sprintf("迁移完成：%s/%s → %s/%s", srcFolder, notePath, destFolder, notePath), nil)
	return result, nil
}

// MigrateCNBGCMToUserBundle 查找 host=cnb.cool 的非托管 GCM Note，迁到 bundle/cnb 并启用。
// 专用于本次把启动信任根凭据收回一等公民模型；多候选时要求唯一 unmanaged。
func MigrateCNBGCMToUserBundle(ctx context.Context, reporter Reporter) (*MigrateUnmanagedNoteResult, error) {
	reporter = defaultReporter(reporter)
	prepared, err := PrepareRepoGCMBootstrap(ctx, "", reporter)
	if err != nil {
		return nil, err
	}
	var unmanaged []RepoGCMCandidate
	for _, c := range prepared.Candidates {
		if c.Unmanaged {
			unmanaged = append(unmanaged, c)
		}
	}
	if len(unmanaged) == 0 {
		for _, c := range prepared.Candidates {
			if strings.EqualFold(c.Folder, "bundle/cnb") {
				return nil, fmt.Errorf("cnb.cool 的 GCM 已在 %s/%s，无需迁移", c.Folder, c.NotePath)
			}
		}
		return nil, fmt.Errorf("未找到 host=%s 的非托管 GCM Note", prepared.RepoHost)
	}
	if len(unmanaged) > 1 {
		var labels []string
		for _, c := range unmanaged {
			labels = append(labels, c.Folder+"/"+c.NotePath)
		}
		return nil, fmt.Errorf("找到多个非托管 GCM 候选，请指定源后调用 MigrateUnmanagedNoteToBundle: %s",
			strings.Join(labels, ", "))
	}
	c := unmanaged[0]
	return MigrateUnmanagedNoteToBundle(ctx, MigrateUnmanagedNoteInput{
		SourceFolder: c.Folder,
		NotePath:     c.NotePath,
		DestBundle:   "cnb",
		Plane:        WorkspaceUser,
		Enable:       true,
	}, reporter)
}

func enableBundleOnPlane(workspace Workspace, bundleName string) error {
	bundleName = strings.TrimSpace(bundleName)
	if bundleName == "" {
		return fmt.Errorf("bundle 名不能为空")
	}
	if workspace.EffectivePlane() == WorkspaceUser {
		cfg, err := config.LoadGlobalConfig()
		if err != nil {
			return err
		}
		cfg.EnabledBundles = normalizeEnabledBundles(append(append([]string{}, cfg.EnabledBundles...), bundleName))
		return config.SaveGlobalConfig(cfg)
	}
	mgr := config.NewProjectConfigManager(workspace.Root)
	pc, err := mgr.LoadProjectConfig()
	if err != nil {
		return err
	}
	if pc == nil {
		pc = &types.ProjectConfig{}
	}
	pc.EnabledBundles = normalizeEnabledBundles(append(append([]string{}, pc.EnabledBundles...), bundleName))
	return mgr.SaveProjectConfig(pc)
}

// MigrateProjectSecretsInput 把存量裸 project folder / `.secrets/project` 迁入 scope:project Bundle。
type MigrateProjectSecretsInput struct {
	ProjectRoot string
	// ProjectName 为空时从项目配置推断。
	ProjectName string
	// DestBundle 目标 bundle 短名；默认与 ProjectName 相同。
	DestBundle string
	// SourceFolder 裸 BW folder；默认与 ProjectName / secrets.Config.project_secrets 相同。
	SourceFolder string
	// DeleteSource 为 true 时迁移成功后删除源 folder 中已迁 Note（逐条）。
	DeleteSource bool
	// Enable 为 true 时把 DestBundle 写入项目 enabled_bundles。
	Enable bool
}

// MigrateProjectSecretsResult 汇报 project→bundle 迁移结果。
type MigrateProjectSecretsResult struct {
	ProjectName   string
	SourceFolder  string
	DestFolder    string
	DestBundle    string
	MigratedNotes []string
	LocalMoved    bool
	Enabled       bool
	DeletedSource bool
}

// MigrateProjectSecretsToBundle 将历史 project 级 secrets 收进 `bundle/<DestBundle>`（ADR 0014 Phase 3）。
// 本地同步根从 `.secrets/project` 迁到 `.secrets/bundles/<DestBundle>`；vault 补 `scope: project` manifest。
func MigrateProjectSecretsToBundle(ctx context.Context, input MigrateProjectSecretsInput, reporter Reporter) (*MigrateProjectSecretsResult, error) {
	reporter = defaultReporter(reporter)
	projectRoot := strings.TrimSpace(input.ProjectRoot)
	if projectRoot == "" {
		return nil, fmt.Errorf("必须指定 ProjectRoot")
	}
	projectName := strings.TrimSpace(input.ProjectName)
	if projectName == "" {
		mgr := config.NewProjectConfigManager(projectRoot)
		pc, err := mgr.LoadProjectConfig()
		if err != nil {
			return nil, err
		}
		projectName, _ = ResolveProjectName(projectRoot, pc)
	}
	if projectName == "" || projectName == "unknown" {
		return nil, fmt.Errorf("无法解析 project 名")
	}
	destBundle := strings.TrimSpace(input.DestBundle)
	if destBundle == "" {
		destBundle = projectName
	}
	if err := validateRemoteOwnerName("bundle", destBundle); err != nil {
		return nil, err
	}
	srcFolder := strings.Trim(strings.TrimSpace(strings.ReplaceAll(input.SourceFolder, "\\", "/")), "/")
	if srcFolder == "" {
		cfg, err := secrets.LoadConfig()
		if err != nil {
			return nil, err
		}
		if name, ok := cfg.ResolveProjectSecrets(projectName); ok {
			srcFolder = name
		} else {
			srcFolder = projectName
		}
	}
	if strings.HasPrefix(srcFolder, secrets.BundleFolderPrefix) {
		return nil, fmt.Errorf("源 folder %q 已是 bundle/*，无需 project 迁移", srcFolder)
	}
	destFolder := secrets.DefaultBundleFolder(destBundle)
	if srcFolder == destFolder {
		return nil, fmt.Errorf("源与目标 folder 相同: %s", srcFolder)
	}

	workspace := NewWorkspace(WorkspaceProject, projectRoot)
	if err := ensureBitwardenSession(ctx, reporter, "remote.migrate.project"); err != nil {
		return nil, err
	}
	client := secretsClientFactory()

	destTarget, err := resolveRemoteRegisterTarget(workspace, destFolder, true, reporter)
	if err != nil {
		return nil, err
	}

	notes, err := client.ListFolderNotes(ctx, srcFolder)
	if err != nil {
		return nil, fmt.Errorf("列出源 folder %q 失败: %w", srcFolder, err)
	}
	result := &MigrateProjectSecretsResult{
		ProjectName:  projectName,
		SourceFolder: srcFolder,
		DestFolder:   destFolder,
		DestBundle:   destBundle,
	}
	binding := secrets.BundleBinding{
		DecBundleName:     destTarget.Name,
		SecretsBundleName: destTarget.Folder,
	}
	for _, meta := range notes {
		notePath := strings.TrimSpace(meta.Name)
		if notePath == "" {
			continue
		}
		note, getErr := client.GetSecureNote(ctx, srcFolder, notePath)
		if getErr != nil {
			return result, fmt.Errorf("读取 %s/%s: %w", srcFolder, notePath, getErr)
		}
		if note == nil || strings.TrimSpace(note.Content) == "" {
			emit(reporter, EventWarn, "remote.migrate.project",
				fmt.Sprintf("跳过空 Note %s/%s", srcFolder, notePath), nil)
			continue
		}
		if _, pushErr := client.PushBundle(ctx, secrets.PushBundleRequest{
			Target:                destTarget,
			Binding:               binding,
			CreateFolderIfMissing: true,
		}, []secrets.SecureNote{{RelativePath: notePath, Content: note.Content}}); pushErr != nil {
			return result, fmt.Errorf("写入 %s/%s: %w", destFolder, notePath, pushErr)
		}
		result.MigratedNotes = append(result.MigratedNotes, notePath)
		emit(reporter, EventInfo, "remote.migrate.project",
			fmt.Sprintf("已写入 %s/%s", destFolder, notePath), nil)

		if input.DeleteSource {
			srcTarget, browseErr := secrets.NewBrowseFolder(srcFolder)
			if browseErr != nil {
				return result, browseErr
			}
			if delErr := client.DeleteSecureNote(ctx, secrets.DeleteSecureNoteRequest{
				Target:   srcTarget,
				Binding:  secrets.BundleBinding{SecretsBundleName: srcFolder},
				NotePath: notePath,
			}); delErr != nil {
				return result, fmt.Errorf("目标已写入，但删除源 %s/%s 失败: %w", srcFolder, notePath, delErr)
			}
			result.DeletedSource = true
		}
	}

	moved, moveErr := migrateLocalProjectSecretsTree(projectRoot, destBundle)
	if moveErr != nil {
		return result, fmt.Errorf("远端已迁移，但本地同步根迁移失败: %w", moveErr)
	}
	result.LocalMoved = moved
	if moved {
		emit(reporter, EventInfo, "remote.migrate.project",
			fmt.Sprintf("本地 %s → .secrets/bundles/%s", secrets.ProjectSecretsLocalRel, destBundle), nil)
	}

	if input.Enable {
		if err := enableBundleOnPlane(workspace, destBundle); err != nil {
			return result, fmt.Errorf("Note 已迁移，但启用 bundle 失败: %w", err)
		}
		result.Enabled = true
		emit(reporter, EventInfo, "remote.migrate.project",
			fmt.Sprintf("已在 project 平面启用 bundle %q", destBundle), nil)
	}

	emit(reporter, EventInfo, "remote.migrate.project",
		fmt.Sprintf("project secrets 迁移完成：%s → %s（%d notes）", srcFolder, destFolder, len(result.MigratedNotes)), nil)
	return result, nil
}

func migrateLocalProjectSecretsTree(projectRoot, destBundle string) (bool, error) {
	src := filepath.Join(projectRoot, filepath.FromSlash(secrets.ProjectSecretsLocalRel))
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s 不是目录", src)
	}
	dest := filepath.Join(projectRoot, filepath.FromSlash(secrets.BundleSecretsLocalRelPrefix), destBundle)
	if _, err := os.Stat(dest); err == nil {
		return false, fmt.Errorf("目标本地同步根已存在: %s", dest)
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return false, err
	}
	if err := os.Rename(src, dest); err != nil {
		return false, err
	}
	return true, nil
}

