package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shichao402/Dec/internal/agenttools"
)

type fakeGateway struct {
	hello          *Hello
	helloErr       error
	toolCallFn     func(name string, args json.RawMessage) *ToolCallResult
	connections    any
	calls          []string
	manifestVersion string
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

func (f *fakeGateway) Invoke(context.Context, string, string, string, any) (*RPCResult, error) {
	return &RPCResult{OK: true, Result: json.RawMessage(`{}`)}, nil
}

func (f *fakeGateway) Run(context.Context, string, string, string, any) (*RPCResult, error) {
	return &RPCResult{OK: true, Result: json.RawMessage(`{}`)}, nil
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

func (f *fakeGateway) ToolCall(_ context.Context, name string, args json.RawMessage, _ string) (*ToolCallResult, error) {
	f.calls = append(f.calls, name)
	if f.toolCallFn != nil {
		return f.toolCallFn(name, args), nil
	}
	return &ToolCallResult{
		OK:              true,
		Result:          json.RawMessage(`{"ok":true}`),
		ManifestVersion: f.manifestVersion,
	}, nil
}

func TestRegisterFromManifest(t *testing.T) {
	dir := t.TempDir()
	raw, err := agenttools.DumpJSON("v9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-tools.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("注册 tools panic: %v", r)
		}
	}()
	s := New(Config{Gateway: &fakeGateway{}, ManifestPath: path, ClientVersion: "v9.9.9"})
	s.Register(mcp.NewServer(&mcp.Implementation{Name: "dec", Version: "test"}, nil))
	if s.manifest == nil || len(s.manifest.Tools) != len(agenttools.ToolNames()) {
		t.Fatalf("manifest tools = %d", len(s.manifest.Tools))
	}
}

func TestRegisterBootstrapWhenMissing(t *testing.T) {
	s := New(Config{Gateway: &fakeGateway{}, ManifestPath: filepath.Join(t.TempDir(), "missing.json")})
	s.Register(mcp.NewServer(&mcp.Implementation{Name: "dec", Version: "test"}, nil))
	if len(s.manifest.Tools) != 1 || s.manifest.Tools[0].Name != agenttools.BootstrapTool {
		t.Fatalf("%+v", s.manifest)
	}
}

func TestCallToolForwardsArguments(t *testing.T) {
	var gotArgs json.RawMessage
	gw := &fakeGateway{
		toolCallFn: func(name string, args json.RawMessage) *ToolCallResult {
			gotArgs = append(json.RawMessage(nil), args...)
			return &ToolCallResult{OK: true, Result: json.RawMessage(`{"plane":"local"}`)}
		},
	}
	dir := t.TempDir()
	raw, _ := agenttools.DumpJSON("v1")
	path := filepath.Join(dir, "agent-tools.json")
	_ = os.WriteFile(path, raw, 0o600)
	s := New(Config{Gateway: gw, ManifestPath: path})
	s.Register(mcp.NewServer(&mcp.Implementation{Name: "dec", Version: "test"}, nil))
	args := json.RawMessage(`{"project_root":"/work","plane":"local"}`)
	res, err := s.callTool(context.Background(), "dec_status", args)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%+v", res)
	}
	if string(gotArgs) != string(args) {
		t.Fatalf("args rewritten: %s", gotArgs)
	}
}

func TestManifestVersionTriggersReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-tools.json")
	v1, _ := agenttools.DumpJSON("v1")
	_ = os.WriteFile(path, v1, 0o600)
	gw := &fakeGateway{manifestVersion: "v2#997fc99e78d37300"}
	s := New(Config{Gateway: gw, ManifestPath: path, ClientVersion: "v1"})
	s.Register(mcp.NewServer(&mcp.Implementation{Name: "dec", Version: "test"}, nil))
	if !strings.HasPrefix(s.manifest.Version, "v1#") {
		t.Fatalf("start version = %s", s.manifest.Version)
	}
	v2, _ := agenttools.DumpJSON("v2")
	_ = os.WriteFile(path, v2, 0o600)
	_, _ = s.callTool(context.Background(), "dec_console_status", json.RawMessage(`{}`))
	if !strings.HasPrefix(s.manifest.Version, "v2#") {
		t.Fatalf("reloaded version = %s", s.manifest.Version)
	}
}
