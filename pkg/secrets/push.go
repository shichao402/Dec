package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FolderNameFor 解析一次操作对应的 Bitwarden folder 名。
// 未显式绑定时退回 Dec bundle 名。
func FolderNameFor(binding BundleBinding, decBundleName string) string {
	if name := strings.TrimSpace(binding.SecretsBundleName); name != "" {
		return name
	}
	return strings.TrimSpace(decBundleName)
}

// PushBundle 按远端 folder 的 note 列表读取本地对应文件并推送。
//
// 权威索引是远端 folder 的 note 列表，不是本地目录：落地路径散在项目根，
// 没有可靠的本地枚举方式。本地缺文件只记进 MissingLocal 报告，绝不删远端
// ——枚举漏一个就等于静默删掉一条真密钥。
//
// 新增 note 走 AddSecureNote，不由 push 隐式创建。
func PushBundle(ctx context.Context, client Client, req PushBundleRequest) (*PushBundleResult, error) {
	if client == nil {
		client = DefaultClient()
	}
	folder := FolderNameFor(req.Binding, req.DecBundleName)

	remote, err := client.ListFolderNotes(ctx, folder)
	if err != nil {
		return nil, fmt.Errorf("列出 Bitwarden folder %q 的 Secure Note 失败: %w", folder, err)
	}
	if len(remote) == 0 {
		return &PushBundleResult{}, nil
	}

	notes := make([]SecureNote, 0, len(remote))
	var missing []string
	for _, note := range remote {
		rel, normErr := normalizeProjectRelativePath(note.Name)
		if normErr != nil {
			return nil, fmt.Errorf("远端 Secure Note 名 %q 不是合法的项目根相对路径: %w", note.Name, normErr)
		}
		content, readErr := os.ReadFile(filepath.Join(req.ProjectRoot, filepath.FromSlash(rel)))
		if readErr != nil {
			if os.IsNotExist(readErr) {
				missing = append(missing, rel)
				continue
			}
			return nil, fmt.Errorf("读取 %s 失败: %w", rel, readErr)
		}
		notes = append(notes, SecureNote{RelativePath: rel, Content: string(content)})
	}

	if len(notes) == 0 {
		return &PushBundleResult{MissingLocal: missing}, nil
	}

	candidates := make([]LandingCandidate, 0, len(notes))
	for _, note := range notes {
		candidates = append(candidates, LandingCandidate{Folder: folder, RelativePath: note.RelativePath})
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
	}
	return result, nil
}

// AddSecureNote 把项目内一个已存在的文件登记为新的 Secure Note。
//
// 这是新增 secret 的唯一入口：note 名即该文件的项目根相对路径，
// folder 决定归属分组。已存在同名 note 时按更新处理。
func AddSecureNote(ctx context.Context, client Client, projectRoot, folder, relPath string) error {
	if client == nil {
		client = DefaultClient()
	}
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return fmt.Errorf("必须指定 Bitwarden folder")
	}
	rel, err := normalizeProjectRelativePath(relPath)
	if err != nil {
		return err
	}
	if err := ValidateLandingPaths(projectRoot, []LandingCandidate{{Folder: folder, RelativePath: rel}}); err != nil {
		return err
	}
	content, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(rel)))
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", rel, err)
	}

	_, err = client.PushBundle(ctx, PushBundleRequest{
		ProjectRoot: projectRoot,
		Binding:     BundleBinding{SecretsBundleName: folder},
	}, []SecureNote{{RelativePath: rel, Content: string(content)}})
	return err
}
