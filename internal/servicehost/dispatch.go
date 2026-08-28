package servicehost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/service"
	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type eventCollector struct {
	events []*servicev1.OperationEvent
}

func (c *eventCollector) Emit(event app.OperationEvent) {
	c.events = append(c.events, encodeEvent(event))
}

func (s *Server) Invoke(ctx context.Context, req *servicev1.InvokeRequest) (*servicev1.InvokeResponse, error) {
	facade, clientID := callerFromMetadata(ctx)
	ctx = app.WithUnlockConfig(ctx, app.UnlockConfig{
		Timeout:        time.Duration(req.UnlockTimeoutMs) * time.Millisecond,
		Facade:         facade,
		ClientID:       clientID,
		Operation:      req.Method,
		ProjectRoot:    req.ProjectRoot,
		WorkspacePlane: req.WorkspacePlane,
	})
	if isProjectMutation(req.Method) && s.broker.active(req.ProjectRoot).Active {
		return nil, status.Error(codes.AlreadyExists, "project busy: 当前有写操作进行中")
	}
	if isMachineMutation(req.Method) {
		s.machineMu.Lock()
		defer s.machineMu.Unlock()
	}
	collector := &eventCollector{}
	s.ensureProjectRepaired(req.ProjectRoot, collector)
	workspace := app.NewWorkspace(app.WorkspacePlane(req.WorkspacePlane), req.ProjectRoot)
	writer := app.DefaultPWriter()
	result, err := dispatchInvokeWorkspace(ctx, req.Method, workspace, req.PayloadJson, collector, writer)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if req.Method == "save_global_settings" {
		s.presence.setTimeout(loadIdleTimeout())
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &servicev1.InvokeResponse{ResultJson: data, Events: collector.events}, nil
}

func callerFromMetadata(ctx context.Context) (facade, clientID string) {
	md, _ := metadata.FromIncomingContext(ctx)
	if values := md.Get(service.FacadeHeader); len(values) > 0 {
		facade = values[0]
	}
	if values := md.Get(service.ClientIDHeader); len(values) > 0 {
		clientID = values[0]
	}
	return facade, clientID
}

func isProjectMutation(method string) bool {
	switch method {
	case "save_enabled_bundles", "prepare_project_config_init", "ensure_local_project_config",
		"apply_vault_project", "save_project_settings", "ensure_project_vars",
		"prepare_remote_note_edit", "prepare_remote_ssh_hosts_edit":
		return true
	default:
		return false
	}
}

func isMachineMutation(method string) bool {
	switch method {
	case "connect_repo", "save_global_settings", "ensure_builtin_ide_assets", "ensure_global_vars":
		return true
	default:
		return false
	}
}

func dispatchInvoke(ctx context.Context, method, projectRoot string, payload []byte, reporter app.Reporter) (any, error) {
	return dispatchInvokeWorkspace(ctx, method, app.NewWorkspace(app.WorkspaceProject, projectRoot), payload, reporter, app.DefaultPWriter())
}

func dispatchInvokeWorkspace(ctx context.Context, method string, workspace app.Workspace, payload []byte, reporter app.Reporter, writer app.PWriter) (any, error) {
	projectRoot := workspace.Root
	switch method {
	case "load_project_overview":
		var in struct{ IncludeVaultBundles bool }
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.LoadWorkspaceOverviewOpts(workspace, app.OverviewLoadOpts{IncludeVaultBundles: in.IncludeVaultBundles})
	case "load_asset_selection":
		return app.LoadWorkspaceAssetSelection(workspace, reporter)
	case "save_enabled_bundles":
		var in struct {
			EnabledProjects []string
			EnabledBundles  []string
		}
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		names := in.EnabledProjects
		if names == nil {
			names = in.EnabledBundles
		}
		return writer.SaveProjects(workspace, names, reporter)
	case "connect_repo":
		var in struct{ RepoURL string }
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.ConnectRepo(in.RepoURL, reporter)
	case "prepare_project_config_init":
		return app.PrepareProjectConfigInit(projectRoot, reporter)
	case "ensure_local_project_config":
		return app.EnsureLocalProjectConfig(projectRoot, reporter)
	case "ensure_home_p":
		return writer.BindHomeP(projectRoot, reporter)
	case "infer_vault_project":
		return app.InferVaultProject(projectRoot, reporter)
	case "apply_vault_project":
		return app.ApplyVaultProject(projectRoot, reporter)
	case "load_global_settings":
		return app.LoadGlobalSettings(reporter)
	case "save_global_settings":
		var in app.SaveGlobalSettingsInput
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.SaveGlobalSettings(in, reporter)
	case "load_global_vars":
		return app.LoadGlobalVarsView()
	case "ensure_global_vars":
		return app.EnsureGlobalVarsFile()
	case "ensure_builtin_ide_assets":
		var in struct{ IDENames []string }
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.EnsureBuiltinIDEAssets(in.IDENames, reporter), nil
	case "load_project_settings":
		return app.LoadProjectSettings(projectRoot, reporter)
	case "save_project_settings":
		var in app.SaveProjectSettingsInput
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		in.ProjectRoot = projectRoot
		return app.SaveProjectSettings(in, reporter)
	case "load_project_vars":
		return app.LoadProjectVarsView(projectRoot)
	case "ensure_project_vars":
		return app.EnsureProjectVarsFile(projectRoot)
	case "list_secret_sync_targets":
		return app.ListSecretSyncTargets(projectRoot)
	case "suggest_secret_targets":
		return app.SuggestSecretTargets(projectRoot)
	case "list_secrets":
		var in struct{ IncludeRemote bool }
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.ListWorkspaceSecretsMetadata(ctx, workspace, in.IncludeRemote, reporter)
	case "list_delete_candidates":
		var in struct{ IncludeRemote bool }
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.ListWorkspaceDeleteCandidates(ctx, workspace, in.IncludeRemote, reporter)
	case "prepare_remote_note_edit":
		var in app.DeleteSelectionItem
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.PrepareRemoteNoteEdit(ctx, projectRoot, in, reporter)
	case "prepare_remote_note_register":
		var in struct {
			Folder      string
			NoteRel     string
			InitialBody string
		}
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.PrepareRemoteNoteRegister(ctx, projectRoot, in.Folder, in.NoteRel, in.InitialBody, reporter)
	case "validate_remote_register_folder":
		var in struct{ Folder string }
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		if err := writer.ValidateRemoteRegisterFolder(workspace, in.Folder); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	case "prepare_remote_register":
		var in app.RemoteRegisterInput
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		in.ProjectRoot = projectRoot
		in.Plane = workspace.EffectivePlane()
		return writer.PrepareRemoteRegister(ctx, in, reporter)
	case "prepare_remote_ssh_hosts_edit":
		var in app.DeleteSelectionItem
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.PrepareRemoteSSHHostsEdit(ctx, projectRoot, in, reporter)
	case "session_status":
		return map[string]bool{
			"ready": secrets.HasSession() && secrets.HasUserKey(),
		}, nil
	default:
		return nil, fmt.Errorf("未知服务方法 %q", method)
	}
}

func (s *Server) RunOperation(req *servicev1.RunOperationRequest, stream grpc.ServerStreamingServer[servicev1.RunOperationResponse]) (retErr error) {
	state, err := s.broker.start(req.ProjectRoot, req.Operation, req.ClientId, req.Facade)
	if err != nil {
		return status.Error(codes.AlreadyExists, err.Error())
	}
	// 兜底：无论业务逻辑是否 panic，都要 finish，否则该 project 会永久卡在 busy。
	// broker.finish 对已结束的 state 是幂等的，正常路径显式 finish 后此处不会重复标记。
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("operation panic: %v", r)
			s.broker.finish(req.ProjectRoot, &servicev1.WatchOperationResponse{Error: msg, Done: true})
			retErr = status.Error(codes.Internal, msg)
		}
	}()
	if err := stream.Send(&servicev1.RunOperationResponse{
		OperationId: state.meta.OperationId,
		Active:      cloneActive(state.meta),
	}); err != nil {
		s.broker.finish(req.ProjectRoot, &servicev1.WatchOperationResponse{Error: err.Error(), Done: true})
		return err
	}

	var sendMu sync.Mutex
	send := func(message *servicev1.RunOperationResponse) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(message)
	}

	reporter := app.ReporterFunc(func(event app.OperationEvent) {
		encoded := encodeEvent(event)
		_ = send(&servicev1.RunOperationResponse{
			OperationId: state.meta.OperationId,
			Active:      cloneActive(state.meta),
			Event:       encoded,
		})
		s.broker.publish(req.ProjectRoot, &servicev1.WatchOperationResponse{Event: encoded})
	})
	s.ensureProjectRepaired(req.ProjectRoot, reporter)

	operationCtx := stream.Context()
	operationCtx = app.WithUnlockConfig(operationCtx, app.UnlockConfig{
		Timeout:        time.Duration(req.UnlockTimeoutMs) * time.Millisecond,
		Facade:         req.Facade,
		ClientID:       req.ClientId,
		Operation:      req.Operation,
		OperationID:    state.meta.OperationId,
		ProjectRoot:    req.ProjectRoot,
		WorkspacePlane: req.WorkspacePlane,
	})
	workspace := app.NewWorkspace(app.WorkspacePlane(req.WorkspacePlane), req.ProjectRoot)
	writer := app.DefaultPWriter()
	result, runErr := dispatchOperationWorkspace(operationCtx, req.Operation, workspace, req.PayloadJson, reporter, writer)
	var resultJSON []byte
	if runErr == nil {
		resultJSON, runErr = json.Marshal(result)
	}
	final := &servicev1.RunOperationResponse{
		OperationId: state.meta.OperationId,
		Active:      cloneActive(state.meta),
		ResultJson:  resultJSON,
		Done:        true,
	}
	watchFinal := &servicev1.WatchOperationResponse{
		ResultJson: resultJSON,
		Done:       true,
	}
	if runErr != nil {
		final.Error = runErr.Error()
		watchFinal.Error = runErr.Error()
	}
	s.broker.finish(req.ProjectRoot, watchFinal)
	if err := send(final); err != nil {
		return err
	}
	return nil
}

