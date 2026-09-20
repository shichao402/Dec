package ide

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/types"
)

// withIDE 适配腾讯内部 Agent 编辑器 With。
//
// 用户平面根目录是 ~/.bg-agent/config-with-app（skills / rules / mcp_config.json）。
// 项目平面使用约定目录 .with/，布局与用户平面对称。
type withIDE struct {
	baseIDE
}

func newWithIDE() IDE {
	return &withIDE{baseIDE: baseIDE{
		name:          "with",
		dirKey:        ".with",
		userDirKey:    filepath.Join(".bg-agent", "config-with-app"),
		mcpConfigPath: filepath.Join(".with", "mcp_config.json"),
		userMCPPath:   filepath.Join(".bg-agent", "config-with-app", "mcp_config.json"),
	}}
}

func (w *withIDE) WriteMCPConfig(projectRoot string, config *types.MCPConfig) error {
	return w.WriteMCPConfigForPlane(PlaneProject, projectRoot, "", config)
}

func (w *withIDE) WriteMCPConfigForPlane(plane Plane, projectRoot, homeDir string, config *types.MCPConfig) error {
	configPath := w.MCPConfigPathForPlane(plane, projectRoot, homeDir)

	root, servers, err := loadWithMCPRaw(configPath)
	if err != nil {
		return err
	}

	desired := map[string]types.MCPServer{}
	if config != nil {
		for name, server := range config.MCPServers {
			if isManagedMCPName(name, plane) {
				desired[name] = server
			}
		}
	}

	for name := range servers {
		if isManagedMCPName(name, plane) {
			delete(servers, name)
		}
	}
	for name, server := range desired {
		raw, encodeErr := encodeWithMCPServer(server)
		if encodeErr != nil {
			return encodeErr
		}
		servers[name] = raw
	}

	if len(servers) == 0 {
		delete(root, "mcpServers")
	} else {
		raw, marshalErr := json.Marshal(servers)
		if marshalErr != nil {
			return marshalErr
		}
		root["mcpServers"] = raw
	}

	if len(root) == 0 {
		if removeErr := os.Remove(configPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return removeErr
		}
		_ = removeEmptyParents(filepath.Dir(configPath), w.PlaneRoot(plane, projectRoot, homeDir))
		return nil
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(configPath, append(out, '\n'), 0644)
}

func (w *withIDE) LoadMCPConfig(projectRoot string) (*types.MCPConfig, error) {
	return w.LoadMCPConfigForPlane(PlaneProject, projectRoot, "")
}

func (w *withIDE) LoadMCPConfigForPlane(plane Plane, projectRoot, homeDir string) (*types.MCPConfig, error) {
	configPath := w.MCPConfigPathForPlane(plane, projectRoot, homeDir)
	_, servers, err := loadWithMCPRaw(configPath)
	if err != nil {
		return nil, err
	}

	config := &types.MCPConfig{MCPServers: make(map[string]types.MCPServer, len(servers))}
	for name, raw := range servers {
		server, decodeErr := decodeWithMCPServer(raw)
		if decodeErr != nil {
			return nil, fmt.Errorf("解析 With MCP %s 失败: %w", name, decodeErr)
		}
		config.MCPServers[name] = server
	}
	return config, nil
}

func isManagedMCPName(name string, plane Plane) bool {
	if strings.HasPrefix(name, "dec-") {
		return true
	}
	return plane == PlaneUser && name == "dec"
}

func loadWithMCPRaw(configPath string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	root := make(map[string]json.RawMessage)
	servers := make(map[string]json.RawMessage)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return root, servers, nil
		}
		return nil, nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return root, servers, nil
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, nil, fmt.Errorf("解析 With MCP 配置失败 (%s): %w", configPath, err)
	}
	if raw, ok := root["mcpServers"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, nil, fmt.Errorf("解析 With mcpServers 失败 (%s): %w", configPath, err)
		}
	}
	if servers == nil {
		servers = make(map[string]json.RawMessage)
	}
	return root, servers, nil
}

func decodeWithMCPServer(raw json.RawMessage) (types.MCPServer, error) {
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return types.MCPServer{}, err
	}

	var server types.MCPServer
	if command, ok := generic["command"].(string); ok {
		server.Command = command
	}
	if cwd, ok := generic["cwd"].(string); ok {
		server.Cwd = cwd
	}
	if url, ok := generic["url"].(string); ok {
		server.URL = url
	}
	if args, ok := asStringSlice(generic["args"]); ok {
		server.Args = args
	}
	if env, ok := asStringMap(generic["env"]); ok {
		server.Env = env
	}
	if headers, ok := asStringMap(generic["headers"]); ok {
		server.HTTPHeaders = headers
	} else if headers, ok := asStringMap(generic["http_headers"]); ok {
		server.HTTPHeaders = headers
	}
	if disabled, ok := generic["disabled"].(bool); ok {
		enabled := !disabled
		server.Enabled = &enabled
	} else if enabled, ok := generic["enabled"].(bool); ok {
		server.Enabled = &enabled
	}
	if timeout, ok := asInt(generic["timeout"]); ok {
		server.ToolTimeoutSec = &timeout
	}

	known := map[string]struct{}{
		"command": {}, "args": {}, "env": {}, "cwd": {}, "url": {},
		"headers": {}, "http_headers": {}, "disabled": {}, "enabled": {}, "timeout": {},
	}
	for key, value := range generic {
		if _, ok := known[key]; ok {
			continue
		}
		if server.Extra == nil {
			server.Extra = make(map[string]any)
		}
		server.Extra[key] = value
	}
	return server, nil
}

func encodeWithMCPServer(server types.MCPServer) (json.RawMessage, error) {
	out := make(map[string]any, 8)
	for key, value := range server.Extra {
		out[key] = value
	}

	if strings.TrimSpace(server.Command) != "" {
		out["command"] = server.Command
	} else {
		delete(out, "command")
	}
	if len(server.Args) > 0 {
		out["args"] = server.Args
	} else {
		delete(out, "args")
	}
	if len(server.Env) > 0 {
		out["env"] = server.Env
	} else {
		delete(out, "env")
	}
	if strings.TrimSpace(server.Cwd) != "" {
		out["cwd"] = server.Cwd
	} else {
		delete(out, "cwd")
	}
	if strings.TrimSpace(server.URL) != "" {
		out["url"] = server.URL
		if _, hasTransport := out["transportType"]; !hasTransport {
			out["transportType"] = "streamable-http"
		}
	} else {
		delete(out, "url")
	}
	if len(server.HTTPHeaders) > 0 {
		out["headers"] = server.HTTPHeaders
	} else {
		delete(out, "headers")
	}
	delete(out, "http_headers")
	delete(out, "enabled")

	if server.Enabled != nil {
		out["disabled"] = !*server.Enabled
	} else if _, ok := out["disabled"]; !ok {
		out["disabled"] = false
	}
	if server.ToolTimeoutSec != nil {
		out["timeout"] = *server.ToolTimeoutSec
	}

	return json.Marshal(out)
}

func asStringSlice(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	default:
		return nil, false
	}
}

func asStringMap(value any) (map[string]string, bool) {
	switch typed := value.(type) {
	case map[string]string:
		return typed, true
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			out[key] = text
		}
		return out, true
	default:
		return nil, false
	}
}

func asInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case json.Number:
		n, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}
