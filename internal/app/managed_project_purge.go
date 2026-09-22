package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/ide"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
)

// PurgeManagedProjectResult 描述单项目「移除管理」深清结果。
type PurgeManagedProjectResult struct {
	Root     string
	Removed  bool
	Deleted  []string
	Modified []string
	Revoked  []string
	Warnings []string
}

// PurgeManagedProject 清理该目录的 Dec 接管痕迹后从受管列表摘掉登记。
// 不删业务源码；.secrets 下保留 integration 测试凭据与隔离 DEC_HOME。
func PurgeManagedProject(ctx context.Context, root string, reporter Reporter) (*PurgeManagedProjectResult, error) {
	reporter = defaultReporter(reporter)
	normalized, err := config.NormalizeManagedProjectRoot(root)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	result := &PurgeManagedProjectResult{Root: normalized}
	warn := func(message string) {
		result.Warnings = append(result.Warnings, message)
		emit(reporter, EventWarn, "projects.purge", message, nil)
	}
	trackDelete := func(path string) {
		if !pathExists(path) {
			return
		}
		if removeErr := os.RemoveAll(path); removeErr != nil {
			warn(fmt.Sprintf("删除失败 %s: %v", path, removeErr))
			return
		}
		result.Deleted = append(result.Deleted, path)
	}

	for _, item := range projectGCMItems(normalized) {
		data, readErr := os.ReadFile(item.path)
		if readErr != nil {
			warn(fmt.Sprintf("读取 GCM 落地失败 %s: %v", item.path, readErr))
			continue
		}
		_, revokeErr := handler.RevokeNotes(ctx, nil, []handler.Item{{
			Source: handler.SourceNote, Name: item.name, NoteContent: string(data),
			ProjectRoot: item.projectRoot, ProjectScoped: true,
		}})
		if revokeErr != nil {
			warn(fmt.Sprintf("撤销 GCM 凭据失败 %s: %v", item.name, revokeErr))
		} else {
			result.Revoked = append(result.Revoked, item.name)
		}
	}

	scope := cleanupScope{plane: ide.PlaneProject, projectRoot: normalized}
	for _, ideName := range ide.List() {
		impl := ide.Get(ideName)
		for _, candidate := range managedIDEPaths(impl, scope, home) {
			trackDelete(candidate.path)
		}
		names, removeErr := ide.RemoveDecMCPEntries(impl, scope.plane, scope.projectRoot, home)
		if removeErr != nil {
			warn(fmt.Sprintf("更新 %s MCP 配置失败: %v", ideName, removeErr))
		} else if len(names) > 0 {
			result.Modified = append(result.Modified, impl.MCPConfigPathForPlane(scope.plane, scope.projectRoot, home))
		}
	}

	if cleanupErr := secrets.CleanupProjectCredentialScope(normalized); cleanupErr != nil {
		warn(fmt.Sprintf("清理项目凭据配置失败 %s: %v", normalized, cleanupErr))
	}

	trackDelete(filepath.Join(normalized, ".dec"))

	secretsRoot := filepath.Join(normalized, ".secrets")
	if pathExists(secretsRoot) {
		keeps := []string{
			filepath.Join(normalized, filepath.FromSlash(secrets.IntegrationAuthRel)),
			filepath.Join(normalized, filepath.FromSlash(secrets.IntegrationDecHomeRel)),
		}
		if cleanupErr := removeTreeExcept(secretsRoot, keeps); cleanupErr != nil {
			warn(fmt.Sprintf("清理项目 secrets 失败 %s: %v", normalized, cleanupErr))
		} else {
			result.Modified = append(result.Modified, secretsRoot)
		}
	}

	removed, err := config.RemoveManagedProject(normalized)
	if err != nil {
		return nil, err
	}
	result.Removed = removed
	emit(reporter, EventInfo, "projects.purge",
		fmt.Sprintf("已移除管理 %s：删除 %d，修改 %d，撤销凭据 %d",
			normalized, len(result.Deleted), len(result.Modified), len(result.Revoked)), nil)
	return result, nil
}

// projectGCMItems 只扫单个项目 .secrets 下的 GCM 落地，不碰本机 secrets 根。
func projectGCMItems(projectRoot string) []cleanupGCMItem {
	return localGCMItems("", []string{projectRoot})
}
