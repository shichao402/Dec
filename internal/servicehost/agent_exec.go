package servicehost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shichao402/Dec/internal/agenttools"
	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/service"
	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const mcpUnlockTimeout = 180 * time.Second

func (s *Server) callMCPTool(ctx context.Context, name string, arguments json.RawMessage) (*mcp.CallToolResult, error) {
	ok, result, errText, events := s.executeAgentTool(ctx, name, arguments)
	body := map[string]any{
		"ok":     ok,
		"result": result,
		"error":  errText,
		"events": events,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		raw = []byte(`{"ok":false,"error":"结果无法编码"}`)
		ok = false
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
		IsError: !ok,
	}, nil
}

func (s *Server) executeAgentTool(ctx context.Context, name string, arguments json.RawMessage) (bool, any, string, []map[string]any) {
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	plan := agenttools.Plan(name, arguments)
	if plan == nil {
		return false, nil, "计划为空", nil
	}
	if plan.Error != "" {
		return false, nil, plan.Error, nil
	}
	if plan.Owner == agenttools.OwnerConsole {
		value, err := s.executeConsoleTool(name)
		if err != nil {
			return false, nil, err.Error(), nil
		}
		return true, value, "", nil
	}
	ctx = s.mcpCallContext(ctx)
	return s.executePlan(ctx, plan)
}

func (s *Server) mcpCallContext(ctx context.Context) context.Context {
	md := metadata.Pairs(
		service.FacadeHeader, "mcp",
		service.ClientIDHeader, "mcp-http",
		service.InteractiveAuthHeader, "1",
		service.TokenHeader, s.listenToken,
		service.ClientVersionHeader, s.version,
	)
	return metadata.NewIncomingContext(ctx, md)
}

func (s *Server) executeConsoleTool(name string) (any, error) {
	switch name {
	case agenttools.BootstrapTool:
		return map[string]any{
			"version":        s.version,
			"connected":      true,
			"unlocked":       secrets.InstanceUnlocked(),
			"server_version": s.version,
			"connection": map[string]any{
				"kind": "local",
				"host": "127.0.0.1",
				"port": 47653,
			},
		}, nil
	case "dec_list_connections", "dec_connect":
		return nil, fmt.Errorf("本机 MCP 只操作本机 dec-server，不跟随 Console 的 SSH 连接。请在 Console 的连接页切换远端")
	default:
		return nil, fmt.Errorf("未知 Console 工具 %s", name)
	}
}

func (s *Server) executePlan(ctx context.Context, plan *agenttools.PlanResult) (bool, any, string, []map[string]any) {
	var events []map[string]any
	switch plan.Shape {
	case agenttools.ShapePlanes:
		outcomes := make([]any, 0, len(plan.Steps))
		anyOK := false
		for _, step := range plan.Steps {
			plane := step.Plane
			if strings.TrimSpace(plane) == "" {
				plane = "local"
			}
			if step.Kind == agenttools.StepError {
				outcomes = append(outcomes, map[string]any{"plane": plane, "ok": false, "error": step.Method})
				continue
			}
			value, err := s.runStep(ctx, step, &events)
			if err != nil {
				outcomes = append(outcomes, map[string]any{"plane": plane, "ok": false, "error": err.Error()})
				continue
			}
			anyOK = true
			outcomes = append(outcomes, map[string]any{"plane": plane, "ok": true, "result": value})
		}
		body := map[string]any{"planes": outcomes}
		if !anyOK {
			return false, body, "两平面均失败，详见 planes[].error", events
		}
		return true, body, "", events
	case agenttools.ShapeKeyed:
		out := map[string]any{}
		for key, value := range plan.Envelope {
			out[key] = value
		}
		for _, step := range plan.Steps {
			value, err := s.runStep(ctx, step, &events)
			if err != nil {
				if step.Optional {
					continue
				}
				return false, nil, err.Error(), events
			}
			key := step.Key
			if key == "" {
				key = "result"
			}
			out[key] = value
		}
		return true, out, "", events
	default:
		if len(plan.Steps) == 0 {
			return true, nil, "", events
		}
		value, err := s.runStep(ctx, plan.Steps[0], &events)
		if err != nil {
			return false, nil, err.Error(), events
		}
		return true, value, "", events
	}
}

