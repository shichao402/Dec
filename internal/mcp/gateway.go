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
		gw, err := connectGateway(clientID)
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
	endpoint := strings.TrimSpace(meta.Endpoint)
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
