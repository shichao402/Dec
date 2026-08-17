package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TypeDirMigrateResult 记录一次点类型目录存量迁移。
type TypeDirMigrateResult struct {
	RenamedNotes []string // "old -> new"
	RenamedSSH   []string
}

// MigrateTypeDirNames 将 folder 内旧标识一次性迁到点类型目录规范名，并尽量改本地文件。
// 主路径之后只认新名；本函数是唯一读旧约定的入口。
func MigrateTypeDirNames(ctx context.Context, client Client, projectRoot string, target SyncTarget) (*TypeDirMigrateResult, error) {
	if client == nil {
		client = DefaultClient()
	}
	if err := RequireDeclared(target); err != nil {
		return nil, err
	}
	folder := strings.TrimSpace(target.Folder)
	if folder == "" {
		return nil, fmt.Errorf("MigrateTypeDirNames 需要 Target.Folder")
	}
	binding := BundleBinding{
		DecBundleName:     target.Name,
		SecretsBundleName: folder,
	}
	result := &TypeDirMigrateResult{}

	notes, err := client.ListFolderNotes(ctx, folder)
	if err != nil {
		return nil, err
	}
	for _, note := range notes {
		oldPath := strings.TrimSpace(note.Name)
		if oldPath == "" {
			continue
		}
		newPath, ok := MigrateLegacyGitGCMPath(oldPath)
		if !ok {
			newPath, ok = MigrateLegacyEnvPath(oldPath)
		}
		if !ok {
			if _, _, parseErr := ParseTypePath(oldPath); parseErr != nil {
				return nil, parseErr
			}
			continue
		}
		if err := client.RenameSecureNote(ctx, RenameSecureNoteRequest{
			Binding: binding,
			OldPath: oldPath,
			NewPath: newPath,
			Target:  target,
		}); err != nil {
			return nil, fmt.Errorf("迁移 Note %q → %q: %w", oldPath, newPath, err)
		}
		result.RenamedNotes = append(result.RenamedNotes, oldPath+" -> "+newPath)

		oldAbs, absErr := AbsolutePath(projectRoot, target, oldPath)
		if absErr == nil {
			newAbs, absErr2 := AbsolutePath(projectRoot, target, newPath)
			if absErr2 == nil {
				if err := renameIfExistsAbs(oldAbs, newAbs); err != nil {
					return nil, fmt.Errorf("迁移本地文件 %s → %s: %w", oldPath, newPath, err)
				}
			}
		}
	}

	keys, err := client.ListFolderSSHKeys(ctx, folder)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		oldName := strings.TrimSpace(key.Name)
		if oldName == "" {
			continue
		}
		if !NeedsLegacySSHKeyMigrate(oldName) {
			if _, err := SSHKeyInstance(oldName); err != nil && strings.HasPrefix(oldName, ".") {
				return nil, err
			}
			continue
		}
		newName := CanonicalSSHKeyName(oldName)
		if err := client.RenameSSHKey(ctx, RenameSSHKeyRequest{
			Binding: binding,
			OldName: oldName,
			NewName: newName,
			Target:  target,
		}); err != nil {
			return nil, fmt.Errorf("迁移 SSH Key %q → %q: %w", oldName, newName, err)
		}
		result.RenamedSSH = append(result.RenamedSSH, oldName+" -> "+newName)
	}
	return result, nil
}

func renameIfExistsAbs(oldAbs, newAbs string) error {
	if oldAbs == newAbs {
		return nil
	}
	if _, err := os.Stat(oldAbs); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newAbs), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(newAbs); err == nil {
		return fmt.Errorf("目标已存在: %s", newAbs)
	}
	return os.Rename(oldAbs, newAbs)
}

// ValidateNoteTypePaths 校验一批 Note 相对路径：未知点目录硬失败。
func ValidateNoteTypePaths(noteRels []string) error {
	for _, rel := range noteRels {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		if _, _, err := ParseTypePath(rel); err != nil {
			return err
		}
	}
	return nil
}