func (s *Server) runStep(ctx context.Context, step agenttools.Step, events *[]map[string]any) (any, error) {
	switch step.Kind {
	case agenttools.StepError:
		return nil, fmt.Errorf("%s", step.Method)
	case agenttools.StepConsoleActiveOperation:
		active := s.broker.active(step.ProjectRoot)
		if active == nil || !active.GetActive() {
			return map[string]any{"active": false}, nil
		}
		return map[string]any{
			"active":       true,
			"operation_id": active.GetOperationId(),
			"operation":    active.GetOperation(),
		}, nil
	case agenttools.StepInvoke:
		plane := step.Plane
		if strings.TrimSpace(plane) == "" {
			plane = "local"
		}
		resp, err := s.Invoke(ctx, &servicev1.InvokeRequest{
			Method:          step.Method,
			ProjectRoot:     step.ProjectRoot,
			WorkspacePlane:  plane,
			PayloadJson:     payloadBytes(step.Payload),
			UnlockTimeoutMs: mcpUnlockTimeout.Milliseconds(),
		})
		if err != nil {
			return nil, fmt.Errorf("%s", statusMessage(err))
		}
		appendProtoEvents(events, resp.GetEvents())
		if resp == nil {
			return nil, nil
		}
		return decodeResultJSON(resp.GetResultJson()), nil
	case agenttools.StepRun:
		plane := step.Plane
		if strings.TrimSpace(plane) == "" {
			plane = "local"
		}
		resultJSON, protoEvents, err := s.runOperationSync(ctx, step.ProjectRoot, step.Method, plane, payloadBytes(step.Payload))
		appendProtoEvents(events, protoEvents)
		if err != nil {
			return nil, err
		}
		return decodeResultJSON(resultJSON), nil
	default:
		return nil, fmt.Errorf("未知计划步骤 kind %s", step.Kind)
	}
}

func (s *Server) runOperationSync(ctx context.Context, projectRoot, operation, plane string, payload []byte) (resultJSON []byte, protoEvents []*servicev1.OperationEvent, err error) {
	facade, clientID := callerFromMetadata(ctx)
	unlockOpts := secrets.EnsureSessionOpts{
		UnlockTimeout:    mcpUnlockTimeout,
		InteractiveLocal: s.allowsInteractiveUnlock(ctx, facade),
		Facade:           facade,
		ClientID:         clientID,
		Operation:        operation,
		ProjectRoot:      projectRoot,
		WorkspacePlane:   plane,
	}
	if !secrets.InstanceUnlocked() && !operationAllowedWhenLocked(operation) {
		if err := secrets.EnsureSession(ctx, &unlockOpts); err != nil {
			return nil, nil, err
		}
	}
	state, err := s.broker.start(projectRoot, operation, clientID, facade)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("operation panic: %v", r)
			s.broker.finish(projectRoot, &servicev1.WatchOperationResponse{Error: err.Error(), Done: true})
		}
	}()
	reporter := app.ReporterFunc(func(event app.OperationEvent) {
		encoded := encodeEvent(event)
		protoEvents = append(protoEvents, encoded)
		s.broker.publish(projectRoot, &servicev1.WatchOperationResponse{Event: encoded})
	})
	s.ensureProjectRepaired(projectRoot, reporter)
	unlockOpts.OperationID = state.meta.OperationId
	ctx = secrets.WithEnsureSessionOpts(ctx, unlockOpts)
	ctx = app.WithUnlockConfig(ctx, app.UnlockConfig{
		Timeout:          unlockOpts.UnlockTimeout,
		InteractiveLocal: unlockOpts.InteractiveLocal,
		Facade:           facade,
		ClientID:         clientID,
		Operation:        operation,
		OperationID:      state.meta.OperationId,
		ProjectRoot:      projectRoot,
		WorkspacePlane:   plane,
	})
	if operation == "provision_remote_host" {
		payload = pinProvisionVersion(payload, s.version)
	}
	workspace := app.NewWorkspace(app.WorkspacePlane(plane), projectRoot)
	result, runErr := dispatchOperationWorkspace(ctx, operation, workspace, payload, reporter, app.DefaultPWriter())
	if runErr == nil {
		resultJSON, runErr = json.Marshal(result)
	}
	watchFinal := &servicev1.WatchOperationResponse{ResultJson: resultJSON, Done: true}
	if runErr != nil {
		watchFinal.Error = runErr.Error()
	}
	s.broker.finish(projectRoot, watchFinal)
	return resultJSON, protoEvents, runErr
}

func payloadBytes(raw json.RawMessage) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return []byte(`{}`)
	}
	return raw
}

func decodeResultJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	return value
}

func appendProtoEvents(dst *[]map[string]any, events []*servicev1.OperationEvent) {
	for _, event := range events {
		if event == nil {
			continue
		}
		*dst = append(*dst, map[string]any{
			"level":   event.GetLevel(),
			"scope":   event.GetScope(),
			"message": event.GetMessage(),
		})
	}
}

func statusMessage(err error) string {
	if st, ok := status.FromError(err); ok && st.Code() != codes.OK {
		return st.Message()
	}
	return err.Error()
}
