package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// WriteSecureNotes 将 Secure Note 写入项目根相对路径，返回已写入路径列表。
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

// PullBundle 从 Client 拉取并落地单个 secrets bundle。
func PullBundle(ctx context.Context, client Client, req PullBundleRequest) ([]string, error) {
	if client == nil {
		client = DefaultClient()
	}
	result, err := client.PullBundle(ctx, req)
	if err != nil {
		return nil, err
	}

	secretsBundleName := req.Binding.SecretsBundleName
	if secretsBundleName == "" {
		secretsBundleName = req.DecBundleName
	}

	mapped := make([]SecureNote, 0, len(result.Notes))
	seenLanding := make(map[string]struct{}, len(result.Notes))
	for _, note := range result.Notes {
		landing, mapErr := LandingPathForNote(secretsBundleName, note.RelativePath)
		if mapErr != nil {
			return nil, mapErr
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
	return WriteSecureNotes(req.ProjectRoot, mapped)
}
