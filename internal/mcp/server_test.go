package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/app"
)

type fakeGateway struct {
	hello       *Hello
	helloErr    error
	invokeFn    func(method, root, plane string) *RPCResult
	runFn       func(op, root, plane string) *RPCResult
	connections any
	methods     []string
}

func (f *fakeGateway) Hello(context.Context) (*Hello, error) {
	if f.helloErr != nil {
		return nil, f.helloErr
	}
	if f.hello != nil {
		return f.hello, nil
	}
	return &Hello{Version: "test", Connected: true, Unlocked: true}, nil
}

func (f *fakeGateway) Invoke(_ context.Context, method, projectRoot, plane string, _ any) (*RPCResult, error) {
	f.methods = append(f.methods, method+":"+plane+":"+projectRoot)
	if f.invokeFn != nil {
		return f.invokeFn(method, projectRoot, plane), nil
	}
	return &RPCResult{OK: true, Result: json.RawMessage(`{"ok":true}`)}, nil
}

func (f *fakeGateway) Run(_ context.Context, operation, projectRoot, plane string, _ any) (*RPCResult, error) {
	f.methods = append(f.methods, operation+":"+plane+":"+projectRoot)
	if f.runFn != nil {
		return f.runFn(operation, projectRoot, plane), nil
	}
	return &RPCResult{OK: true, Result: json.RawMessage(`{"ok":true}`)}, nil
}

func (f *fakeGateway) Connections(context.Context) (any, error) {
	if f.connections != nil {
		return f.connections, nil
	}
	return map[string]any{"connections": []any{}}, nil
}

func (f *fakeGateway) Connect(context.Context, map[string]any) (*Hello, error) {
	return f.Hello(context.Background())
}

func (f *fakeGateway) ActiveOperation(context.Context, string) (any, error) {
	return map[string]any{"active": false}, nil
}

func TestHandleStatus_NoProjectRootLocalFails(t *testing.T) {
	s := New(Config{Gateway: &fakeGateway{}})
	_, out, err := s.handleStatus(context.Background(), nil, statusParams{})
	if err != nil {
		t.Fatalf("handleStatus() err = %v", err)
	}
	resp := out.(toolResponse)
	if resp.OK {
		t.Fatal("缺 project_root 的 local 状态应失败")
	}
	if !strings.Contains(resp.Error, "dec_list_managed_projects") {
		t.Fatalf("错误应提示受管项目: %s", resp.Error)
	}
}

func TestHandleStatus_UsesGateway(t *testing.T) {
	gw := &fakeGateway{
		invokeFn: func(method, root, plane string) *RPCResult {
			if method != "load_project_overview" {
				t.Fatalf("method = %s", method)
			}
			if root != "/work" || plane != "local" {
				t.Fatalf("ws = %s %s", plane, root)
			}
			return &RPCResult{OK: true, Result: json.RawMessage(`{"ProjectRoot":"/work"}`)}
		},
	}
	s := New(Config{Gateway: gw})
	_, out, err := s.handleStatus(context.Background(), nil, statusParams{ProjectRoot: "/work"})
	if err != nil {
		t.Fatalf("handleStatus() err = %v", err)
	}
	resp := out.(toolResponse)
	if !resp.OK {
		t.Fatalf("expected ok, got %#v", resp)
	}
	data := resp.Data.(map[string]any)
	if data["plane"] != "local" {
		t.Fatalf("plane = %#v", data["plane"])
	}
}

func TestHandleConnectRepoAndInit(t *testing.T) {
	gw := &fakeGateway{}
	s := New(Config{Gateway: gw})
	_, connectOut, err := s.handleConnectRepo(context.Background(), nil, connectRepoParams{RepoURL: "git://x"})
	connectResp := connectOut.(toolResponse)
	if err != nil || !connectResp.OK {
		t.Fatalf("handleConnectRepo() = %#v, %v", connectResp, err)
	}
	_, initOut, err := s.handleInitProject(context.Background(), nil, initProjectParams{ProjectRoot: t.TempDir()})
	initResp := initOut.(toolResponse)
	if err != nil || !initResp.OK {
		t.Fatalf("handleInitProject() = %#v, %v", initResp, err)
	}
}

func TestHandleListAssets_BothPlanes(t *testing.T) {
	s := New(Config{Gateway: &fakeGateway{}})
	_, out, err := s.handleListAssets(context.Background(), nil, listAssetsParams{Plane: "both", ProjectRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("handleListAssets() err = %v", err)
	}
	resp := out.(toolResponse)
	if !resp.OK {
		t.Fatalf("expected ok, got %#v", out)
	}
	data := resp.Data.(map[string]any)
	outcomes := data["planes"].([]planeOutcome)
	if len(outcomes) != 2 {
		t.Fatalf("planes len = %d", len(outcomes))
	}
	if outcomes[0].Plane != string(app.WorkspaceLocal) || outcomes[1].Plane != string(app.WorkspaceGlobal) {
		t.Fatalf("planes order = %q,%q", outcomes[0].Plane, outcomes[1].Plane)
	}
}

func TestHandleDelete_RejectsBoth(t *testing.T) {
	s := New(Config{Gateway: &fakeGateway{}})
	_, out, err := s.handleDelete(context.Background(), nil, deleteParams{Confirmed: true, Plane: "both", ProjectRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("handleDelete() err = %v", err)
	}
	resp, ok := out.(toolResponse)
	if !ok || resp.OK {
		t.Fatalf("plane=both 应失败: %#v", out)
	}
}

func TestHandleConsoleStatus(t *testing.T) {
	s := New(Config{Gateway: &fakeGateway{hello: &Hello{Connected: true, Unlocked: false, Version: "1.0"}}})
	_, out, err := s.handleConsoleStatus(context.Background(), nil, emptyParams{})
	if err != nil {
		t.Fatal(err)
	}
	resp := out.(toolResponse)
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
}
