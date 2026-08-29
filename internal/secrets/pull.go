package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// WriteSecureNotes 将 Secure Note 写入 SyncTarget.LocalRoot，返回展示用相对路径。
func WriteSecureNotes(projectRoot string, target SyncTarget, notes []SecureNote) ([]string, error) {
	written := make([]string, 0, len(notes))
	for _, note := range notes {
		display, err := RootRelPath(target, note.RelativePath)
		if err != nil {
			return written, err
		}
		dest, err := AbsolutePath(projectRoot, target, note.RelativePath)
		if err != nil {
			return written, err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return written, fmt.Errorf("创建目录 %s 失败: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, []byte(note.Content), 0600); err != nil {
			return written, fmt.Errorf("写入 %s 失败: %w", display, err)
		}
		written = append(written, display)
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
		// Client 已按远端寻址域解码过条目名，这里只做同步根内的路径校验。
		rel, relErr := normalizeSyncRelPath(note.RelativePath)
		if relErr != nil {
			return nil, nil, relErr
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
			Address:      target.Address,
			LocalRoot:    target.LocalRoot,
			RelativePath: note.RelativePath,
			Plane:        planeOf(target),
		})
	}
	if err := ValidateLandingPaths(req.ProjectRoot, candidates); err != nil {
		return nil, err
	}
	return WriteSecureNotes(req.ProjectRoot, target, notes)
}

// requestTarget 校验请求携带的 SyncTarget：项目模型下没有可推断的默认目标，
// 调用方必须给出声明型 target。
func requestTarget(req PullBundleRequest) (SyncTarget, error) {
	target := req.Target
	if target.Declared() {
		return target, nil
	}
	scope, err := target.Scope()
	if err != nil {
		return SyncTarget{}, err
	}
	return NewPSyncTarget(scope.P, scope.Plane)
}
