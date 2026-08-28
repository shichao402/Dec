package secrets

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ScanSyncRoot 递归扫描 SyncTarget.LocalRoot，返回相对 LocalRoot 的 SecureNote 列表。
func ScanSyncRoot(projectRoot string, target SyncTarget) ([]SecureNote, error) {
	rootAbs, err := ResolveAbsDir(projectRoot, target)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(rootAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s 不是目录", target.LocalRoot)
	}

	var notes []SecureNote
	err = filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return err
		}
		noteRel, err := normalizeSyncRelPath(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", noteRel, err)
		}
		notes = append(notes, SecureNote{RelativePath: noteRel, Content: string(content)})
		return nil
	})
	return notes, err
}

// PushBundle 扫描 LocalRoot 并推送到远端 folder（create/update，不删除）。
//
// 本地新文件会创建远端 Note；远端有而本地缺的路径记入 MissingLocal，绝不删远端。
func PushBundle(ctx context.Context, client Client, req PushBundleRequest) (*PushBundleResult, error) {
	if client == nil {
		client = DefaultClient()
	}
	target := req.Target
	if err := RequireDeclared(target); err != nil {
		return nil, err
	}

	localNotes, err := ScanSyncRoot(req.ProjectRoot, target)
	if err != nil {
		return nil, err
	}
	localSet := localSetFromNotes(localNotes)

	remote, err := client.ListNotes(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("列出 %s 的 Secure Note 失败: %w", target.Address, err)
	}
	var missing []string
	for _, note := range remote {
		rel, relErr := normalizeSyncRelPath(note.Name)
		if relErr != nil {
			missing = append(missing, note.Name)
			continue
		}
		if _, has := localSet[rel]; !has {
			missing = append(missing, rel)
		}
	}

	notes := append([]SecureNote(nil), localNotes...)
	if len(notes) == 0 {
		return &PushBundleResult{MissingLocal: missing}, nil
	}

	candidates := make([]LandingCandidate, 0, len(localNotes))
	for _, note := range localNotes {
		candidates = append(candidates, LandingCandidate{
			Address:      target.Address,
			LocalRoot:    target.LocalRoot,
			RelativePath: note.RelativePath,
			Plane:        planeOf(target),
		})
	}
	if err := ValidateLandingPaths(req.ProjectRoot, candidates); err != nil {
		return nil, err
	}

	result, err := client.PushBundle(ctx, req, notes)
	if err != nil {
		return nil, err
	}
	if result != nil {
		result.MissingLocal = missing
		localPaths := make([]string, 0, len(localNotes))
		for _, note := range localNotes {
			localPaths = append(localPaths, note.RelativePath)
		}
		result.Paths = localPaths
	}
	return result, nil
}

// AddSecureNote 在指定 SyncTarget 下登记/创建一条 Secure Note。
// noteRel 是相对 LocalRoot 的路径；本地文件必须已存在。
func AddSecureNote(ctx context.Context, client Client, projectRoot string, target SyncTarget, noteRel string) error {
	if client == nil {
		client = DefaultClient()
	}
	if err := RequireDeclared(target); err != nil {
		return err
	}
	if target.Address == "" || target.LocalRoot == "" {
		return fmt.Errorf("SyncTarget 不完整")
	}
	rel, err := normalizeSyncRelPath(noteRel)
	if err != nil {
		return err
	}
	if err := ValidateLandingPaths(projectRoot, []LandingCandidate{{
		Address:      target.Address,
		LocalRoot:    target.LocalRoot,
		RelativePath: rel,
		Plane:        planeOf(target),
	}}); err != nil {
		return err
	}
	abs, err := AbsolutePath(projectRoot, target, rel)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", rel, err)
	}

	req := PushBundleRequest{
		ProjectRoot: projectRoot,
		Target:      target,
	}
	_, err = client.PushBundle(ctx, req, []SecureNote{{RelativePath: rel, Content: string(content)}})
	return err
}

func localSetFromNotes(notes []SecureNote) map[string]SecureNote {
	m := make(map[string]SecureNote, len(notes))
	for _, note := range notes {
		m[note.RelativePath] = note
	}
	return m
}

