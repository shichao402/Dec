package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/consoleopen"
	"github.com/shichao402/Dec/internal/service"
)

const consoleMetadataVersion = 1

// Gateway 是 Console loopback Agent 网关。
type Gateway interface {
	Hello(ctx context.Context) (*Hello, error)
	Invoke(ctx context.Context, method, projectRoot, plane string, payload any) (*RPCResult, error)
	Run(ctx context.Context, operation, projectRoot, plane string, payload any) (*RPCResult, error)
	Connections(ctx context.Context) (any, error)
	Connect(ctx context.Context, req map[string]any) (*Hello, error)
	ActiveOperation(ctx context.Context, projectRoot string) (any, error)
	// ToolCall 是统一入口：{name, arguments} → Console 编排执行（ADR 0030）。
	ToolCall(ctx context.Context, name string, arguments json.RawMessage, clientVersion string) (*ToolCallResult, error)
}

type Hello struct {
	Version       string         `json:"version"`
	Connected     bool           `json:"connected"`
	Unlocked      bool           `json:"unlocked"`
	ServerVersion string         `json:"server_version"`
	Connection    map[string]any `json:"connection"`
	Error         string         `json:"error"`
	Code          string         `json:"code"`
}

type RPCResult struct {
	OK     bool             `json:"ok"`
	Result json.RawMessage  `json:"result"`
	Error  string           `json:"error"`
	Events []map[string]any `json:"events"`
}

// ToolCallResult 是 /agent/tool_call 的响应。
type ToolCallResult struct {
	OK              bool             `json:"ok"`
	Result          json.RawMessage  `json:"result"`
	Error           string           `json:"error"`
	Events          []map[string]any `json:"events"`
	ManifestVersion string           `json:"manifest_version,omitempty"`
}

type consoleMetadata struct {
	Version  int    `json:"version"`
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	PID      int    `json:"pid"`
}

type httpGateway struct {
	base     string
	token    string
	clientID string
	http     *http.Client
}

func ConsoleMetadataPath() (string, error) {
	dir, err := service.RuntimeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "console.json"), nil
}

func readConsoleMetadata() (*consoleMetadata, error) {
	path, err := ConsoleMetadataPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta consoleMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("解析 console.json 失败: %w", err)
	}
	if meta.Version != consoleMetadataVersion || meta.Endpoint == "" || meta.Token == "" {
		return nil, fmt.Errorf("console.json 无效")
	}
	return &meta, nil
}

func WaitForGateway(ctx context.Context, clientID string) (Gateway, error) {
	deadline := time.Now().Add(app.MCPSessionUnlockTimeout)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	var last error
	opened := false
	for time.Now().Before(deadline) {
		gw, err := newRefreshingGateway(clientID)
		if err == nil {
			if hello, helloErr := gw.Hello(ctx); helloErr == nil {
				if !hello.Connected {
					_, _ = gw.Connect(ctx, map[string]any{"kind": "local"})
				}
				return gw, nil
			} else {
				last = helloErr
			}
		} else {
			last = err
		}
		if !opened {
			if openErr := consoleopen.Open(); openErr != nil {
				return nil, fmt.Errorf("无法连接 Dec Console: %v（拉起失败: %w）", last, openErr)
			}
			opened = true
		}
		select {
		case <-ctx.Done():
			if last == nil {
				last = ctx.Err()
			}
			return nil, last
		case <-time.After(200 * time.Millisecond):
		}
	}
	if last == nil {
		last = fmt.Errorf("等待 Dec Console 网关超时")
	}
	return nil, last
}

func connectGateway(clientID string) (*httpGateway, error) {
	meta, err := readConsoleMetadata()
	if err != nil {
		return nil, err
	}
	return gatewayFromMeta(meta, clientID)
}

