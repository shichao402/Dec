package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteSecureNotes 将 Secure Note 写入 SyncTarget.LocalRoot，返回已写入的项目根相对路径。
func WriteSecureNotes(projectRoot string, target SyncTarget, notes []SecureNote) ([]string, error) {
	written := make([]string, 0, len(notes))
	for _, note := range notes {
		projectRel, err := ProjectRelPath(target, note.RelativePath)
		if err != nil {
			return written, err
		}
		dest := filepath.Join(projectRoot, filepath.FromSlash(projectRel))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return written, fmt.Errorf("创建目录 %s 失败: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, []byte(note.Content), 0600); err != nil {
			return written, fmt.Errorf("写入 %s 失败: %w", projectRel, err)
		}
		written = append(written, projectRel)
	}
	return written, nil
}

// ResolveBundleNotes 取回单个 SyncTarget 的 Secure Note，但不写盘。
func ResolveBundleNotes(ctx context.Context, client Client, req PullBundleRequest) ([]SecureNote, error) {
	notes, _, err := ResolveBundle(ctx, client, req)
	return notes, err
}

// ResolveBundle 一次取回 Secure Notes 与 SSH Keys，不写盘。
// Note.RelativePath 已规范化为相对 LocalRoot 的路径。
func ResolveBundle(ctx context.Context, client Client, req PullBundleRequest) ([]SecureNote, []SSHKeyItem, error) {
	if client == nil {
		client = DefaultClient()
	}
	target, err := requestTarget(req)
	if err != nil {
		return nil, nil, err
	}
	req.Target = target

	result, err := client.PullBundle(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	mapped := make([]SecureNote, 0, len(result.Notes))
	seen := make(map[string]struct{}, len(result.Notes))
	for _, note := range result.Notes {
		rel, normErr := normalizeSyncRelPath(note.RelativePath)
		if normErr != nil {
			return nil, nil, normErr
		}
		if _, dup := seen[rel]; dup {
			continue
		}
		seen[rel] = struct{}{}
		mapped = append(mapped, SecureNote{RelativePath: rel, Content: note.Content})
	}
	keys := make([]SSHKeyItem, len(result.SSHKeys))
	copy(keys, result.SSHKeys)
	return mapped, keys, nil
}

// PullBundle 取回、校验并落地单个 SyncTarget。
func PullBundle(ctx context.Context, client Client, req PullBundleRequest) ([]string, error) {
	target, err := requestTarget(req)
	if err != nil {
		return nil, err
	}
	req.Target = target

	notes, err := ResolveBundleNotes(ctx, client, req)
	if err != nil {
		return nil, err
	}

	candidates := make([]LandingCandidate, 0, len(notes))
	for _, note := range notes {
		candidates = append(candidates, LandingCandidate{
			Folder:       target.Folder,
			LocalRoot:    target.LocalRoot,
			RelativePath: note.RelativePath,
		})
	}
	if err := ValidateLandingPaths(req.ProjectRoot, candidates); err != nil {
		return nil, err
	}
	return WriteSecureNotes(req.ProjectRoot, target, notes)
}

func requestTarget(req PullBundleRequest) (SyncTarget, error) {
	if req.Target.LocalRoot != "" && req.Target.Folder != "" {
		return req.Target, nil
	}
	name := strings.TrimSpace(req.DecBundleName)
	if name == "" {
		name = strings.TrimSpace(req.Binding.DecBundleName)
	}
	if name == ProjectSecretsDecBundleName || req.Target.Kind == SyncKindProject {
		folder := strings.TrimSpace(req.Binding.SecretsBundleName)
		if folder == "" {
			folder = strings.TrimSpace(req.Target.Folder)
		}
		projName := strings.TrimSpace(req.Target.Name)
		if projName == "" {
			projName = folder
		}
		return NewProjectSyncTarget(projName, folder)
	}
	return NewBundleSyncTarget(name, req.Binding.SecretsBundleName)
}
