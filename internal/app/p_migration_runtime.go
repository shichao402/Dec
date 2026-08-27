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

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

// PreviewPMigration 对已连接 Git vault 与 Bitwarden 仅做读取。
func PreviewPMigration(ctx context.Context, workspace Workspace, reporter Reporter) (*PMigrationPlan, error) {
	if journalPath, pathErr := pMigrationJournalPath(workspace); pathErr == nil {
		if data, readErr := os.ReadFile(journalPath); readErr == nil {
			var recovery PMigrationJournal
			if err := json.Unmarshal(data, &recovery); err != nil {
				return nil, fmt.Errorf("解析迁移恢复日志失败: %w", err)
			}
			if recovery.Version == PMigrationJournalVersion && recovery.Plan != nil && recovery.Phase != PMigrationComplete {
				emit(reporter, EventWarn, "p.migrate.preview",
					fmt.Sprintf("发现未完成迁移，将从阶段 %s 恢复", recovery.Phase), nil)
				return recovery.Plan, nil
			}
		} else if !os.IsNotExist(readErr) {
			return nil, readErr
		}
	}
	var (
		plan *PMigrationPlan
		snap = PMigrationBWSnapshot{Folders: map[string]PMigrationBWFolder{}}
	)
	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, err
	}
	if configured {
		if err := ensureBitwardenSession(ctx, reporter, "p.migrate.preview"); err != nil {
			return nil, err
		}
		client := secretsClientFactory()
		folders, err := client.ListAllFolderNames(ctx)
		if err != nil {
			return nil, fmt.Errorf("读取 Bitwarden folder 元数据: %w", err)
		}
		for _, folder := range folders {
			notes, err := client.ListFolderNotes(ctx, folder)
			if err != nil {
				return nil, fmt.Errorf("读取 Bitwarden folder %q: %w", folder, err)
			}
			keys, err := client.ListFolderSSHKeys(ctx, folder)
			if err != nil {
				return nil, fmt.Errorf("读取 Bitwarden SSH folder %q: %w", folder, err)
			}
			item := PMigrationBWFolder{}
			for _, note := range notes {
				item.Notes = append(item.Notes, note.Name)
			}
			for _, key := range keys {
				item.SSHKeys = append(item.SSHKeys, key.Name)
			}
			snap.Folders[folder] = item
		}
	}
	if err := withAppReadRepo(func(tx *repo.Transaction) error {
		var buildErr error
		plan, buildErr = BuildPMigrationPlan(tx.WorkDir(), snap)
		return buildErr
	}); err != nil {
		return nil, err
	}
	emit(reporter, EventInfo, "p.migrate.preview",
		fmt.Sprintf("只读预览：%d 个 P · %d 个 Git 文件 · %d 个 BW 项 · %d 个问题",
			len(plan.Manifests), len(plan.GitMoves), len(plan.BWMoves), len(plan.Issues)), nil)
	return plan, nil
}

