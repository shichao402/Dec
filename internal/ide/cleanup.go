package ide

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DecMCPEntries 返回配置中由 Dec 管理的 MCP 条目名。
func DecMCPEntries(impl IDE, plane Plane, projectRoot, homeDir string) ([]string, error) {
	config, err := impl.LoadMCPConfigForPlane(plane, projectRoot, homeDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for name := range config.MCPServers {
		if (plane == PlaneUser && name == "dec") || strings.HasPrefix(name, "dec-") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// RemoveDecMCPEntries 只移除 Dec 管理的 MCP 条目，保留共享配置中的其它内容。
func RemoveDecMCPEntries(impl IDE, plane Plane, projectRoot, homeDir string) ([]string, error) {
	names, err := DecMCPEntries(impl, plane, projectRoot, homeDir)
	if err != nil || len(names) == 0 {
		return names, err
	}
	configPath := impl.MCPConfigPathForPlane(plane, projectRoot, homeDir)
	if _, ok := impl.(*codexIDE); ok {
		data, readErr := os.ReadFile(configPath)
		if readErr != nil {
			return nil, readErr
		}
		out := stripDecMCPSections(data, plane == PlaneUser)
		if len(strings.TrimSpace(string(out))) == 0 {
			if removeErr := os.Remove(configPath); removeErr != nil && !os.IsNotExist(removeErr) {
				return nil, removeErr
			}
			return names, nil
		}
		return names, os.WriteFile(configPath, out, 0o644)
	}

	var root map[string]json.RawMessage
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		return nil, readErr
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	var servers map[string]json.RawMessage
	if raw := root["mcpServers"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, err
		}
	}
	for _, name := range names {
		delete(servers, name)
	}
	if len(servers) == 0 {
		delete(root, "mcpServers")
	} else {
		raw, marshalErr := json.Marshal(servers)
		if marshalErr != nil {
			return nil, marshalErr
		}
		root["mcpServers"] = raw
	}
	if len(root) == 0 {
		if removeErr := os.Remove(configPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, removeErr
		}
		_ = removeEmptyParents(filepath.Dir(configPath), impl.PlaneRoot(plane, projectRoot, homeDir))
		return names, nil
	}
	out, marshalErr := json.MarshalIndent(root, "", "  ")
	if marshalErr != nil {
		return nil, marshalErr
	}
	return names, os.WriteFile(configPath, append(out, '\n'), 0o644)
}

func stripDecMCPSections(data []byte, includeBuiltin bool) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	var out strings.Builder
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if path, ok := parseTOMLSectionHeader(trimmed); ok {
			skip = len(path) >= 2 && path[0] == "mcp_servers" &&
				((includeBuiltin && path[1] == "dec") || strings.HasPrefix(path[1], "dec-"))
		}
		if !skip {
			out.WriteString(line)
		}
	}
	return []byte(strings.TrimLeft(out.String(), "\r\n"))
}

func removeEmptyParents(path, stop string) error {
	for filepath.Clean(path) != filepath.Clean(stop) {
		if err := os.Remove(path); err != nil {
			return nil
		}
		path = filepath.Dir(path)
	}
	return nil
}
