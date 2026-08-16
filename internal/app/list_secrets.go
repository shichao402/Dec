package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/secrets"
)

// SecretFileMetadata 描述一条私密资产的元数据（不含内容）。
// ProjectRelPath 是项目根相对落地路径；Bitwarden Note 名是相对 SyncTarget.LocalRoot 的路径。
type SecretFileMetadata struct {
	SecretsBundle     string `json:"secrets_bundle"`
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
	return ListWorkspaceSecretsMetadata(ctx, NewWorkspace(WorkspaceProject, projectRoot), includeRemote, reporter)
}

// ListWorkspaceSecretsMetadata 按平面列出私密资产元数据（ADR 0009 平面隔离）。
// 用户平面读机器根 secrets，项目平面读项目根 secrets；绝不返回正文。
func ListWorkspaceSecretsMetadata(ctx context.Context, workspace Workspace, includeRemote bool, reporter Reporter) (*ListSecretsMetadataResult, error) {
	reporter = defaultReporter(reporter)
	if strings.TrimSpace(workspace.Root) == "" {
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

	// 权威索引在远端：密文落在 .secrets/<sync-root>/ 下，无法靠扫目录判断
	// 哪些文件归 Bitwarden 管。不查远端就只能给出空列表。
	if !includeRemote {
		result.SkippedReason = "未查询远端：secrets 清单以 Bitwarden folder 的 Note 列表为准，请设置 includeRemote"
		return finalizeSecretsMetadata(result, byKey), nil
	}

	if configured {
		if !secrets.HasSession() {
			if err := ensureBitwardenSession(ctx, reporter, "secrets.list"); err != nil {
				result.SkippedReason = "远端未检查: " + err.Error()
				return finalizeSecretsMetadata(result, byKey), nil
			}
			result.SessionActive = true
		}
		if secrets.HasSession() && secrets.HasUserKey() {
			if err := mergeRemoteSecretMetadataForWorkspace(ctx, workspace, byKey, reporter); err != nil {
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
		return files[i].ProjectRelPath < files[j].ProjectRelPath
	})
}

func mergeRemoteSecretMetadataForWorkspace(ctx context.Context, workspace Workspace, byKey map[string]*SecretFileMetadata, reporter Reporter) error {
	projectRoot := workspace.Root
	projectConfig, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return err
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		return err
	}
	plan, err := planWorkspaceSecretsBrowse(workspace, projectConfig.EnabledBundles, cfg, reporter)
	if err != nil {
		return err
	}
	client := secretsClientFactory()
	targets := append([]secrets.SyncTarget(nil), plan.Targets...)
	targets = append(targets, discoverRemoteSecretTargets(
		ctx, client, workspace, loadVaultBundleScopes(workspace, reporter), targets, reporter)...)

	// ListFolderNotes 只回 note 名（相对同步根），不回正文——元数据接口绝不能碰到密钥内容。
	for _, target := range targets {
		label := formatSyncTargetLabel(target)
		notes, listErr := client.ListFolderNotes(ctx, target.Folder)
		if listErr != nil {
			emit(reporter, EventWarn, "secrets.list", fmt.Sprintf("列出远端 %s 元数据失败: %v", label, listErr), nil)
			continue
		}
		for _, note := range notes {
			noteRel := strings.TrimSpace(note.Name)
			if noteRel == "" {
				continue
			}
			projectRel, relErr := secrets.ProjectRelPath(target, noteRel)
			if relErr != nil {
				emit(reporter, EventWarn, "secrets.list", fmt.Sprintf("跳过非法 note 名 %q: %v", noteRel, relErr), nil)
				continue
			}
			key := target.Folder + "\x00" + projectRel
			meta, ok := byKey[key]
			if !ok {
				meta = &SecretFileMetadata{SecretsBundle: target.Folder, ProjectRelPath: projectRel}
				byKey[key] = meta
			}
			exists := true
			meta.RemoteExists = &exists
			localAbs, absErr := secrets.AbsolutePath(projectRoot, target, noteRel)
			if absErr != nil {
				localAbs = filepath.Join(projectRoot, filepath.FromSlash(projectRel))
			}
			if info, statErr := os.Stat(localAbs); statErr == nil {
				meta.LocalExists = true
				meta.LocalSizeBytes = info.Size()
				meta.LocalModifiedUnix = info.ModTime().Unix()
			}
		}
	}
	return nil
}
