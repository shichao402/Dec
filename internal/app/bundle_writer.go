package app

import (
	"context"

	"github.com/shichao402/Dec/internal/secrets"
)

// BundleWriter 是 ADR 0014 规定的权威写入门面。
// TUI / MCP / servicehost 对 Bundle 状态的变更只应经此类型；内部委托现有 app 实现。
// SyncTarget / 裸 project folder 不是可写主语。
type BundleWriter struct{}

// DefaultBundleWriter 返回默认门面实例。
func DefaultBundleWriter() BundleWriter { return BundleWriter{} }

// SaveEnabledBundles 在当前工作平面启用/禁用 Bundle（先校验 vault manifest/scope，再写 enabled_bundles）。
func (BundleWriter) SaveEnabledBundles(workspace Workspace, bundles []string, reporter Reporter) (*SaveBundleSelectionResult, error) {
	return SaveWorkspaceEnabledBundles(workspace, bundles, reporter)
}

// ValidateRemoteRegisterFolder 校验 Remote 登记 folder（仅 bundle/<名>）。
func (BundleWriter) ValidateRemoteRegisterFolder(workspace Workspace, folder string) error {
	return ValidateRemoteRegisterFolder(workspace, folder)
}

// PrepareRemoteRegister 准备私密半边登记会话。
func (BundleWriter) PrepareRemoteRegister(ctx context.Context, in RemoteRegisterInput, reporter Reporter) (*RemoteRegisterSession, error) {
	return PrepareRemoteRegister(ctx, in, reporter)
}

// CommitRemoteRegister 提交私密半边登记。
func (BundleWriter) CommitRemoteRegister(ctx context.Context, session RemoteRegisterSession, reporter Reporter) (*AddSecretResult, error) {
	return CommitRemoteRegister(ctx, session, reporter)
}

// CommitRemoteNoteEdit 提交 Note 编辑。
func (BundleWriter) CommitRemoteNoteEdit(ctx context.Context, session RemoteNoteEditSession, reporter Reporter) error {
	return CommitRemoteNoteEdit(ctx, session, reporter)
}

// CommitRemoteNoteRegister 提交 Note 登记（旧路径）。
func (BundleWriter) CommitRemoteNoteRegister(ctx context.Context, session RemoteNoteEditSession, reporter Reporter) error {
	return CommitRemoteNoteRegister(ctx, session, reporter)
}

// CommitRemoteSSHHostsEdit 提交 SSH Hosts 编辑。
func (BundleWriter) CommitRemoteSSHHostsEdit(ctx context.Context, session RemoteSSHHostsEditSession, reporter Reporter) error {
	return CommitRemoteSSHHostsEdit(ctx, session, reporter)
}

// RegisterRemoteNoteFromPath 从本地路径登记 Note。
func (BundleWriter) RegisterRemoteNoteFromPath(ctx context.Context, projectRoot, folder, noteRel, localPath string, reporter Reporter) (*AddSecretResult, error) {
	return RegisterRemoteNoteFromPath(ctx, projectRoot, folder, noteRel, localPath, reporter)
}

// AddSecretToTarget 把本地文件登记为 Bundle 私密半边 Note（target 必须为已声明 bundle）。
func (BundleWriter) AddSecretToTarget(ctx context.Context, projectRoot string, target secrets.SyncTarget, noteRel string, reporter Reporter) (*AddSecretResult, error) {
	return AddSecretToTarget(ctx, projectRoot, target, noteRel, reporter)
}

// RemoveBundle 移除公开半边本地落地（不删 vault / BW，除非实现另有约定）。
func (BundleWriter) RemoveBundle(input RemoveBundleInput, reporter Reporter) (*RemoveBundleResult, error) {
	return RemoveBundle(input, reporter)
}

// DeleteItems 删除公开/私密选中项（Remote 页）。
func (BundleWriter) DeleteItems(ctx context.Context, input DeleteProjectInput, reporter Reporter) (*DeleteProjectResult, error) {
	return DeleteProjectItems(ctx, input, reporter)
}

// MigrateUnmanagedNote 将非托管裸 folder Note 迁入 Bundle（标准迁移动作）。
func (BundleWriter) MigrateUnmanagedNote(ctx context.Context, input MigrateUnmanagedNoteInput, reporter Reporter) (*MigrateUnmanagedNoteResult, error) {
	return MigrateUnmanagedNoteToBundle(ctx, input, reporter)
}

// MigrateProjectSecrets 将存量裸 project folder / .secrets/project 迁入 scope:project 的 Bundle。
func (BundleWriter) MigrateProjectSecrets(ctx context.Context, input MigrateProjectSecretsInput, reporter Reporter) (*MigrateProjectSecretsResult, error) {
	return MigrateProjectSecretsToBundle(ctx, input, reporter)
}

// CleanupUnmanaged 删除非托管裸 folder 内容（只删 BW，不创建 Bundle）。文案钉死「非模型内写入」。
func (BundleWriter) CleanupUnmanaged(ctx context.Context, input DeleteProjectInput, reporter Reporter) (*DeleteProjectResult, error) {
	input.Mode = "remote"
	return DeleteRemoteOnly(ctx, input, reporter)
}
