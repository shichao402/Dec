package serviceapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/service"
	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ErrActiveOperation = errors.New("dec-server 有进行中的操作，拒绝重启")

type API struct {
	client        *service.Client
	clientID      string
	facade        string
	clientVersion string
	unlockTimeout time.Duration
	mu            sync.Mutex
}

func Connect(ctx context.Context, facade, clientID, clientVersion string) (*API, error) {
	client, err := service.Connect(ctx, facade, clientID, clientVersion)
	if err != nil {
		return nil, err
	}
	api := &API{
		client:        client,
		clientID:      clientID,
		facade:        facade,
		clientVersion: clientVersion,
	}
	if facade == "mcp" {
		api.unlockTimeout = app.MCPSessionUnlockTimeout
	}
	return api, nil
}

func (a *API) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return nil
	}
	return a.client.Close()
}

func (a *API) ServerVersion() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return ""
	}
	return a.client.ServerVersion()
}

func (a *API) ClientVersion() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return a.clientVersion
	}
	return a.client.ClientVersion()
}

func (a *API) VersionMismatch() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return false
	}
	return a.client.VersionMismatch()
}

// RestartOptions 控制 Shutdown → 等待退出 → 重连。
type RestartOptions struct {
	Reason string
	// SkipActiveCheck 为 true 时跳过门面侧 GetActiveOperation 预检（服务端仍会拒绝忙碌 Shutdown）。
	SkipActiveCheck bool
	ProjectRoot     string
}

// ShutdownServer 请求服务退出并等待发现文件/锁消失；不自动重连。
func (a *API) ShutdownServer(ctx context.Context, reason string) error {
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()
	if client == nil {
		return fmt.Errorf("dec-server 客户端尚未连接")
	}
	_, err := client.RPC().Shutdown(ctx, &servicev1.ShutdownRequest{Reason: reason})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.FailedPrecondition {
			return fmt.Errorf("%w: %s", ErrActiveOperation, st.Message())
		}
		return err
	}
	_ = client.Close()
	a.mu.Lock()
	a.client = nil
	a.mu.Unlock()
	return service.WaitUntilStopped(ctx)
}

// RestartServer = Shutdown → 等退出 → Connect 拉起新进程。
func (a *API) RestartServer(ctx context.Context, opts RestartOptions) error {
	if !opts.SkipActiveCheck {
		roots := []string{opts.ProjectRoot}
		if opts.ProjectRoot != "" {
			roots = append(roots, "")
		}
		for _, root := range roots {
			op, err := a.GetActiveOperation(ctx, root)
			if err != nil {
				continue
			}
			if op != nil && op.Active {
				return fmt.Errorf("%w: %s（%s）", ErrActiveOperation, op.Operation, op.Facade)
			}
		}
	}
	reason := opts.Reason
	if reason == "" {
		reason = "restart"
	}
	if err := a.ShutdownServer(ctx, reason); err != nil {
		return err
	}
	return a.reconnect(ctx)
}

// ShutdownIfRunning 在有存活服务时连接并关闭；服务未运行则直接返回。
// 用于自更新流程：替换二进制后停掉旧进程，不拉起新实例（下次由 TUI/MCP 门面自动拉起）。
func ShutdownIfRunning(ctx context.Context, clientVersion, reason string) error {
	if _, err := service.ReadMetadata(); err != nil {
		return nil
	}
	api, err := Connect(ctx, "cli", fmt.Sprintf("cli-%d", time.Now().UnixNano()), clientVersion)
	if err != nil {
		return err
	}
	defer api.Close()
	if reason == "" {
		reason = "update"
	}
	return api.ShutdownServer(ctx, reason)
}

func (a *API) Invoke(ctx context.Context, method, projectRoot string, input, output any, reporter app.Reporter) error {
	return a.InvokeWorkspace(ctx, method, app.NewWorkspace(app.WorkspaceProject, projectRoot), input, output, reporter)
}

func (a *API) InvokeWorkspace(ctx context.Context, method string, workspace app.Workspace, input, output any, reporter app.Reporter) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request := &servicev1.InvokeRequest{
		Method:          method,
		ProjectRoot:     workspace.Root,
		WorkspacePlane:  string(workspace.EffectivePlane()),
		PayloadJson:     payload,
		UnlockTimeoutMs: a.unlockTimeout.Milliseconds(),
	}
	resp, err := a.rpc().Invoke(ctx, request)
	if shouldReconnect(err) {
		if reconnectErr := a.reconnect(ctx); reconnectErr == nil {
			resp, err = a.rpc().Invoke(ctx, request)
		}
	}
	if err != nil {
		return err
	}
	for _, event := range resp.Events {
		emit(reporter, event)
	}
	if output == nil || len(resp.ResultJson) == 0 || string(resp.ResultJson) == "null" {
		return nil
	}
	return json.Unmarshal(resp.ResultJson, output)
}

