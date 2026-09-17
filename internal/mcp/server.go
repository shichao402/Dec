// Package mcp 提供 Dec 的 MCP（stdio）接口层。
//
// 架构约束（ADR 0025 / 0030）：dec-mcp 是零业务知识的 stdio 壳。
// 工具清单来自 ~/.dec/run/agent-tools.json（Console 对齐运行时后由 dec-server --dump-agent-tools 写出）；
// tools/call 一律 POST /agent/tool_call，由 Console 按服务端计划执行。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shichao402/Dec/internal/agenttools"
)

// Config 配置 Dec MCP Server。
type Config struct {
	ClientVersion string
	Gateway       Gateway
	// ManifestPath 覆盖默认 ~/.dec/run/agent-tools.json（测试用）。
	ManifestPath string
}

// Server 持有 MCP 服务状态。
type Server struct {
	cfg      Config
	gw       Gateway
	mu       sync.Mutex
	manifest *agenttools.Manifest
	mcp      *mcp.Server
}

// New 创建 Dec MCP Server。
func New(cfg Config) *Server {
	return &Server{cfg: cfg, gw: cfg.Gateway}
}

func (s *Server) gateway() Gateway {
	if s.gw != nil {
		return s.gw
	}
	return s.cfg.Gateway
}

// Register 按清单动态注册工具。清单缺失时只注册 bootstrap 工具。
func (s *Server) Register(mcpServer *mcp.Server) {
	s.mcp = mcpServer
	manifest := s.loadManifest()
	s.applyManifest(manifest)
}

func (s *Server) loadManifest() *agenttools.Manifest {
	path := s.cfg.ManifestPath
	if path == "" {
		if p, err := agentToolsPath(); err == nil {
			path = p
		}
	}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if m, err := agenttools.ParseManifest(data); err == nil {
				return m
			}
		}
	}
	return agenttools.BootstrapManifest(s.cfg.ClientVersion)
}

func (s *Server) applyManifest(m *agenttools.Manifest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mcp == nil || m == nil {
		return
	}
	oldNames := make([]string, 0)
	if s.manifest != nil {
		for _, t := range s.manifest.Tools {
			oldNames = append(oldNames, t.Name)
		}
	}
	if len(oldNames) > 0 {
		s.mcp.RemoveTools(oldNames...)
	}
	for _, listing := range m.Tools {
		tool := listing
		var schema any
		_ = json.Unmarshal(tool.InputSchema, &schema)
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		s.mcp.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		}, s.makeHandler(tool.Name))
	}
	s.manifest = m
}

func (s *Server) makeHandler(name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.callTool(ctx, name, req.Params.Arguments)
	}
}

func (s *Server) callTool(ctx context.Context, name string, arguments json.RawMessage) (*mcp.CallToolResult, error) {
	gw := s.gateway()
	if gw == nil {
		_, resp, _ := toolFail(fmt.Errorf("尚未连接 Dec Console"), nil)
		raw, _ := json.Marshal(resp)
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: string(raw)}},
			StructuredContent: resp,
			IsError:           true,
		}, nil
	}
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	res, err := gw.ToolCall(ctx, name, arguments, s.cfg.ClientVersion)
	if err != nil {
		_, resp, _ := toolFail(err, nil)
		return structuredResult(resp, true), nil
	}
	if res != nil && res.ManifestVersion != "" {
		s.maybeReloadManifest(res.ManifestVersion)
	}
	if res == nil {
		_, resp, _ := toolFail(fmt.Errorf("Console 网关无响应"), nil)
		return structuredResult(resp, true), nil
	}
	if !res.OK && res.Error != "" {
		_, resp, _ := toolFail(fmt.Errorf("%s", res.Error), logsFromEvents(res.Events))
		return structuredResult(resp, true), nil
	}
	_, resp, _ := toolOK(res.data(), logsFromEvents(res.Events))
	return structuredResult(resp, false), nil
}

func structuredResult(resp toolResponse, isErr bool) *mcp.CallToolResult {
	raw, _ := json.Marshal(resp)
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(raw)}},
		StructuredContent: resp,
		IsError:           isErr,
	}
}

func logsFromEvents(events []map[string]any) []logEntry {
	out := make([]logEntry, 0, len(events))
	for _, event := range events {
		entry := logEntry{}
		if v, _ := event["level"].(string); v != "" {
			entry.Level = v
		}
		if v, _ := event["scope"].(string); v != "" {
			entry.Scope = v
		}
		if v, _ := event["message"].(string); v != "" {
			entry.Message = v
		}
		out = append(out, entry)
	}
	return out
}

func (s *Server) maybeReloadManifest(version string) {
	s.mu.Lock()
	current := ""
	if s.manifest != nil {
		current = s.manifest.Version
	}
	s.mu.Unlock()
	if version == "" || version == current {
		return
	}
	s.applyManifest(s.loadManifest())
}

// Run 启动 stdio MCP Server。
func Run(ctx context.Context, cfg Config) error {
	ctx, stopWatchers := withExitWatchers(ctx)
	defer stopWatchers()

	if cfg.Gateway == nil {
		clientID := fmt.Sprintf("mcp-%d", os.Getpid())
		gw, err := WaitForGateway(ctx, clientID)
		if err != nil {
			return err
		}
		cfg.Gateway = gw
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "dec",
		Version: "1.0.0",
	}, nil)
	s := New(cfg)
	s.Register(mcpServer)
	stdin := newIdleReader(os.Stdin)
	ctx, stopIdle := watchStdinIdle(ctx, stdin, stdinIdleTimeout)
	defer stopIdle()
	return mcpServer.Run(ctx, &mcp.IOTransport{
		Reader: stdin,
		Writer: nopWriteCloser{os.Stdout},
	})
}
