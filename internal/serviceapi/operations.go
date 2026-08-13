package serviceapi

import (
	"context"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/secrets"
)

func invoke[T any](ctx context.Context, method, projectRoot string, input any, reporter app.Reporter) (*T, error) {
	api, err := Default()
	if err != nil {
		return nil, err
	}
	var out T
	if err := api.Invoke(ctx, method, projectRoot, input, &out, reporter); err != nil {
		return nil, err
	}
	return &out, nil
}

func run[T any](ctx context.Context, operation, projectRoot string, input any, reporter app.Reporter) (*T, error) {
	api, err := Default()
	if err != nil {
		return nil, err
	}
	var out T
	if err := api.Run(ctx, operation, projectRoot, input, &out, reporter); err != nil {
		return nil, err
	}
	return &out, nil
}

func LoadProjectOverview(projectRoot string, includeVaultBundles bool) (*app.ProjectOverview, error) {
	return invoke[app.ProjectOverview](context.Background(), "load_project_overview", projectRoot,
		struct{ IncludeVaultBundles bool }{includeVaultBundles}, nil)
}

func LoadAssetSelection(projectRoot string, reporter app.Reporter) (*app.AssetSelectionState, error) {
	return invoke[app.AssetSelectionState](context.Background(), "load_asset_selection", projectRoot, nil, reporter)
}

func SaveEnabledBundles(projectRoot string, bundles []string, reporter app.Reporter) (*app.SaveBundleSelectionResult, error) {
	return invoke[app.SaveBundleSelectionResult](context.Background(), "save_enabled_bundles", projectRoot,
		struct{ EnabledBundles []string }{bundles}, reporter)
}

func ConnectRepo(repoURL string, reporter app.Reporter) (*app.ConnectRepoResult, error) {
	return invoke[app.ConnectRepoResult](context.Background(), "connect_repo", "",
		struct{ RepoURL string }{repoURL}, reporter)
}

func PrepareProjectConfigInit(projectRoot string, reporter app.Reporter) (*app.ConfigInitPreparation, error) {
	return invoke[app.ConfigInitPreparation](context.Background(), "prepare_project_config_init", projectRoot, nil, reporter)
}

func EnsureLocalProjectConfig(projectRoot string, reporter app.Reporter) (*app.ConfigInitPreparation, error) {
	return invoke[app.ConfigInitPreparation](context.Background(), "ensure_local_project_config", projectRoot, nil, reporter)
}

func InferVaultProject(projectRoot string, reporter app.Reporter) (*app.VaultProjectInference, error) {
	return invoke[app.VaultProjectInference](context.Background(), "infer_vault_project", projectRoot, nil, reporter)
}

func ApplyVaultProject(projectRoot string, reporter app.Reporter) (*app.VaultProjectAutoApplyResult, error) {
	return invoke[app.VaultProjectAutoApplyResult](context.Background(), "apply_vault_project", projectRoot, nil, reporter)
}

func LoadGlobalSettings(reporter app.Reporter) (*app.GlobalSettingsState, error) {
	return invoke[app.GlobalSettingsState](context.Background(), "load_global_settings", "", nil, reporter)
}

func SaveGlobalSettings(input app.SaveGlobalSettingsInput, reporter app.Reporter) (*app.SaveGlobalSettingsResult, error) {
	return invoke[app.SaveGlobalSettingsResult](context.Background(), "save_global_settings", "", input, reporter)
}

func EnsureBuiltinIDEAssets(ideNames []string, reporter app.Reporter) ([]string, error) {
	return invokeSlice[string](context.Background(), "ensure_builtin_ide_assets", "",
		struct{ IDENames []string }{ideNames}, reporter)
}

func invokeSlice[T any](ctx context.Context, method, root string, input any, reporter app.Reporter) ([]T, error) {
	api, err := Default()
	if err != nil {
		return nil, err
	}
	var out []T
	if err := api.Invoke(ctx, method, root, input, &out, reporter); err != nil {
		return nil, err
	}
	return out, nil
}

func LoadProjectSettings(projectRoot string, reporter app.Reporter) (*app.ProjectSettingsState, error) {
	return invoke[app.ProjectSettingsState](context.Background(), "load_project_settings", projectRoot, nil, reporter)
}

func SaveProjectSettings(input app.SaveProjectSettingsInput, reporter app.Reporter) (*app.SaveProjectSettingsResult, error) {
	return invoke[app.SaveProjectSettingsResult](context.Background(), "save_project_settings", input.ProjectRoot, input, reporter)
}