// RunPMigration 执行已确认的 preview。调用前会重做只读 preview 并比对指纹，防止陈旧计划写入。
func RunPMigration(ctx context.Context, workspace Workspace, expectedFingerprint string, reporter Reporter) (*PMigrationJournal, error) {
	if workspace.EffectivePlane() != WorkspaceProject {
		return nil, fmt.Errorf("P 迁移会删除全局旧远端结构，只能在项目工作区 Run 页执行，以便同时备份并切换项目与用户平面")
	}
	journalPath, err := pMigrationJournalPath(workspace)
	if err != nil {
		return nil, err
	}
	if data, readErr := os.ReadFile(journalPath); readErr == nil {
		var recovery PMigrationJournal
		if err := json.Unmarshal(data, &recovery); err != nil {
			return nil, fmt.Errorf("解析迁移恢复日志失败: %w", err)
		}
		if recovery.PlanFingerprint != strings.TrimSpace(expectedFingerprint) || recovery.Plan == nil {
			return nil, fmt.Errorf("恢复日志与当前确认不匹配，请检查 %s", journalPath)
		}
		if len(recovery.Plan.BWMoves) > 0 {
			if err := ensureBitwardenSession(ctx, reporter, "p.migrate.resume"); err != nil {
				return nil, err
			}
		}
		backend := &livePMigrationBackend{workspace: workspace, client: secretsClientFactory(), reporter: reporter}
		return ExecutePMigration(ctx, recovery.Plan, journalPath, backend, reporter)
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}
	plan, err := PreviewPMigration(ctx, workspace, reporter)
	if err != nil {
		return nil, err
	}
	if plan.Fingerprint != strings.TrimSpace(expectedFingerprint) {
		return nil, fmt.Errorf("仓库或 Bitwarden 已变化，请重新 preview 后确认")
	}
	if plan.HasBlockers() {
		return nil, fmt.Errorf("迁移存在阻断问题")
	}
	client := secretsClientFactory()
	backend := &livePMigrationBackend{workspace: workspace, client: client, reporter: reporter}
	return ExecutePMigration(ctx, plan, journalPath, backend, reporter)
}

func pMigrationJournalPath(workspace Workspace) (string, error) {
	if workspace.EffectivePlane() == WorkspaceProject {
		if strings.TrimSpace(workspace.Root) == "" {
			return "", config.ErrProjectRootRequired
		}
		return filepath.Join(workspace.Root, ".dec", "migrations", "p-four-quadrant-v1.json"), nil
	}
	root, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "migrations", "p-four-quadrant-v1.json"), nil
}

type livePMigrationBackend struct {
	workspace Workspace
	client    secrets.Client
	reporter  Reporter
}

func (b *livePMigrationBackend) BackupLocal(_ context.Context, _ *PMigrationPlan) (string, error) {
	base, err := pMigrationJournalPath(b.workspace)
	if err != nil {
		return "", err
	}
	backup := filepath.Join(filepath.Dir(base), "backups", time.Now().UTC().Format("20060102T150405.000000000Z"))
	var sources []string
	if b.workspace.EffectivePlane() == WorkspaceProject {
		sources = []string{
			filepath.Join(b.workspace.Root, ".dec", "config.yaml"),
			filepath.Join(b.workspace.Root, ".dec", "cache"),
			filepath.Join(b.workspace.Root, secrets.SecretsRootDir),
		}
		root, rootErr := repo.GetRootDir()
		if rootErr != nil {
			return "", rootErr
		}
		for _, source := range []string{
			filepath.Join(root, "config.yaml"),
			filepath.Join(root, "cache"),
			filepath.Join(root, "secrets"),
		} {
			if _, statErr := os.Stat(source); os.IsNotExist(statErr) {
				continue
			}
			if err := copyTreeNoOverwrite(source, filepath.Join(backup, "user", filepath.Base(source))); err != nil {
				return "", fmt.Errorf("备份用户平面 %s: %w", source, err)
			}
		}
	} else {
		root, rootErr := repo.GetRootDir()
		if rootErr != nil {
			return "", rootErr
		}
		sources = []string{filepath.Join(root, "config.yaml"), filepath.Join(root, "cache"), filepath.Join(root, "secrets")}
	}
	for _, source := range sources {
		if _, statErr := os.Stat(source); os.IsNotExist(statErr) {
			continue
		}
		if err := copyTreeNoOverwrite(source, filepath.Join(backup, filepath.Base(source))); err != nil {
			return "", fmt.Errorf("备份 %s: %w", source, err)
		}
	}
	return backup, nil
}

