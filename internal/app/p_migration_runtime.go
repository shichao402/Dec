package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

// PreviewPMigration 对已连接 Git vault 与 Bitwarden 仅做读取。
func PreviewPMigration(ctx context.Context, workspace Workspace, reporter Reporter) (*PMigrationPlan, error) {
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

// SyncPManifestsFromBitwarden 为已经是 P folder 的 BW 项补齐缺失的 Git dec.yaml。
func SyncPManifestsFromBitwarden(ctx context.Context, reporter Reporter) error {
	if err := ensureBitwardenSession(ctx, reporter, "p.migrate.manifests"); err != nil {
		return err
	}
	client := secretsClientFactory()
	folders, err := client.ListAllFolderNames(ctx)
	if err != nil {
		return err
	}
	names := map[string]struct{}{}
	for _, folder := range folders {
		pName, _, ok := secrets.ParsePFolder(folder)
		if ok {
			names[pName] = struct{}{}
		}
	}
	return withAppWriteRepo(func(tx *repo.Transaction) error {
		added := 0
		for name := range names {
			if _, err := pmodel.Load(tx.WorkDir(), name); err == nil {
				continue
			}
			if err := pmodel.SaveManifest(tx.WorkDir(), types.P{Name: name, Title: name}); err != nil {
				return err
			}
			added++
			emit(reporter, EventInfo, "p.migrate.manifests", "补齐 P 声明："+name, nil)
		}
		if added == 0 {
			return nil
		}
		_, err := tx.CommitAndPush("migrate: add missing P manifests from Bitwarden")
		return err
	})
}

// RunPMigration 执行已确认的 preview。调用前会重做只读 preview 并比对指纹，防止陈旧计划写入。
func RunPMigration(ctx context.Context, workspace Workspace, expectedFingerprint string, reporter Reporter) (*PMigrationJournal, error) {
	journalPath, err := pMigrationJournalPath()
	if err != nil {
		return nil, err
	}
	if data, readErr := os.ReadFile(journalPath); readErr == nil {
		var recovery PMigrationJournal
		if err := json.Unmarshal(data, &recovery); err != nil {
			return nil, fmt.Errorf("解析迁移恢复日志失败: %w", err)
		}
		if recovery.Phase == PMigrationComplete {
			_ = os.Remove(journalPath)
		} else {
			if recovery.PlanFingerprint != strings.TrimSpace(expectedFingerprint) || recovery.Plan == nil {
				return nil, fmt.Errorf("恢复日志与当前确认不匹配，请检查 %s", journalPath)
			}
			if len(recovery.Plan.BWMoves) > 0 || len(recovery.Plan.LegacyBWFolders) > 0 {
				if err := ensureBitwardenSession(ctx, reporter, "p.migrate.resume"); err != nil {
					return nil, err
				}
			}
			backend := &livePMigrationBackend{workspace: workspace, client: secretsClientFactory(), reporter: reporter}
			return ExecutePMigration(ctx, recovery.Plan, journalPath, backend, reporter)
		}
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

func pMigrationJournalPath() (string, error) {
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

func (b *livePMigrationBackend) PrepareGit(ctx context.Context, plan *PMigrationPlan) error {
	return withAppWriteRepo(func(tx *repo.Transaction) error {
		for _, manifest := range plan.Manifests {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := pmodel.Load(tx.WorkDir(), manifest.Name); err == nil {
				continue
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
				if _, err := b.client.GetSecureNote(ctx, move.TargetFolder, move.Path); err == nil {
					continue
				}
				if _, err := b.client.PushBundle(ctx, secrets.PushBundleRequest{Target: target, CreateFolderIfMissing: true}, []secrets.SecureNote{note}); err != nil {
					return err
				}
			case "sshkey":
				key, ok := keysByPath[move.Path]
				if !ok {
					return fmt.Errorf("旧 BW SSH Key 已缺失: %s/%s", sourceFolder, move.Path)
				}
				if sshKeyExists(ctx, b.client, move.TargetFolder, move.Path) {
					continue
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
	for _, folder := range plan.LegacyBWFolders {
		if _, _, isP := secrets.ParsePFolder(folder); isP {
			continue
		}
		if err := b.client.DeleteFolder(ctx, folder); err != nil {
			return fmt.Errorf("删除旧 BW folder %q: %w", folder, err)
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

func sshKeyExists(ctx context.Context, client secrets.Client, folder, path string) bool {
	keys, err := client.ListFolderSSHKeys(ctx, folder)
	if err != nil {
		return false
	}
	for _, key := range keys {
		if cleanLogicalPath(key.Name) == path {
			return true
		}
	}
	return false
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