func gatewayFromMeta(meta *consoleMetadata, clientID string) (*httpGateway, error) {
	if meta == nil {
		return nil, fmt.Errorf("console.json 无效")
	}
	endpoint := strings.TrimSpace(meta.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("console.json 无效")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	return &httpGateway{
		base:     strings.TrimRight(endpoint, "/"),
		token:    meta.Token,
		clientID: clientID,
		http:     &http.Client{Timeout: app.MCPSessionUnlockTimeout + 30*time.Second},
	}, nil
}

// refreshingGateway 每次调用前对照 ~/.dec/run/console.json。
// Console 重启会换 loopback 端口与 token；dec-mcp 是长驻 stdio 进程，
// 若启动时钉死网关，就会一直打到旧端口（connection refused）。
type refreshingGateway struct {
	clientID string
	mu       sync.Mutex
	meta     consoleMetadata
	inner    *httpGateway
}

func newRefreshingGateway(clientID string) (*refreshingGateway, error) {
	g := &refreshingGateway{clientID: clientID}
	if err := g.ensure(true); err != nil {
		return nil, err
	}
	return g, nil
}

func sameConsoleMeta(a, b consoleMetadata) bool {
	return a.Endpoint == b.Endpoint && a.Token == b.Token && a.PID == b.PID && a.Version == b.Version
}

func (g *refreshingGateway) ensure(force bool) error {
	meta, err := readConsoleMetadata()
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !force && g.inner != nil && sameConsoleMeta(g.meta, *meta) {
		return nil
	}
	inner, err := gatewayFromMeta(meta, g.clientID)
	if err != nil {
		return err
	}
	g.meta = *meta
	g.inner = inner
	return nil
}

func (g *refreshingGateway) current() *httpGateway {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inner
}

func shouldRefreshGateway(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return true
	case strings.Contains(msg, "actively refused"):
		return true
	case strings.Contains(msg, "No connection could be made"):
		return true
	case strings.Contains(msg, "connectex"):
		return true
	case strings.Contains(msg, "HTTP 401"):
		return true
	case strings.Contains(msg, "未授权"):
		return true
	case strings.Contains(msg, "Unauthorized"):
		return true
	default:
		return false
	}
}

func (g *refreshingGateway) withInner(ctx context.Context, call func(*httpGateway) error) error {
	if err := g.ensure(false); err != nil {
		return err
	}
	err := call(g.current())
	if err == nil || !shouldRefreshGateway(err) {
		return err
	}
	// Console 可能已重启并改写了 console.json；强制重读后重试一次。
	if refreshErr := g.ensure(true); refreshErr != nil {
		return err
	}
	return call(g.current())
}

func (g *refreshingGateway) Hello(ctx context.Context) (*Hello, error) {
	var out *Hello
	err := g.withInner(ctx, func(inner *httpGateway) error {
		hello, callErr := inner.Hello(ctx)
		out = hello
		return callErr
	})
	return out, err
}

func (g *refreshingGateway) Invoke(ctx context.Context, method, projectRoot, plane string, payload any) (*RPCResult, error) {
	var out *RPCResult
	err := g.withInner(ctx, func(inner *httpGateway) error {
		res, callErr := inner.Invoke(ctx, method, projectRoot, plane, payload)
		out = res
		return callErr
	})
	return out, err
}

func (g *refreshingGateway) Run(ctx context.Context, operation, projectRoot, plane string, payload any) (*RPCResult, error) {
	var out *RPCResult
	err := g.withInner(ctx, func(inner *httpGateway) error {
		res, callErr := inner.Run(ctx, operation, projectRoot, plane, payload)
		out = res
		return callErr
	})
	return out, err
}

func (g *refreshingGateway) Connections(ctx context.Context) (any, error) {
	var out any
	err := g.withInner(ctx, func(inner *httpGateway) error {
		res, callErr := inner.Connections(ctx)
		out = res
		return callErr
	})
	return out, err
}

func (g *refreshingGateway) Connect(ctx context.Context, req map[string]any) (*Hello, error) {
	var out *Hello
	err := g.withInner(ctx, func(inner *httpGateway) error {
		hello, callErr := inner.Connect(ctx, req)
		out = hello
		return callErr
	})
	return out, err
}

func (g *refreshingGateway) ActiveOperation(ctx context.Context, projectRoot string) (any, error) {
	var out any
	err := g.withInner(ctx, func(inner *httpGateway) error {
		res, callErr := inner.ActiveOperation(ctx, projectRoot)
		out = res
		return callErr
	})
	return out, err
}

