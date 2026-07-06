package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ScanSecretsBundleFiles 扫描 `.secrets/<secrets_bundle>/` 下文件，返回待推送 Secure Note。
func ScanSecretsBundleFiles(projectRoot, secretsBundleName string) ([]SecureNote, error) {
	dir := SecretsBundleDir(projectRoot, secretsBundleName)
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q 不是目录", dir)
	}

	var notes []SecureNote
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relWithinBundle, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if IsIntegrationAuthRelWithinBundle(relWithinBundle) {
			return nil
		}
		notePath, err := NotePathForBundleFile(secretsBundleName, relWithinBundle)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", notePath, err)
		}
		notes = append(notes, SecureNote{
			RelativePath: notePath,
			Content:      string(content),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return notes, nil
}

// PushBundle 扫描本地 `.secrets/<secrets_bundle>/` 并推送到 Bitwarden。
func PushBundle(ctx context.Context, client Client, req PushBundleRequest) (*PushBundleResult, error) {
	if client == nil {
		client = DefaultClient()
	}

	secretsBundleName := req.Binding.SecretsBundleName
	if secretsBundleName == "" {
		secretsBundleName = req.DecBundleName
	}

	notes, err := ScanSecretsBundleFiles(req.ProjectRoot, secretsBundleName)
	if err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return &PushBundleResult{}, nil
	}

	paths := make([]string, 0, len(notes))
	for _, note := range notes {
		landing, err := SecretsLocalPath(secretsBundleName, note.RelativePath)
		if err != nil {
			return nil, err
		}
		paths = append(paths, landing)
	}
	if err := ValidateNoOverlap(req.ProjectRoot, paths); err != nil {
		return nil, err
	}

	return client.PushBundle(ctx, req, notes)
}