func (a *API) Run(ctx context.Context, operation, projectRoot string, input, output any, reporter app.Reporter) error {
	return a.RunWorkspace(ctx, operation, app.NewWorkspace(app.WorkspaceProject, projectRoot), input, output, reporter)
}

func (a *API) RunWorkspace(ctx context.Context, operation string, workspace app.Workspace, input, output any, reporter app.Reporter) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request := &servicev1.RunOperationRequest{
		Operation:       operation,
		ProjectRoot:     workspace.Root,
		WorkspacePlane:  string(workspace.EffectivePlane()),
		ClientId:        a.clientID,
		Facade:          a.facade,
		PayloadJson:     payload,
		UnlockTimeoutMs: a.unlockTimeout.Milliseconds(),
	}
	stream, err := a.rpc().RunOperation(ctx, request)
	if shouldReconnect(err) {
		if reconnectErr := a.reconnect(ctx); reconnectErr == nil {
			stream, err = a.rpc().RunOperation(ctx, request)
		}
	}
	if err != nil {
		return err
	}
	for {
		message, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if message.Event != nil {
			emit(reporter, message.Event)
		}
		if !message.Done {
			continue
		}
		if message.Error != "" {
			return fmt.Errorf("%s", message.Error)
		}
		if output != nil && len(message.ResultJson) > 0 && string(message.ResultJson) != "null" {
			if err := json.Unmarshal(message.ResultJson, output); err != nil {
				return err
			}
		}
		return nil
	}
}

func (a *API) reconnect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		_ = a.client.Close()
	}
	client, err := service.Connect(ctx, a.facade, a.clientID, a.clientVersion)
	if err != nil {
		return err
	}
	a.client = client
	return nil
}

func (a *API) rpc() servicev1.DecServiceClient {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return nil
	}
	return a.client.RPC()
}

func shouldReconnect(err error) bool {
	if err == nil {
		return false
	}
	code := status.Code(err)
	return code == codes.Unavailable || code == codes.Unauthenticated
}

func (a *API) GetActiveOperation(ctx context.Context, projectRoot string) (*servicev1.ActiveOperation, error) {
	rpc := a.rpc()
	if rpc == nil {
		return nil, fmt.Errorf("dec-server 客户端尚未连接")
	}
	request := &servicev1.GetActiveOperationRequest{ProjectRoot: projectRoot}
	resp, err := rpc.GetActiveOperation(ctx, request)
	if shouldReconnect(err) {
		if reconnectErr := a.reconnect(ctx); reconnectErr == nil {
			resp, err = a.rpc().GetActiveOperation(ctx, request)
		}
	}
	if err != nil {
		return nil, err
	}
	return resp.Operation, nil
}

func (a *API) WatchOperation(ctx context.Context, projectRoot, operationID string, reporter app.Reporter) error {
	rpc := a.rpc()
	if rpc == nil {
		return fmt.Errorf("dec-server 客户端尚未连接")
	}
	request := &servicev1.WatchOperationRequest{
		ProjectRoot: projectRoot,
		OperationId: operationID,
	}
	stream, err := rpc.WatchOperation(ctx, request)
	if shouldReconnect(err) {
		if reconnectErr := a.reconnect(ctx); reconnectErr == nil {
			stream, err = a.rpc().WatchOperation(ctx, request)
		}
	}
	if err != nil {
		return err
	}
	for {
		message, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if message.Event != nil {
			emit(reporter, message.Event)
		}
		if message.Done {
			if message.Error != "" {
				return fmt.Errorf("%s", message.Error)
			}
			return nil
		}
	}
}

func emit(reporter app.Reporter, event *servicev1.OperationEvent) {
	if reporter == nil || event == nil {
		return
	}
	out := app.OperationEvent{
		Time:    time.UnixMilli(event.TimeUnixMs),
		Level:   app.EventLevel(event.Level),
		Scope:   event.Scope,
		Message: event.Message,
	}
	if event.Progress != nil {
		out.Progress = &app.Progress{
			Phase:   event.Progress.Phase,
			Current: int(event.Progress.Current),
			Total:   int(event.Progress.Total),
		}
	}
	reporter.Emit(out)
}

var (
	defaultMu  sync.RWMutex
	defaultAPI *API
)

func SetDefault(api *API) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultAPI = api
}

func Default() (*API, error) {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	if defaultAPI == nil {
		return nil, fmt.Errorf("dec-server 客户端尚未初始化")
	}
	return defaultAPI, nil
}