func (b *livePMigrationBackend) PrepareGit(ctx context.Context, plan *PMigrationPlan) error {
	return withAppWriteRepo(func(tx *repo.Transaction) error {
		for _, manifest := range plan.Manifests {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := pmodel.SaveManifest(tx.WorkDir(), types.P{
				Name: manifest.Name, Title: manifest.Title, Description: manifest.Description,
				Requires: manifest.Requires, IDEs: manifest.IDEs, Editor: manifest.Editor,
			}); err != nil {
				return err
			}
		}
		for _, move := range plan.GitMoves {
			source := filepath.Join(tx.WorkDir(), filepath.FromSlash(move.Source))
			target := filepath.Join(tx.WorkDir(), filepath.FromSlash(move.Target))
			if err := copyFileNoOverwriteOrEqual(source, target); err != nil {
				return err
			}
		}
		_, err := tx.CommitAndPush("migrate: prepare P four-quadrant tree")
		return err
	})
}

func (b *livePMigrationBackend) VerifyGit(_ context.Context, plan *PMigrationPlan) error {
	return withAppReadRepo(func(tx *repo.Transaction) error {
		for _, manifest := range plan.Manifests {
			if _, err := pmodel.Load(tx.WorkDir(), manifest.Name); err != nil {
				return fmt.Errorf("校验 P %q: %w", manifest.Name, err)
			}
		}
		for _, move := range plan.GitMoves {
			if err := filesEqual(
				filepath.Join(tx.WorkDir(), filepath.FromSlash(move.Source)),
				filepath.Join(tx.WorkDir(), filepath.FromSlash(move.Target))); err != nil {
				return fmt.Errorf("校验 Git 迁移 %s: %w", move.Target, err)
			}
		}
		return nil
	})
}

func (b *livePMigrationBackend) WriteBitwarden(ctx context.Context, plan *PMigrationPlan) error {
	grouped := migrationBWMovesBySource(plan)
	for sourceFolder, moves := range grouped {
		source, err := secrets.NewBrowseFolder(sourceFolder)
		if err != nil {
			return err
		}
		content, err := b.client.PullBundle(ctx, secrets.PullBundleRequest{Target: source})
		if err != nil {
			return fmt.Errorf("读取旧 BW folder %q: %w", sourceFolder, err)
		}
		notesByPath := map[string]secrets.SecureNote{}
		keysByPath := map[string]secrets.SSHKeyItem{}
		for _, note := range content.Notes {
			notesByPath[cleanLogicalPath(note.RelativePath)] = note
		}
		for _, key := range content.SSHKeys {
			keysByPath[cleanLogicalPath(key.Name)] = key
		}
		for _, move := range moves {
			pName, plane, ok := secrets.ParsePFolder(move.TargetFolder)
			if !ok {
				return fmt.Errorf("无效 P folder %q", move.TargetFolder)
			}
			target, err := secrets.NewPSyncTarget(pName, plane)
			if err != nil {
				return err
			}
			switch move.Kind {
			case "note":
				note, ok := notesByPath[move.Path]
				if !ok {
					return fmt.Errorf("旧 BW Note 已缺失: %s/%s", sourceFolder, move.Path)
				}
				if _, err := b.client.PushBundle(ctx, secrets.PushBundleRequest{Target: target, CreateFolderIfMissing: true}, []secrets.SecureNote{note}); err != nil {
					return err
				}
			case "sshkey":
				key, ok := keysByPath[move.Path]
				if !ok {
					return fmt.Errorf("旧 BW SSH Key 已缺失: %s/%s", sourceFolder, move.Path)
				}
				if err := b.client.CreateSSHKey(ctx, secrets.CreateSSHKeyRequest{Target: target, Key: key, CreateFolderIfMissing: true}); err != nil &&
					!strings.Contains(strings.ToLower(err.Error()), "已存在") {
					return err
				}
			}
		}
	}
	return nil
}