func dispatchOperation(ctx context.Context, operation, projectRoot string, payload []byte, reporter app.Reporter) (any, error) {
	return dispatchOperationWorkspace(ctx, operation, app.NewWorkspace(app.WorkspaceProject, projectRoot), payload, reporter, app.DefaultPWriter())
}

func dispatchOperationWorkspace(ctx context.Context, operation string, workspace app.Workspace, payload []byte, reporter app.Reporter, writer app.PWriter) (any, error) {
	projectRoot := workspace.Root
	switch operation {
	case "pull":
		return app.PullWorkspaceAssets(ctx, workspace, "", reporter)
	case "push":
		return writer.PushWorkspace(ctx, workspace, reporter)
	case "preview_push":
		return app.PreviewPushWorkspaceAssets(workspace)
	case "prepare_repo_gcm_bootstrap":
		var in struct {
			RepoURL string
		}
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.PrepareRepoGCMBootstrap(ctx, in.RepoURL, reporter)
	case "apply_repo_gcm_bootstrap":
		var in app.ApplyRepoGCMBootstrapInput
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return app.ApplyRepoGCMBootstrap(ctx, in, reporter)
	case "remove_bundle":
		var in app.RemoveBundleInput
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		in.ProjectRoot = projectRoot
		in.Plane = workspace.EffectivePlane()
		return writer.RemoveBundle(in, reporter)
	case "delete":
		var in app.DeleteProjectInput
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		in.ProjectRoot = projectRoot
		in.Plane = workspace.EffectivePlane()
		return writer.DeleteItems(ctx, in, reporter)
	case "add_secret":
		var in struct {
			Target  secrets.SyncTarget
			NoteRel string
		}
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return writer.AddSecretToTarget(ctx, projectRoot, in.Target, in.NoteRel, reporter)
	case "register_remote_note_from_path":
		var in struct {
			Folder    string
			NoteRel   string
			LocalPath string
		}
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return writer.RegisterRemoteNoteFromPath(ctx, projectRoot, in.Folder, in.NoteRel, in.LocalPath, reporter)
	case "commit_remote_register":
		var in app.RemoteRegisterSession
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		in.ProjectRoot = projectRoot
		in.Plane = workspace.EffectivePlane()
		return writer.CommitRemoteRegister(ctx, in, reporter)
	case "commit_remote_note_edit":
		var in app.RemoteNoteEditSession
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return struct{}{}, writer.CommitRemoteNoteEdit(ctx, in, reporter)
	case "commit_remote_note_register":
		var in app.RemoteNoteEditSession
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return struct{}{}, writer.CommitRemoteNoteRegister(ctx, in, reporter)
	case "commit_remote_ssh_hosts_edit":
		var in app.RemoteSSHHostsEditSession
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return struct{}{}, writer.CommitRemoteSSHHostsEdit(ctx, in, reporter)
	case "migrate_unmanaged_note":
		var in app.MigrateUnmanagedNoteInput
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		return writer.MigrateUnmanagedNote(ctx, in, reporter)
	case "migrate_project_secrets":
		var in app.MigrateProjectSecretsInput
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.ProjectRoot) == "" {
			in.ProjectRoot = projectRoot
		}
		return writer.MigrateProjectSecrets(ctx, in, reporter)
	default:
		return nil, fmt.Errorf("未知写操作 %q", operation)
	}
}

func decode(data []byte, out any) error {
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析调用参数失败: %w", err)
	}
	return nil
}

func encodeEvent(event app.OperationEvent) *servicev1.OperationEvent {
	out := &servicev1.OperationEvent{
		TimeUnixMs: event.Time.UnixMilli(),
		Level:      string(event.Level),
		Scope:      event.Scope,
		Message:    event.Message,
	}
	if event.Time.IsZero() {
		out.TimeUnixMs = time.Now().UnixMilli()
	}
	if event.Progress != nil {
		out.Progress = &servicev1.Progress{
			Phase:   event.Progress.Phase,
			Current: int32(event.Progress.Current),
			Total:   int32(event.Progress.Total),
		}
	}
	return out
}
