package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MigrateBundle 自动迁移 Bitwarden note 名与本地 legacy 路径（幂等）。
func MigrateBundle(ctx context.Context, client Client, req MigrateBundleRequest) (*MigrateBundleResult, error) {
	if client == nil {
		client = DefaultClient()
	}
	secretsBundleName := req.Binding.SecretsBundleName
	if secretsBundleName == "" {
		secretsBundleName = req.DecBundleName
	}

	result := &MigrateBundleResult{}
	if req.ProjectRoot != "" {
		moved, err := migrateLocalSecretsFiles(req.ProjectRoot, req.DecBundleName, secretsBundleName)
		if err != nil {
			return nil, err
		}
		result.MovedLocal = moved
	}

	bwResult, err := client.MigrateBundle(ctx, req)
	if err != nil {
		return nil, err
	}
	if bwResult != nil {
		result.RenamedNotes = append(result.RenamedNotes, bwResult.RenamedNotes...)
		result.SkippedCiphers = append(result.SkippedCiphers, bwResult.SkippedCiphers...)
	}
	return result, nil
}

func migrateLocalSecretsFiles(projectRoot, decBundleName, secretsBundleName string) ([]string, error) {
	var moved []string
	for _, pair := range localMigrationPairs(projectRoot, decBundleName, secretsBundleName) {
		src := filepath.Join(projectRoot, filepath.FromSlash(pair.from))
		dst := filepath.Join(projectRoot, filepath.FromSlash(pair.to))
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return moved, err
		}
		if _, err := os.Stat(dst); err == nil {
			if _, srcErr := os.Stat(src); srcErr == nil {
				if rmErr := os.Remove(src); rmErr != nil {
					return moved, fmt.Errorf("删除重复 %s 失败: %w", pair.from, rmErr)
				}
				moved = append(moved, pair.from+" (已有 "+pair.to+"，已清理)")
				cleanupEmptyLegacyDirs(projectRoot, pair.from)
			}
			continue
		} else if !os.IsNotExist(err) {
			return moved, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return moved, fmt.Errorf("创建目录 %s 失败: %w", filepath.Dir(dst), err)
		}
		if err := os.Rename(src, dst); err != nil {
			return moved, fmt.Errorf("移动 %s → %s 失败: %w", pair.from, pair.to, err)
		}
		moved = append(moved, pair.from+" → "+pair.to)
		cleanupEmptyLegacyDirs(projectRoot, pair.from)
	}
	return moved, nil
}

type localMigrationPair struct {
	from string
	to   string
}

func localMigrationPairs(projectRoot, decBundleName, secretsBundleName string) []localMigrationPair {
	var pairs []localMigrationPair
	seen := make(map[string]struct{})

	addPair := func(from, bundleRel string) {
		to, err := SecretsLocalPath(secretsBundleName, bundleRel)
		if err != nil {
			return
		}
		if from == to {
			return
		}
		key := from + "\x00" + to
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		pairs = append(pairs, localMigrationPair{from: from, to: to})
	}

	for _, bundleRel := range knownBundleRelativePaths(decBundleName, secretsBundleName) {
		addPair(".config/"+bundleRel, bundleRel)
		intermediate := secretsBundlePrefix(secretsBundleName) + ".config/" + bundleRel
		addPair(intermediate, bundleRel)
	}

	bundleDir := SecretsBundleDir(projectRoot, secretsBundleName)
	_ = filepath.Walk(bundleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		relWithin, relErr := filepath.Rel(bundleDir, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(relWithin)
		if !strings.HasPrefix(relSlash, ".config/") {
			return nil
		}
		bundleRel := canonicalWithinBundlePath(relSlash)
		from, fromErr := normalizeProjectRelativePath(secretsBundlePrefix(secretsBundleName) + relSlash)
		if fromErr != nil {
			return nil
		}
		addPair(from, bundleRel)
		return nil
	})

	return pairs
}

func knownBundleRelativePaths(decBundleName, secretsBundleName string) []string {
	switch {
	case decBundleName == "vikunja", secretsBundleName == "vikunja_workflow":
		return []string{"mise/conf.d/vikunja.toml"}
	default:
		return nil
	}
}

func cleanupEmptyLegacyDirs(projectRoot, movedFrom string) {
	dir := filepath.Dir(filepath.Join(projectRoot, filepath.FromSlash(movedFrom)))
	for dir != projectRoot && strings.HasPrefix(dir, projectRoot) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// MigrateConfigIfNeeded 将废弃的 folder 字段迁移为 secrets_bundle 并回写配置（幂等）。
func MigrateConfigIfNeeded() (bool, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return false, err
	}
	changed := false
	for i := range cfg.Bundles {
		b := &cfg.Bundles[i]
		if strings.TrimSpace(b.Folder) == "" {
			continue
		}
		if b.SecretsBundleName == "" {
			b.SecretsBundleName = strings.TrimSpace(b.Folder)
		}
		b.Folder = ""
		changed = true
	}
	if applyDefaultBindings(cfg) {
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := SaveConfig(cfg); err != nil {
		return false, err
	}
	return true, nil
}

func applyDefaultBindings(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	changed := false
	for _, decBundle := range []string{"vikunja"} {
		secretsBundle := defaultSecretsBundleName(decBundle)
		if secretsBundle == decBundle {
			continue
		}
		found := false
		for _, b := range cfg.Bundles {
			if b.DecBundleName == decBundle {
				found = true
				break
			}
		}
		if found {
			continue
		}
		cfg.Bundles = append(cfg.Bundles, BundleBinding{
			DecBundleName:     decBundle,
			SecretsBundleName: secretsBundle,
		})
		changed = true
	}
	return changed
}