func (b *livePMigrationBackend) VerifyBitwarden(ctx context.Context, plan *PMigrationPlan) error {
	for _, move := range plan.BWMoves {
		switch move.Kind {
		case "note":
			if _, err := b.client.GetSecureNote(ctx, move.TargetFolder, move.Path); err != nil {
				return fmt.Errorf("校验 BW Note %s/%s: %w", move.TargetFolder, move.Path, err)
			}
		case "sshkey":
			keys, err := b.client.ListFolderSSHKeys(ctx, move.TargetFolder)
			if err != nil {
				return err
			}
			found := false
			for _, key := range keys {
				if cleanLogicalPath(key.Name) == move.Path {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("校验 BW SSH Key 失败: %s/%s 不存在", move.TargetFolder, move.Path)
			}
		}
	}
	return nil
}

func (b *livePMigrationBackend) SwitchLocal(_ context.Context, plan *PMigrationPlan, _ string) error {
	if b.workspace.EffectivePlane() == WorkspaceProject {
		mgr := config.NewProjectConfigManager(b.workspace.Root)
		cfg, err := mgr.LoadProjectConfig()
		if err != nil {
			return err
		}
		cfg.ProjectName = NormalizeLegacyPName(cfg.ProjectName)
		for i := range cfg.EnabledBundles {
			cfg.EnabledBundles[i] = NormalizeLegacyPName(cfg.EnabledBundles[i])
		}
		cfg.EnabledBundles = uniqueSorted(cfg.EnabledBundles)
		if err := migrateLocalTrees(b.workspace, plan); err != nil {
			return err
		}
		if err := mgr.SaveProjectConfig(cfg); err != nil {
			return err
		}
		// 远端旧树是全局删除的，因此同一次显式迁移还必须切换本机 user
		// 平面的配置与落地；否则项目迁移成功后用户缓存会永久指向已删除结构。
		global, err := config.LoadGlobalConfig()
		if err != nil {
			return err
		}
		for i := range global.EnabledBundles {
			global.EnabledBundles[i] = NormalizeLegacyPName(global.EnabledBundles[i])
		}
		global.EnabledBundles = uniqueSorted(global.EnabledBundles)
		if err := migrateLocalTrees(NewWorkspace(WorkspaceUser, ""), plan); err != nil {
			return err
		}
		return config.SaveGlobalConfig(global)
	}
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return err
	}
	for i := range cfg.EnabledBundles {
		cfg.EnabledBundles[i] = NormalizeLegacyPName(cfg.EnabledBundles[i])
	}
	cfg.EnabledBundles = uniqueSorted(cfg.EnabledBundles)
	if err := migrateLocalTrees(b.workspace, plan); err != nil {
		return err
	}
	return config.SaveGlobalConfig(cfg)
}

func (b *livePMigrationBackend) DeleteLegacyBitwarden(ctx context.Context, plan *PMigrationPlan) error {
	for _, move := range plan.BWMoves {
		source, err := secrets.NewBrowseFolder(move.SourceFolder)
		if err != nil {
			return err
		}
		if move.Kind == "note" {
			err = b.client.DeleteSecureNote(ctx, secrets.DeleteSecureNoteRequest{Target: source, NotePath: move.Path})
		} else {
			err = b.client.DeleteSSHKey(ctx, secrets.DeleteSSHKeyRequest{Target: source, KeyName: move.Path})
		}
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "不在 folder") && !strings.Contains(strings.ToLower(err.Error()), "不存在") {
			return err
		}
	}
	return nil
}

func (b *livePMigrationBackend) DeleteLegacyGit(_ context.Context, _ *PMigrationPlan) error {
	return withAppWriteRepo(func(tx *repo.Transaction) error {
		for _, name := range []string{types.VaultProjectsDir, types.VaultBundlesDir} {
			if err := os.RemoveAll(filepath.Join(tx.WorkDir(), name)); err != nil {
				return err
			}
		}
		_, err := tx.CommitAndPush("migrate: remove legacy project and bundle tree")
		return err
	})
}

func migrationBWMovesBySource(plan *PMigrationPlan) map[string][]PMigrationBWMove {
	out := map[string][]PMigrationBWMove{}
	for _, move := range plan.BWMoves {
		out[move.SourceFolder] = append(out[move.SourceFolder], move)
	}
	return out
}

