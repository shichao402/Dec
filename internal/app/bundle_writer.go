package app

import (
	"context"
	"fmt"

	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

// PWriter 是 ADR 0016 规定的唯一权威写入口。P 是所有新写路径的主语；
// SyncTarget、旧 bundle scope 和裸 project folder 只能用于兼容读取/迁移。
type PWriter struct{}

func DefaultPWriter() PWriter { return PWriter{} }

// BundleWriter / DefaultBundleWriter 保留源码兼容；实际语义和实现均由 PWriter 提供。
type BundleWriter = PWriter

type ProjectWriter = PWriter

func DefaultProjectWriter() ProjectWriter { return DefaultPWriter() }

func (w PWriter) BindHomeProject(projectRoot string, reporter Reporter) (*ConfigInitPreparation, error) {
	return w.BindHomeP(projectRoot, reporter)
}

// SaveProjects 保存 P 选择：user 平面写 enabled_projects；project 平面写家 P 的 requires。
func (PWriter) SaveProjects(workspace Workspace, names []string, reporter Reporter) (*SaveBundleSelectionResult, error) {
	return saveWorkspacePSelection(workspace, names, reporter)
}

// SaveEnabledBundles 是 wire/source 兼容名，新仓库中按 P 语义执行。
func (w PWriter) SaveEnabledBundles(workspace Workspace, names []string, reporter Reporter) (*SaveBundleSelectionResult, error) {
	return w.SaveProjects(workspace, names, reporter)
}

func (PWriter) PushWorkspace(ctx context.Context, workspace Workspace, reporter Reporter) (*PushProjectAssetsResult, error) {
	return PushWorkspaceAssets(ctx, workspace, reporter)
}

// BindHomeP 初始化本地配置并确保仓库中存在家 P。
func (PWriter) BindHomeP(projectRoot string, reporter Reporter) (*ConfigInitPreparation, error) {
	prepared, err := EnsureLocalProjectConfig(projectRoot, reporter)
	if err != nil || prepared == nil || prepared.ProjectConfig == nil {
		return prepared, err
	}
	name := prepared.ProjectConfig.ProjectName
	if !types.IsValidPName(name) {
		return nil, fmt.Errorf("目录名 %q 不能绑定为家 P：必须为小写 kebab-case", name)
	}
	if err := withAppWriteRepo(func(tx *repo.Transaction) error {
		projects, scanErr := pmodel.Scan(tx.WorkDir())
		if scanErr != nil {
			return scanErr
		}
		if _, exists := projects[name]; exists {
			return nil
		}
		if err := pmodel.SaveManifest(tx.WorkDir(), types.P{Name: name, Title: name}); err != nil {
			return err
		}
		_, err := tx.CommitAndPush("p: initialize " + name)
		return err
	}); err != nil {
		return nil, err
	}
	prepared.Model = "p"
	prepared.HomeProject = name
	return prepared, nil
}

// ValidateRemoteRegisterScope 校验 Remote 登记归属（已声明 P + 平面）。
func (PWriter) ValidateRemoteRegisterScope(workspace Workspace, scope secrets.RemoteScope) error {
	return ValidateRemoteRegisterScope(workspace, scope)
}

// PrepareRemoteRegister 准备私密半边登记会话。
func (PWriter) PrepareRemoteRegister(ctx context.Context, in RemoteRegisterInput, reporter Reporter) (*RemoteRegisterSession, error) {
	return PrepareRemoteRegister(ctx, in, reporter)
}

// CommitRemoteRegister 提交私密半边登记。
func (PWriter) CommitRemoteRegister(ctx context.Context, session RemoteRegisterSession, reporter Reporter) (*AddSecretResult, error) {
	return CommitRemoteRegister(ctx, session, reporter)
}

// CommitRemoteNoteEdit 提交 Note 编辑。
func (PWriter) CommitRemoteNoteEdit(ctx context.Context, session RemoteNoteEditSession, reporter Reporter) error {
	return CommitRemoteNoteEdit(ctx, session, reporter)
}

// CommitRemoteNoteRegister 提交 Note 登记（旧路径）。
func (PWriter) CommitRemoteNoteRegister(ctx context.Context, session RemoteNoteEditSession, reporter Reporter) error {
	return CommitRemoteNoteRegister(ctx, session, reporter)
}

// CommitRemoteSSHHostsEdit 提交 SSH Hosts 编辑。
func (PWriter) CommitRemoteSSHHostsEdit(ctx context.Context, session RemoteSSHHostsEditSession, reporter Reporter) error {
	return CommitRemoteSSHHostsEdit(ctx, session, reporter)
}

// RegisterRemoteNoteFromPath 从本地路径登记 Note。
func (PWriter) RegisterRemoteNoteFromPath(ctx context.Context, projectRoot string, scope secrets.RemoteScope, noteRel, localPath string, reporter Reporter) (*AddSecretResult, error) {
	return RegisterRemoteNoteFromPath(ctx, projectRoot, scope, noteRel, localPath, reporter)
}

// AddSecretToTarget 把本地文件登记为 Bundle 私密半边 Note（target 必须为已声明 bundle）。
func (PWriter) AddSecretToTarget(ctx context.Context, projectRoot string, target secrets.SyncTarget, noteRel string, reporter Reporter) (*AddSecretResult, error) {
	return AddSecretToTarget(ctx, projectRoot, target, noteRel, reporter)
}

// RemoveBundle 移除公开半边本地落地（不删 vault / BW，除非实现另有约定）。
func (PWriter) RemoveBundle(input RemoveBundleInput, reporter Reporter) (*RemoveBundleResult, error) {
	if usesP, _ := connectedRepositoryUsesPModel(); usesP {
		return removeP(input, reporter)
	}
	return RemoveBundle(input, reporter)
}

// DeleteItems 删除公开/私密选中项（Remote 页）。
func (PWriter) DeleteItems(ctx context.Context, input DeleteProjectInput, reporter Reporter) (*DeleteProjectResult, error) {
	return DeleteProjectItems(ctx, input, reporter)
}

// CleanupUnmanaged 删除非托管裸 folder 内容（只删 BW，不创建 Bundle）。文案钉死「非模型内写入」。
func (PWriter) CleanupUnmanaged(ctx context.Context, input DeleteProjectInput, reporter Reporter) (*DeleteProjectResult, error) {
	input.Mode = "remote"
	return DeleteRemoteOnly(ctx, input, reporter)
}
