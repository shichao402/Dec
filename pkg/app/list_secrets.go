package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/secrets"
)

// SecretFileMetadata 描述一条私密资产的元数据（不含内容）。
type SecretFileMetadata struct {
	SecretsBundle     string `json:"secrets_bundle"`
	RelWithinBundle   string `json:"rel_within_bundle"`
	ProjectRelPath    string `json:"project_rel_path"`
	LocalExists       bool   `json:"local_exists"`
	LocalSizeBytes    int64  `json:"local_size_bytes,omitempty"`
	LocalModifiedUnix int64  `json:"local_modified_unix,omitempty"`
	RemoteExists      *bool  `json:"remote_exists,omitempty"`
}

// ListSecretsMetadataResult 汇总私密资产元数据列表。
type ListSecretsMetadataResult struct {
	BitwardenConfigured bool                 `json:"bitwarden_configured"`
	SessionActive       bool                 `json:"session_active"`
	RemoteChecked       bool                 `json:"remote_checked"`
	Files               []SecretFileMetadata `json:"files"`
	SkippedReason       string               `json:"skipped_reason,omitempty"`
}

// ListSecretsMetadata 列出项目私密资产元数据；绝不返回文件或 Note 正文。
// includeRemote 为 true 且 Bitwarden 已配置时，会按需触发解锁并检查远端是否存在对应 Note。
func ListSecretsMetadata(ctx context.Context, projectRoot string, includeRemote bool, reporter Reporter) (*ListSecretsMetadataResult, error) {
	reporter = defaultReporter(reporter)
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return nil, fmt.Errorf("项目根目录不能为空")
	}

	result := &ListSecretsMetadataResult{
		Files: []SecretFileMetadata{},
	}

	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	result.BitwardenConfigured = configured
	result.SessionActive = secrets.HasSession()

	byKey := make(map[string]*SecretFileMetadata)

	addLocal := func(secretsBundle, relWithinBundle, projectRel string, info os.FileInfo) {
		key := secretsBundle + "\x00" + relWithinBundle
		meta, ok := byKey[key]
		if !ok {
			meta = &SecretFileMetadata{
				SecretsBundle:   secretsBundle,
				RelWithinBundle: relWithinBundle,
				ProjectRelPath:  projectRel,
			}
			byKey[key] = meta
		}
		meta.LocalExists = true
		if info != nil {
			meta.LocalSizeBytes = info.Size()
			meta.LocalModifiedUnix = info.ModTime().Unix()
		}
	}

	bundleNames, err := secrets.ListSecretsBundleDirNames(projectRoot)
	if err != nil {
		return nil, err
	}
	for _, secretsBundle := range bundleNames {
		if err := walkLocalSecretFiles(projectRoot, secretsBundle, addLocal); err != nil {
			return nil, err
		}
	}

	if includeRemote && configured {
		if !secrets.HasSession() {
			emit(reporter, EventInfo, "secrets.list", "[auth] secrets list: Bitwarden session required", nil)
			if err := ensureBitwardenSession(ctx, reporter, "secrets.list"); err != nil {
				result.SkippedReason = "远端未检查: " + err.Error()
				return finalizeSecretsMetadata(result, byKey), nil
			}
			result.SessionActive = true
		}
		if secrets.HasSession() && secrets.HasUserKey() {
			if err := mergeRemoteSecretMetadata(ctx, projectRoot, byKey, reporter); err != nil {
				result.SkippedReason = "远端检查部分失败: " + err.Error()
			} else {
				result.RemoteChecked = true
			}
		}
	}

	return finalizeSecretsMetadata(result, byKey), nil
}

func finalizeSecretsMetadata(result *ListSecretsMetadataResult, byKey map[string]*SecretFileMetadata) *ListSecretsMetadataResult {
	files := make([]SecretFileMetadata, 0, len(byKey))
	for _, meta := range byKey {
		files = append(files, *meta)
	}
	sortSecretMetadata(files)
	result.Files = files
	return result
}

func sortSecretMetadata(files []SecretFileMetadata) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].SecretsBundle != files[j].SecretsBundle {
			return files[i].SecretsBundle < files[j].SecretsBundle
		}
		return files[i].RelWithinBundle < files[j].RelWithinBundle
	})
}

func walkLocalSecretFiles(projectRoot, secretsBundle string, addLocal func(string, string, string, os.FileInfo)) error {
	dir := secrets.SecretsBundleDir(projectRoot, secretsBundle)
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
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
		relWithinBundle = filepath.ToSlash(relWithinBundle)
		if secrets.IsIntegrationAuthRelWithinBundle(relWithinBundle) {
			return nil
		}
		projectRel, err := secrets.SecretsLocalPath(secretsBundle, relWithinBundle)
		if err != nil {
			return nil
		}
		addLocal(secretsBundle, relWithinBundle, projectRel, info)
		return nil
	})
}

func mergeRemoteSecretMetadata(ctx context.Context, projectRoot string, byKey map[string]*SecretFileMetadata, reporter Reporter) error {
	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return err
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		return err
	}
	plan, err := planSecretsSync(projectRoot, projectConfig.EnabledBundles, cfg)
	if err != nil {
		return err
	}
	client := secretsClientFactory()

	markRemote := func(secretsBundle, relWithinBundle string) {
		key := secretsBundle + "\x00" + relWithinBundle
		meta, ok := byKey[key]
		if !ok {
			projectRel, mapErr := secrets.SecretsLocalPath(secretsBundle, relWithinBundle)
			if mapErr != nil {
				return
			}
			meta = &SecretFileMetadata{
				SecretsBundle:   secretsBundle,
				RelWithinBundle: relWithinBundle,
				ProjectRelPath:  projectRel,
			}
			byKey[key] = meta
		}
		exists := true
		meta.RemoteExists = &exists
	}

	for _, decBundle := range plan.EnabledBundles {
		binding := cfg.ResolveBinding(decBundle)
		secretsBundle := binding.SecretsBundleName
		if secretsBundle == "" {
			secretsBundle = decBundle
		}
		pullResult, pullErr := client.PullBundle(ctx, secrets.PullBundleRequest{
			DecBundleName: decBundle,
			Binding:       binding,
		})
		if pullErr != nil {
			emit(reporter, EventWarn, "secrets.list", fmt.Sprintf("拉取远端元数据失败 bundle=%s: %v", decBundle, pullErr), nil)
			continue
		}
		for _, note := range pullResult.Notes {
			_ = note.Content // 明确丢弃，不暴露
			rel := note.RelativePath
			if rel == "" {
				continue
			}
			canon, canonErr := secrets.CanonicalNoteName(secretsBundle, rel)
			if canonErr != nil {
				continue
			}
			markRemote(secretsBundle, canon)
		}
	}

	if plan.ProjectSecretsName != "" {
		binding := secrets.ProjectSecretsBinding(plan.ProjectSecretsName)
		secretsBundle := binding.SecretsBundleName
		pullResult, pullErr := client.PullBundle(ctx, secrets.PullBundleRequest{
			DecBundleName: plan.ProjectSecretsName,
			Binding:       binding,
		})
		if pullErr != nil {
			emit(reporter, EventWarn, "secrets.list", fmt.Sprintf("拉取 project secrets 元数据失败: %v", pullErr), nil)
		} else {
			for _, note := range pullResult.Notes {
				_ = note.Content
				canon, canonErr := secrets.CanonicalNoteName(secretsBundle, note.RelativePath)
				if canonErr != nil {
					continue
				}
				markRemote(secretsBundle, canon)
			}
		}
	}

	return nil
}
