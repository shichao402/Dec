package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shichao402/Dec/pkg/app"
)

// logEntry 是写入 MCP tool 响应的精简日志行。
type logEntry struct {
	Level   string `json:"level"`
	Scope   string `json:"scope"`
	Message string `json:"message"`
}

type toolResponse struct {
	OK    bool       `json:"ok"`
	Error string     `json:"error,omitempty"`
	Data  any        `json:"data,omitempty"`
	Logs  []logEntry `json:"logs,omitempty"`
}

func newCollector() (app.Reporter, func() []logEntry) {
	c := &collectorReporter{}
	return c, func() []logEntry { return c.entries }
}

type collectorReporter struct {
	entries []logEntry
}

func (c *collectorReporter) Emit(event app.OperationEvent) {
	c.entries = append(c.entries, logEntry{
		Level:   string(event.Level),
		Scope:   event.Scope,
		Message: event.Message,
	})
}

func toolOK(data any, logs []logEntry) (*mcp.CallToolResult, toolResponse, error) {
	return nil, toolResponse{OK: true, Data: data, Logs: logs}, nil
}

func toolFail(err error, logs []logEntry) (*mcp.CallToolResult, toolResponse, error) {
	return nil, toolResponse{OK: false, Error: err.Error(), Logs: logs}, nil
}

func (s *Server) toolContext(ctx context.Context) context.Context {
	return app.WithUnlockConfig(ctx, app.UnlockConfig{Timeout: app.MCPSessionUnlockTimeout})
}