func (g *refreshingGateway) ToolCall(ctx context.Context, name string, arguments json.RawMessage, clientVersion string) (*ToolCallResult, error) {
	var out *ToolCallResult
	err := g.withInner(ctx, func(inner *httpGateway) error {
		res, callErr := inner.ToolCall(ctx, name, arguments, clientVersion)
		out = res
		return callErr
	})
	return out, err
}

func (g *httpGateway) Hello(ctx context.Context) (*Hello, error) {
	var out Hello
	if err := g.do(ctx, http.MethodGet, "/agent/hello", nil, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return &out, fmt.Errorf("%s", out.Error)
	}
	return &out, nil
}

func (g *httpGateway) Invoke(ctx context.Context, method, projectRoot, plane string, payload any) (*RPCResult, error) {
	return g.rpc(ctx, "/agent/invoke", map[string]any{
		"method":          method,
		"project_root":    projectRoot,
		"workspace_plane": plane,
		"payload":         payloadOrEmpty(payload),
	})
}

func (g *httpGateway) Run(ctx context.Context, operation, projectRoot, plane string, payload any) (*RPCResult, error) {
	return g.rpc(ctx, "/agent/run", map[string]any{
		"operation":       operation,
		"project_root":    projectRoot,
		"workspace_plane": plane,
		"payload":         payloadOrEmpty(payload),
	})
}

func (g *httpGateway) Connections(ctx context.Context) (any, error) {
	var out map[string]any
	if err := g.do(ctx, http.MethodGet, "/agent/connections", nil, &out); err != nil {
		return nil, err
	}
	if errMsg, _ := out["error"].(string); errMsg != "" {
		return nil, fmt.Errorf("%s", errMsg)
	}
	return out, nil
}

func (g *httpGateway) Connect(ctx context.Context, req map[string]any) (*Hello, error) {
	var out Hello
	if err := g.do(ctx, http.MethodPost, "/agent/connect", req, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return &out, fmt.Errorf("%s", out.Error)
	}
	return &out, nil
}

func (g *httpGateway) ActiveOperation(ctx context.Context, projectRoot string) (any, error) {
	path := "/agent/active_operation"
	if strings.TrimSpace(projectRoot) != "" {
		path += "?project_root=" + url.QueryEscape(projectRoot)
	}
	var out any
	if err := g.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (g *httpGateway) ToolCall(ctx context.Context, name string, arguments json.RawMessage, clientVersion string) (*ToolCallResult, error) {
	var out ToolCallResult
	body := struct {
		Name          string          `json:"name"`
		Arguments     json.RawMessage `json:"arguments"`
		ClientVersion string          `json:"client_version"`
	}{Name: name, Arguments: arguments, ClientVersion: clientVersion}
	if err := g.do(ctx, http.MethodPost, "/agent/tool_call", body, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		out.OK = false
	}
	return &out, nil
}

func (r *ToolCallResult) data() any {
	if len(r.Result) == 0 || string(r.Result) == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal(r.Result, &v); err != nil {
		return string(r.Result)
	}
	return v
}

func agentToolsPath() (string, error) {
	dir, err := service.RuntimeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agent-tools.json"), nil
}

func (g *httpGateway) rpc(ctx context.Context, path string, body any) (*RPCResult, error) {
	var out RPCResult
	if err := g.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		out.OK = false
	}
	return &out, nil
}

func (g *httpGateway) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("X-Dec-Client-Id", g.clientID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		if resp.StatusCode >= 400 {
			return fmt.Errorf("Console 网关 HTTP %d", resp.StatusCode)
		}
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析 Console 网关响应失败: %w", err)
	}
	if resp.StatusCode >= 400 {
		var wrap struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.Unmarshal(data, &wrap)
		if wrap.Error != "" {
			if wrap.Code != "" {
				return fmt.Errorf("%s: %s", wrap.Code, wrap.Error)
			}
			return fmt.Errorf("%s", wrap.Error)
		}
		return fmt.Errorf("Console 网关 HTTP %d", resp.StatusCode)
	}
	return nil
}

func payloadOrEmpty(payload any) any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}

func (r *RPCResult) logs() []logEntry {
	out := make([]logEntry, 0, len(r.Events))
	for _, event := range r.Events {
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

func (r *RPCResult) data() any {
	if len(r.Result) == 0 || string(r.Result) == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal(r.Result, &v); err != nil {
		return string(r.Result)
	}
	return v
}
