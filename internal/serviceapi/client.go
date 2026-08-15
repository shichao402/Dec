package serviceapi

import (
	"context"
	"encoding/json"
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

type API struct {
	client        *service.Client
	clientID      string
	facade        string
	unlockTimeout time.Duration
	mu            sync.Mutex
}

func Connect(ctx context.Context, facade, clientID string) (*API, error) {
	client, err := service.Connect(ctx, facade, clientID)
	if err != nil {
		return nil, err
	}
	api := &API{client: client, clientID: clientID, facade: facade}
	if facade == "mcp" {
		api.unlockTimeout = app.MCPSessionUnlockTimeout
	}
	return api, nil
}

func (a *API) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client.Close()
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
	_ = a.client.Close()
	client, err := service.Connect(ctx, a.facade, a.clientID)
	if err != nil {
		return err
	}
	a.client = client
	return nil
}

func (a *API) rpc() servicev1.DecServiceClient {
	a.mu.Lock()
	defer a.mu.Unlock()
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
	request := &servicev1.GetActiveOperationRequest{ProjectRoot: projectRoot}
	resp, err := a.rpc().GetActiveOperation(ctx, request)
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
	request := &servicev1.WatchOperationRequest{
		ProjectRoot: projectRoot,
		OperationId: operationID,
	}
	stream, err := a.rpc().WatchOperation(ctx, request)
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
