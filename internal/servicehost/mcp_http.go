package servicehost

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shichao402/Dec/internal/agenttools"
	"github.com/shichao402/Dec/internal/config"
)

func newMCPHTTPServer(host *Server) *http.Server {
	mcpServer := newAgentMCPServer(host)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{Stateless: true})
	mux := http.NewServeMux()
	mux.Handle(config.MCPHTTPPath, host.mcpLocalOnly(handler))
	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func newAgentMCPServer(host *Server) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "dec",
		Version: host.version,
	}, nil)
	manifest, err := agenttools.BuildManifest(host.version)
	if err != nil {
		return server
	}
	for _, listing := range manifest.Tools {
		tool := listing
		var schema any
		_ = json.Unmarshal(tool.InputSchema, &schema)
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		server.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return host.callMCPTool(ctx, tool.Name, req.Params.Arguments)
		})
	}
	return server
}

// mcpLocalOnly 只放行 loopback 对端。监听已经绑在 127.0.0.1，这里再看一次 RemoteAddr，
// 避免以后监听地址被放宽时本机门面变成任意来源可调用。不读 Authorization：
// 把 listen token 写进 IDE 的 mcp.json 挡不住能读到该文件的本机进程，却让配置每次启动都变。
func (s *Server) mcpLocalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !remoteAddrIsLoopback(r.RemoteAddr) {
			http.Error(w, "dec MCP 只接受本机连接", http.StatusForbidden)
			return
		}
		s.presence.connected()
		defer s.presence.disconnected()
		next.ServeHTTP(w, r)
	})
}

func remoteAddrIsLoopback(remoteAddr string) bool {
	host := remoteAddr
	if parsed, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = parsed
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