func LoadProjectVarsView(projectRoot string) (*app.ProjectVarsView, error) {
	return invoke[app.ProjectVarsView](context.Background(), "load_project_vars", projectRoot, nil, nil)
}

func EnsureProjectVarsFile(projectRoot string) (*app.EnsureProjectVarsFileResult, error) {
	return invoke[app.EnsureProjectVarsFileResult](context.Background(), "ensure_project_vars", projectRoot, nil, nil)
}

func ListSecretSyncTargets(projectRoot string) ([]app.SecretTargetOption, error) {
	return invokeSlice[app.SecretTargetOption](context.Background(), "list_secret_sync_targets", projectRoot, nil, nil)
}

func SuggestSecretTargets(projectRoot string) ([]app.SecretTargetOption, error) {
	return invokeSlice[app.SecretTargetOption](context.Background(), "suggest_secret_targets", projectRoot, nil, nil)
}

func ListSecretsMetadata(ctx context.Context, projectRoot string, includeRemote bool, reporter app.Reporter) (*app.ListSecretsMetadataResult, error) {
	return invoke[app.ListSecretsMetadataResult](ctx, "list_secrets", projectRoot,
		struct{ IncludeRemote bool }{includeRemote}, reporter)
}

func ListDeleteCandidates(ctx context.Context, projectRoot string, includeRemote bool, reporter app.Reporter) ([]app.DeleteCandidate, error) {
	return invokeSlice[app.DeleteCandidate](ctx, "list_delete_candidates", projectRoot,
		struct{ IncludeRemote bool }{includeRemote}, reporter)
}

func PullProjectAssets(ctx context.Context, projectRoot string, reporter app.Reporter) (*app.PullProjectAssetsResult, error) {
	return run[app.PullProjectAssetsResult](ctx, "pull", projectRoot, nil, reporter)
}

func PushProjectAssets(ctx context.Context, projectRoot string, reporter app.Reporter) (*app.PushProjectAssetsResult, error) {
	return run[app.PushProjectAssetsResult](ctx, "push", projectRoot, nil, reporter)
}

func PreviewPushProjectAssets(ctx context.Context, projectRoot string, reporter app.Reporter) (*app.PushProjectAssetsPreview, error) {
	return run[app.PushProjectAssetsPreview](ctx, "preview_push", projectRoot, nil, reporter)
}

func RemoveBundle(ctx context.Context, input app.RemoveBundleInput, reporter app.Reporter) (*app.RemoveBundleResult, error) {
	return run[app.RemoveBundleResult](ctx, "remove_bundle", input.ProjectRoot, input, reporter)
}

func DeleteProjectItems(ctx context.Context, input app.DeleteProjectInput, reporter app.Reporter) (*app.DeleteProjectResult, error) {
	return run[app.DeleteProjectResult](ctx, "delete", input.ProjectRoot, input, reporter)
}

func AddSecretToTarget(ctx context.Context, projectRoot string, target secrets.SyncTarget, noteRel string, reporter app.Reporter) (*app.AddSecretResult, error) {
	return run[app.AddSecretResult](ctx, "add_secret", projectRoot, struct {
		Target  secrets.SyncTarget
		NoteRel string
	}{target, noteRel}, reporter)
}

func PrepareRemoteNoteEdit(ctx context.Context, projectRoot string, item app.DeleteSelectionItem, reporter app.Reporter) (*app.RemoteNoteEditSession, error) {
	return invoke[app.RemoteNoteEditSession](ctx, "prepare_remote_note_edit", projectRoot, item, reporter)
}

func PrepareRemoteSSHHostsEdit(ctx context.Context, projectRoot string, item app.DeleteSelectionItem, reporter app.Reporter) (*app.RemoteSSHHostsEditSession, error) {
	return invoke[app.RemoteSSHHostsEditSession](ctx, "prepare_remote_ssh_hosts_edit", projectRoot, item, reporter)
}

func CommitRemoteNoteEdit(ctx context.Context, session app.RemoteNoteEditSession, reporter app.Reporter) error {
	_, err := run[struct{}](ctx, "commit_remote_note_edit", session.ProjectRoot, session, reporter)
	return err
}

func CommitRemoteSSHHostsEdit(ctx context.Context, session app.RemoteSSHHostsEditSession, reporter app.Reporter) error {
	_, err := run[struct{}](ctx, "commit_remote_ssh_hosts_edit", session.ProjectRoot, session, reporter)
	return err
}
