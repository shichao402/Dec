package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

var planeDirRenames = [][2]string{
	{"user", "global"},
	{"project", "local"},
}

func migrateRemotePlanes(ctx context.Context, reporter Reporter) error {
	needGit, err := remoteHasLegacyQuadrantDirs()
	if err != nil {
		return err
	}
	if needGit {
		if err := withAppWriteRepo(func(tx *repo.Transaction) error {
			changed, err := renameLegacyQuadrantDirs(tx.WorkDir())
			if err != nil {
				return err
			}
			if !changed {
				return nil
			}
			_, err = tx.CommitAndPush("migrate: rename quadrants to global/local")
			return err
		}); err != nil {
			return err
		}
	}
	if !secrets.HasSession() {
		return nil
	}
	return migrateBitwardenPlaneNames(ctx, reporter)
}

func remoteHasLegacyQuadrantDirs() (bool, error) {
	return repo.BareHEADHasLegacyQuadrants(), nil
}

func hasLegacyQuadrantDirs(repoDir string) (bool, error) {
	projects, err := pmodel.Scan(repoDir)
	if err != nil {
		return false, err
	}
	for name := range projects {
		for _, vis := range []string{"public", "private"} {
			for _, pair := range planeDirRenames {
				if _, err := os.Lstat(filepath.Join(repoDir, name, vis, pair[0])); err == nil {
					return true, nil
				} else if !os.IsNotExist(err) {
					return false, err
				}
			}
		}
	}
	return false, nil
}

func migrateBitwardenPlaneNames(ctx context.Context, reporter Reporter) error {
	configured, err := secrets.IsConfigured()
	if err != nil || !configured {
		return err
	}
	client := secretsClientFactory()
	names, err := client.ListPNames(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		if !types.IsValidProjectName(name) {
			continue
		}
		for _, plane := range []secrets.SyncPlane{secrets.SyncPlaneGlobal, secrets.SyncPlaneLocal} {
			target, err := secrets.NewPSyncTarget(name, plane)
			if err != nil {
				return err
			}
			notes, err := client.ListNotes(ctx, target)
			if err != nil {
				return err
			}
			for _, note := range notes {
				rel := strings.TrimSpace(note.Name)
				if rel == "" {
					continue
				}
				if err := client.RenameSecureNote(ctx, secrets.RenameSecureNoteRequest{
					OldPath: rel, NewPath: rel, Target: target,
				}); err != nil {
					emit(reporter, EventWarn, "plane.migrate", fmt.Sprintf("跳过 note %s: %v", rel, err), nil)
				}
			}
			keys, err := client.ListSSHKeys(ctx, target)
			if err != nil {
				return err
			}
			for _, key := range keys {
				rel := strings.TrimSpace(key.Name)
				if rel == "" {
					continue
				}
				if err := client.RenameSSHKey(ctx, secrets.RenameSSHKeyRequest{
					OldName: rel, NewName: rel, Target: target,
				}); err != nil {
					emit(reporter, EventWarn, "plane.migrate", fmt.Sprintf("跳过 ssh %s: %v", rel, err), nil)
				}
			}
		}
	}
	return nil
}

func renameLegacyQuadrantDirs(repoDir string) (bool, error) {
	projects, err := pmodel.Scan(repoDir)
	if err != nil {
		return false, err
	}
	changed := false
	for name := range projects {
		for _, vis := range []string{"public", "private"} {
			for _, pair := range planeDirRenames {
				src := filepath.Join(repoDir, name, vis, pair[0])
				dst := filepath.Join(repoDir, name, vis, pair[1])
				info, err := os.Lstat(src)
				if err != nil {
					if os.IsNotExist(err) {
						continue
					}
					return false, err
				}
				if !info.IsDir() {
					continue
				}
				if _, err := os.Lstat(dst); err == nil {
					return false, fmt.Errorf("无法迁移 %s：目标已存在", src)
				}
				if err := os.Rename(src, dst); err != nil {
					return false, err
				}
				changed = true
			}
		}
	}
	return changed, nil
}
