package secrets

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FolderNameFor 解析一次操作对应的 Bitwarden folder 名。
func FolderNameFor(binding BundleBinding, decBundleName string) string {
	if name := strings.TrimSpace(binding.SecretsBundleName); name != "" {
		return name
	}
	return DefaultBundleFolder(decBundleName)
}

func folderNameForRequest(targetFolder string, binding BundleBinding, decBundleName string) string {
	if name := strings.TrimSpace(targetFolder); name != "" {
		return name
	}
	return FolderNameFor(binding, decBundleName)
}

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
	target, err := pushRequestTarget(req)
	if err != nil {
		return nil, err
	}
	req.Target = target
	req.Binding = BundleBinding{
		DecBundleName:     target.Name,
		SecretsBundleName: target.Folder,
	}
	if target.Kind == SyncKindProject {
		req.DecBundleName = ProjectSecretsDecBundleName
		req.Binding.DecBundleName = ProjectSecretsDecBundleName
	} else {
		req.DecBundleName = target.Name
	}

	localNotes, err := ScanSyncRoot(req.ProjectRoot, target)
	if err != nil {
		return nil, err
	}
	localSet := localSetFromNotes(localNotes)
	remoteLocal := make(map[string]struct{}, len(localSet))
	for rel := range localSet {
		remoteName, nameErr := RemoteNoteName(target, rel)
		if nameErr != nil {
			return nil, nameErr
		}
		remoteLocal[remoteName] = struct{}{}
	}

	remote, err := client.ListFolderNotes(ctx, target.Folder)
	if err != nil {
		return nil, fmt.Errorf("列出 Bitwarden folder %q 的 Secure Note 失败: %w", target.Folder, err)
	}
	var missing []string
	for _, note := range remote {
		rel, ok, mapErr := LocalNoteRelFromRemote(target, note.Name)
		if mapErr != nil {
			missing = append(missing, note.Name)
			continue
		}
		if !ok {
			continue
		}
		remoteName, _ := RemoteNoteName(target, rel)
		if _, has := remoteLocal[remoteName]; !has {
			missing = append(missing, rel)
		}
	}

	notes := make([]SecureNote, 0, len(localNotes))
	for _, note := range localNotes {
		remoteName, nameErr := RemoteNoteName(target, note.RelativePath)
		if nameErr != nil {
			return nil, nameErr
		}
		notes = append(notes, SecureNote{RelativePath: remoteName, Content: note.Content})
	}
	if len(notes) == 0 {
		return &PushBundleResult{MissingLocal: missing}, nil
	}

	candidates := make([]LandingCandidate, 0, len(localNotes))
	for _, note := range localNotes {
		candidates = append(candidates, LandingCandidate{
			Folder:       target.Folder,
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
	if target.Folder == "" || target.LocalRoot == "" {
		return fmt.Errorf("SyncTarget 不完整")
	}
	rel, err := normalizeSyncRelPath(noteRel)
	if err != nil {
		return err
	}
	if err := ValidateLandingPaths(projectRoot, []LandingCandidate{{
		Folder:       target.Folder,
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

	remoteName, err := RemoteNoteName(target, rel)
	if err != nil {
		return err
	}

	req := PushBundleRequest{
		ProjectRoot: projectRoot,
		Target:      target,
		Binding: BundleBinding{
			DecBundleName:     target.Name,
			SecretsBundleName: target.Folder,
		},
	}
	if target.Kind == SyncKindProject {
		req.DecBundleName = ProjectSecretsDecBundleName
		req.Binding.DecBundleName = ProjectSecretsDecBundleName
	} else {
		req.DecBundleName = target.Name
	}
	_, err = client.PushBundle(ctx, req, []SecureNote{{RelativePath: remoteName, Content: string(content)}})
	return err
}

func localSetFromNotes(notes []SecureNote) map[string]SecureNote {
	m := make(map[string]SecureNote, len(notes))
	for _, note := range notes {
		m[note.RelativePath] = note
	}
	return m
}

func pushRequestTarget(req PushBundleRequest) (SyncTarget, error) {
	name := strings.TrimSpace(req.DecBundleName)
	if name == "" {
		name = strings.TrimSpace(req.Binding.DecBundleName)
	}
	kind := req.Target.Kind
	if name == ProjectSecretsDecBundleName || kind == SyncKindProject {
		kind = SyncKindProject
	} else if kind == "" {
		kind = SyncKindBundle
	}
	return ResolveTarget(kind, name, req.Binding, req.Target)
}