func migrateLocalTrees(workspace Workspace, plan *PMigrationPlan) error {
	cacheRoot := workspaceCacheDir(workspace)
	legacyCacheRoots := map[string]struct{}{}
	for _, move := range plan.GitMoves {
		parts := strings.Split(move.Source, "/")
		if len(parts) < 4 || parts[0] != types.VaultBundlesDir {
			continue
		}
		targetParts := strings.Split(move.Target, "/")
		if len(targetParts) < 4 {
			continue
		}
		wantPlane := string(types.AssetPlaneProject)
		if workspace.EffectivePlane() == WorkspaceUser {
			wantPlane = string(types.AssetPlaneUser)
		}
		if targetParts[2] != wantPlane {
			continue
		}
		legacyCacheRoots[parts[1]] = struct{}{}
		source := filepath.Join(cacheRoot, filepath.FromSlash(strings.Join(parts[1:], "/")))
		target := filepath.Join(cacheRoot, filepath.FromSlash(strings.Join(targetParts, "/")))
		if _, err := os.Stat(source); err == nil {
			if err := copyFileNoOverwriteOrEqual(source, target); err != nil {
				return err
			}
		}
	}
	for name := range legacyCacheRoots {
		if err := os.RemoveAll(filepath.Join(cacheRoot, name)); err != nil {
			return err
		}
	}
	type localMove struct{ source, target string }
	var moves []localMove
	if workspace.EffectivePlane() == WorkspaceProject {
		for _, bwMove := range plan.BWMoves {
			if !strings.HasSuffix(bwMove.TargetFolder, "/private/project") {
				continue
			}
			pName := strings.Split(bwMove.TargetFolder, "/")[0]
			var sourceRoot string
			if strings.HasPrefix(bwMove.SourceFolder, "bundle/") {
				sourceRoot = filepath.Join(workspace.Root, secrets.BundleSecretsLocalRelPrefix, strings.TrimPrefix(bwMove.SourceFolder, "bundle/"))
			} else {
				sourceRoot = filepath.Join(workspace.Root, secrets.ProjectSecretsLocalRel)
			}
			moves = append(moves, localMove{sourceRoot, filepath.Join(workspace.Root, secrets.SecretsRootDir, pName)})
		}
	} else {
		root, err := repo.GetRootDir()
		if err != nil {
			return err
		}
		for _, bwMove := range plan.BWMoves {
			if !strings.HasSuffix(bwMove.TargetFolder, "/private/user") || !strings.HasPrefix(bwMove.SourceFolder, "bundle/") {
				continue
			}
			pName := strings.Split(bwMove.TargetFolder, "/")[0]
			moves = append(moves, localMove{
				filepath.Join(root, "secrets", secrets.MachineBundleSecretsRelPrefix, strings.TrimPrefix(bwMove.SourceFolder, "bundle/")),
				filepath.Join(root, "secrets", pName),
			})
		}
	}
	seen := map[string]struct{}{}
	for _, move := range moves {
		key := move.source + "\x00" + move.target
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, err := os.Stat(move.source); os.IsNotExist(err) {
			continue
		}
		if err := copyTreeNoOverwrite(move.source, move.target); err != nil {
			return err
		}
		if err := os.RemoveAll(move.source); err != nil {
			return err
		}
	}
	return nil
}

func copyTreeNoOverwrite(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFileNoOverwriteOrEqual(source, target)
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o700)
		}
		return copyFileNoOverwriteOrEqual(path, dest)
	})
}

func copyFileNoOverwriteOrEqual(source, target string) error {
	if _, err := os.Stat(target); err == nil {
		return filesEqual(source, target)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func filesEqual(a, b string) error {
	left, err := os.ReadFile(a)
	if err != nil {
		return err
	}
	right, err := os.ReadFile(b)
	if err != nil {
		return err
	}
	if string(left) != string(right) {
		return fmt.Errorf("目标已存在且内容不同")
	}
	return nil
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
