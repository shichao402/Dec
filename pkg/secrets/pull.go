package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// WriteSecureNotes 将 Secure Note 写入项目根相对路径，返回已写入路径列表。
// 调用方必须已经通过 ValidateLandingPaths 校验过这些路径。
func WriteSecureNotes(projectRoot string, notes []SecureNote) ([]string, error) {
	written := make([]string, 0, len(notes))
	for _, note := range notes {
		rel, err := normalizeProjectRelativePath(note.RelativePath)
		if err != nil {
			return written, err
		}
		dest := filepath.Join(projectRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return written, fmt.Errorf("创建目录 %s 失败: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, []byte(note.Content), 0600); err != nil {
			return written, fmt.Errorf("写入 %s 失败: %w", rel, err)
		}
		written = append(written, rel)
	}
	return written, nil
}

// ResolveBundleNotes 取回单个 secrets bundle 的 Secure Note，但不写盘。
//
// Note 名就是落地路径，不做任何映射：不加 `.secrets/` 前缀、不插 folder 名、
// 不剥 `.config/`。拆出取回阶段是为了让调用方能在写任何文件之前做跨 folder 的
// 全局校验。
func ResolveBundleNotes(ctx context.Context, client Client, req PullBundleRequest) ([]SecureNote, error) {
	if client == nil {
		client = DefaultClient()
	}
	result, err := client.PullBundle(ctx, req)
	if err != nil {
		return nil, err
	}

	mapped := make([]SecureNote, 0, len(result.Notes))
	seenLanding := make(map[string]struct{}, len(result.Notes))
	for _, note := range result.Notes {
		landing, normErr := normalizeProjectRelativePath(note.RelativePath)
		if normErr != nil {
			return nil, normErr
		}
		if _, dup := seenLanding[landing]; dup {
			continue
		}
		seenLanding[landing] = struct{}{}
		mapped = append(mapped, SecureNote{
			RelativePath: landing,
			Content:      note.Content,
		})
	}
	return mapped, nil
}

// PullBundle 取回、校验并落地单个 secrets bundle。
func PullBundle(ctx context.Context, client Client, req PullBundleRequest) ([]string, error) {
	notes, err := ResolveBundleNotes(ctx, client, req)
	if err != nil {
		return nil, err
	}

	folder := FolderNameFor(req.Binding, req.DecBundleName)
	candidates := make([]LandingCandidate, 0, len(notes))
	for _, note := range notes {
		candidates = append(candidates, LandingCandidate{Folder: folder, RelativePath: note.RelativePath})
	}
	if err := ValidateLandingPaths(req.ProjectRoot, candidates); err != nil {
		return nil, err
	}

	return WriteSecureNotes(req.ProjectRoot, notes)
}
