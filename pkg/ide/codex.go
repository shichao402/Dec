package ide

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/shichao402/Dec/pkg/types"
)

type codexIDE struct {
	baseIDE
}

var (
	codexExactEnvRefRe  = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
	codexBearerEnvRefRe = regexp.MustCompile(`^Bearer \$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
)

func newCodexIDE(name string) IDE {
	return &codexIDE{baseIDE: baseIDE{
		name:          name,
		dirKey:        ".codex",
		userDirKey:    "." + name,
		mcpConfigPath: filepath.Join(".codex", "config.toml"),
	}}
}

// MigrateLegacyCodexProject 把旧版项目级 Codex 布局迁移到当前约定。
//
// 迁移内容包括：
// 1. 把旧的 .codex/mcp.json 和 .codex-internal/mcp.json 合并到 .codex/config.toml
// 2. 把项目级 .codex-internal/{skills,rules} 挪到 .codex/{skills,rules}
func MigrateLegacyCodexProject(projectRoot string) ([]string, error) {
	var notes []string

	for _, legacyPath := range []string{
		filepath.Join(projectRoot, ".codex", "mcp.json"),
		filepath.Join(projectRoot, ".codex-internal", "mcp.json"),
	} {
		note, err := migrateLegacyCodexMCPJSON(projectRoot, legacyPath)
		if err != nil {
			return nil, err
		}
		if note != "" {
			notes = append(notes, note)
		}
	}

	for _, pair := range []struct {
		src string
		dst string
	}{
		{src: filepath.Join(projectRoot, ".codex-internal", "skills"), dst: filepath.Join(projectRoot, ".codex", "skills")},
		{src: filepath.Join(projectRoot, ".codex-internal", "rules"), dst: filepath.Join(projectRoot, ".codex", "rules")},
	} {
		moved, err := migrateLegacyProjectDir(pair.src, pair.dst)
		if err != nil {
			return nil, err
		}
		if moved > 0 {
			notes = append(notes, fmt.Sprintf("%s -> %s (%d 项)", relProjectPath(projectRoot, pair.src), relProjectPath(projectRoot, pair.dst), moved))
		}
	}

	_ = removeDirIfEmpty(filepath.Join(projectRoot, ".codex-internal", "skills"))
	_ = removeDirIfEmpty(filepath.Join(projectRoot, ".codex-internal", "rules"))
	_ = removeDirIfEmpty(filepath.Join(projectRoot, ".codex-internal"))

	return notes, nil
}

func (c *codexIDE) WriteMCPConfig(projectRoot string, config *types.MCPConfig) error {
	configPath := c.MCPConfigPath(projectRoot)

	var existing []byte
	data, err := os.ReadFile(configPath)
	if err == nil {
		existing = data
	} else if !os.IsNotExist(err) {
		return err
	}

	merged, err := mergeCodexMCPConfig(existing, config)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(configPath, merged, 0644)
}

func (c *codexIDE) LoadMCPConfig(projectRoot string) (*types.MCPConfig, error) {
	configPath := c.MCPConfigPath(projectRoot)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &types.MCPConfig{MCPServers: make(map[string]types.MCPServer)}, nil
		}
		return nil, err
	}

	config, err := parseCodexMCPConfig(data)
	if err != nil {
		return nil, err
	}
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]types.MCPServer)
	}

	return config, nil
}

func parseCodexMCPConfig(data []byte) (*types.MCPConfig, error) {
	config := &types.MCPConfig{MCPServers: make(map[string]types.MCPServer)}

	var currentServer string
	var currentSubtable string

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if path, ok := parseTOMLSectionHeader(line); ok {
			currentServer, currentSubtable = codexMCPSectionTarget(path)
			continue
		}

		if currentServer == "" {
			continue
		}

		key, value, ok := splitTOMLAssignment(line)
		if !ok {
			continue
		}
		value = stripTOMLInlineComment(value)

		server := config.MCPServers[currentServer]
		if err := applyCodexMCPValue(&server, currentSubtable, key, value); err != nil {
			return nil, fmt.Errorf("解析 Codex MCP 配置失败: %w", err)
		}
		config.MCPServers[currentServer] = server
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return config, nil
}

func migrateLegacyCodexMCPJSON(projectRoot, legacyPath string) (string, error) {
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	var legacy types.MCPConfig
	if err := json.Unmarshal(data, &legacy); err != nil {
		return "", fmt.Errorf("解析旧版 Codex MCP 配置失败 (%s): %w", legacyPath, err)
	}
	if legacy.MCPServers == nil {
		legacy.MCPServers = make(map[string]types.MCPServer)
	}

	target := newCodexIDE("codex")
	current, err := target.LoadMCPConfig(projectRoot)
	if err != nil {
		return "", err
	}
	if current.MCPServers == nil {
		current.MCPServers = make(map[string]types.MCPServer)
	}

	toAppend := make(map[string]types.MCPServer)
	for name, server := range legacy.MCPServers {
		if _, exists := current.MCPServers[name]; exists {
			continue
		}
		current.MCPServers[name] = server
		toAppend[name] = server
	}

	if len(toAppend) > 0 {
		if err := appendCodexMCPServers(projectRoot, toAppend); err != nil {
			return "", err
		}
	}
	if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	_ = removeDirIfEmpty(filepath.Dir(legacyPath))

	return fmt.Sprintf("%s -> .codex/config.toml", relProjectPath(projectRoot, legacyPath)), nil
}

func migrateLegacyProjectDir(srcRoot, dstRoot string) (int, error) {
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if err := os.MkdirAll(dstRoot, 0755); err != nil {
		return 0, err
	}

	moved := 0
	for _, entry := range entries {
		srcPath := filepath.Join(srcRoot, entry.Name())
		dstPath := filepath.Join(dstRoot, entry.Name())
		if _, err := os.Stat(dstPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return moved, err
		}
		if err := os.Rename(srcPath, dstPath); err != nil {
			return moved, err
		}
		moved++
	}

	_ = removeDirIfEmpty(srcRoot)
	return moved, nil
}

func removeDirIfEmpty(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func relProjectPath(projectRoot, path string) string {
	rel, err := filepath.Rel(projectRoot, path)
	if err != nil {
		return path
	}
	return rel
}

func appendCodexMCPServers(projectRoot string, servers map[string]types.MCPServer) error {
	if len(servers) == 0 {
		return nil
	}

	configPath := newCodexIDE("codex").MCPConfigPath(projectRoot)
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	blocks := make([]string, 0, len(names))
	for _, name := range names {
		block, err := renderCodexMCPServer(name, servers[name])
		if err != nil {
			return err
		}
		blocks = append(blocks, strings.TrimRight(block, "\n"))
	}

	content := strings.TrimRight(string(data), "\n")
	appendContent := strings.Join(blocks, "\n\n")
	if content == "" {
		content = appendContent
	} else {
		content = content + "\n\n" + appendContent
	}
	content += "\n"

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(configPath, []byte(content), 0644)
}

func mergeCodexMCPConfig(existing []byte, config *types.MCPConfig) ([]byte, error) {
	cleaned := strings.TrimRight(string(stripManagedCodexMCPSections(existing)), "\n")
	managed, err := renderManagedCodexMCPServers(config)
	if err != nil {
		return nil, err
	}

	switch {
	case cleaned == "" && managed == "":
		return []byte{}, nil
	case cleaned == "":
		return []byte(managed), nil
	case managed == "":
		return []byte(cleaned + "\n"), nil
	default:
		return []byte(cleaned + "\n\n" + managed), nil
	}
}

func stripManagedCodexMCPSections(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}

	lines := strings.SplitAfter(string(data), "\n")
	var out strings.Builder
	skip := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if path, ok := parseTOMLSectionHeader(trimmed); ok {
			skip = isManagedCodexMCPSection(path)
		}
		if skip {
			continue
		}
		out.WriteString(line)
	}

	return []byte(out.String())
}

func isManagedCodexMCPSection(path []string) bool {
	return len(path) >= 2 && path[0] == "mcp_servers" && strings.HasPrefix(path[1], "dec-")
}

func renderManagedCodexMCPServers(config *types.MCPConfig) (string, error) {
	if config == nil || len(config.MCPServers) == 0 {
		return "", nil
	}

	var names []string
	for name := range config.MCPServers {
		if strings.HasPrefix(name, "dec-") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", nil
	}

	sort.Strings(names)

	blocks := make([]string, 0, len(names))
	for _, name := range names {
		block, err := renderCodexMCPServer(name, config.MCPServers[name])
		if err != nil {
			return "", err
		}
		blocks = append(blocks, block)
	}

	return strings.Join(blocks, "\n\n"), nil
}

func normalizeCodexMCPServer(server types.MCPServer) types.MCPServer {
	if server.Enabled == nil {
		enabled := true
		server.Enabled = &enabled
	}

	if strings.TrimSpace(server.Command) != "" {
		server = normalizeCodexStdioServer(server)
	}
	if strings.TrimSpace(server.URL) != "" {
		server = normalizeCodexHTTPServer(server)
	}

	return server
}

func normalizeCodexStdioServer(server types.MCPServer) types.MCPServer {
	if len(server.Env) == 0 {
		if len(server.EnvVars) > 1 {
			server.EnvVars = dedupeSortedStrings(server.EnvVars)
		}
		return server
	}

	env := cloneStringMap(server.Env)
	envVarsSet := make(map[string]bool, len(server.EnvVars))
	for _, name := range server.EnvVars {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			envVarsSet[trimmed] = true
		}
	}

	changed := false
	for key, value := range server.Env {
		refName, ok := parseCodexEnvReference(value)
		if !ok || key != refName {
			continue
		}
		envVarsSet[refName] = true
		delete(env, key)
		changed = true
	}

	if len(envVarsSet) != len(server.EnvVars) {
		changed = true
	}

	if !changed {
		return server
	}

	server.Env = env
	if len(server.Env) == 0 {
		server.Env = nil
	}
	server.EnvVars = sortedStringSet(envVarsSet)
	return server
}

func normalizeCodexHTTPServer(server types.MCPServer) types.MCPServer {
	if len(server.HTTPHeaders) == 0 {
		return server
	}

	headers := cloneStringMap(server.HTTPHeaders)
	envHeaders := cloneStringMap(server.EnvHTTPHeaders)
	changed := false

	for name, value := range server.HTTPHeaders {
		if strings.EqualFold(name, "Authorization") && strings.TrimSpace(server.BearerTokenEnvVar) == "" {
			if tokenEnv, ok := parseCodexBearerEnvReference(value); ok {
				server.BearerTokenEnvVar = tokenEnv
				delete(headers, name)
				changed = true
				continue
			}
		}

		envName, ok := parseCodexEnvReference(value)
		if !ok {
			continue
		}
		if envHeaders == nil {
			envHeaders = make(map[string]string)
		}
		if existing, exists := envHeaders[name]; !exists || strings.TrimSpace(existing) == "" {
			envHeaders[name] = envName
		}
		delete(headers, name)
		changed = true
	}

	if !changed {
		return server
	}

	server.HTTPHeaders = headers
	if len(server.HTTPHeaders) == 0 {
		server.HTTPHeaders = nil
	}
	server.EnvHTTPHeaders = envHeaders
	if len(server.EnvHTTPHeaders) == 0 {
		server.EnvHTTPHeaders = nil
	}
	return server
}

func parseCodexEnvReference(value string) (string, bool) {
	matches := codexExactEnvRefRe.FindStringSubmatch(strings.TrimSpace(value))
	if len(matches) != 2 {
		return "", false
	}
	return matches[1], true
}

func parseCodexBearerEnvReference(value string) (string, bool) {
	matches := codexBearerEnvRefRe.FindStringSubmatch(strings.TrimSpace(value))
	if len(matches) != 2 {
		return "", false
	}
	return matches[1], true
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func dedupeSortedStrings(values []string) []string {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			set[trimmed] = true
		}
	}
	return sortedStringSet(set)
}

func sortedStringSet(values map[string]bool) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func renderCodexMCPServer(name string, server types.MCPServer) (string, error) {
	server = normalizeCodexMCPServer(server)

	var lines []string
	sectionKey := formatTOMLKey(name)
	root := fmt.Sprintf("[mcp_servers.%s]", sectionKey)
	lines = append(lines, root)

	appendString := func(key, value string) error {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s = %s", key, encoded))
		return nil
	}
	appendStringArray := func(key string, values []string) error {
		if len(values) == 0 {
			return nil
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s = %s", key, encoded))
		return nil
	}
	appendInt := func(key string, value *int) {
		if value != nil {
			lines = append(lines, fmt.Sprintf("%s = %d", key, *value))
		}
	}
	appendBool := func(key string, value *bool) {
		if value != nil {
			lines = append(lines, fmt.Sprintf("%s = %t", key, *value))
		}
	}

	if err := appendString("command", server.Command); err != nil {
		return "", err
	}
	if err := appendStringArray("args", server.Args); err != nil {
		return "", err
	}
	if err := appendStringArray("env_vars", server.EnvVars); err != nil {
		return "", err
	}
	if err := appendString("cwd", server.Cwd); err != nil {
		return "", err
	}
	if err := appendString("url", server.URL); err != nil {
		return "", err
	}
	if err := appendString("bearer_token_env_var", server.BearerTokenEnvVar); err != nil {
		return "", err
	}
	appendInt("startup_timeout_sec", server.StartupTimeoutSec)
	appendInt("tool_timeout_sec", server.ToolTimeoutSec)
	appendBool("enabled", server.Enabled)
	appendBool("required", server.Required)
	if err := appendStringArray("enabled_tools", server.EnabledTools); err != nil {
		return "", err
	}
	if err := appendStringArray("disabled_tools", server.DisabledTools); err != nil {
		return "", err
	}
	if err := appendStringArray("scopes", server.Scopes); err != nil {
		return "", err
	}

	appendMapSection := func(name string, values map[string]string) error {
		if len(values) == 0 {
			return nil
		}
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("[mcp_servers.%s.%s]", sectionKey, name))

		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			encoded, err := json.Marshal(values[key])
			if err != nil {
				return err
			}
			lines = append(lines, fmt.Sprintf("%s = %s", formatTOMLKey(key), encoded))
		}
		return nil
	}

	if err := appendMapSection("env", server.Env); err != nil {
		return "", err
	}
	if err := appendMapSection("http_headers", server.HTTPHeaders); err != nil {
		return "", err
	}
	if err := appendMapSection("env_http_headers", server.EnvHTTPHeaders); err != nil {
		return "", err
	}

	return strings.Join(lines, "\n") + "\n", nil
}

func applyCodexMCPValue(server *types.MCPServer, subtable, key, value string) error {
	if subtable != "" {
		parsedKey, err := parseTOMLKey(key)
		if err != nil {
			return err
		}
		parsedValue, err := parseJSONString(value)
		if err != nil {
			return err
		}
		switch subtable {
		case "env":
			if server.Env == nil {
				server.Env = make(map[string]string)
			}
			server.Env[parsedKey] = parsedValue
		case "http_headers":
			if server.HTTPHeaders == nil {
				server.HTTPHeaders = make(map[string]string)
			}
			server.HTTPHeaders[parsedKey] = parsedValue
		case "env_http_headers":
			if server.EnvHTTPHeaders == nil {
				server.EnvHTTPHeaders = make(map[string]string)
			}
			server.EnvHTTPHeaders[parsedKey] = parsedValue
		}
		return nil
	}

	switch strings.TrimSpace(key) {
	case "command":
		parsed, err := parseJSONString(value)
		if err != nil {
			return err
		}
		server.Command = parsed
	case "args":
		parsed, err := parseJSONStringArray(value)
		if err != nil {
			return err
		}
		server.Args = parsed
	case "env_vars":
		parsed, err := parseJSONStringArray(value)
		if err != nil {
			return err
		}
		server.EnvVars = parsed
	case "cwd":
		parsed, err := parseJSONString(value)
		if err != nil {
			return err
		}
		server.Cwd = parsed
	case "url":
		parsed, err := parseJSONString(value)
		if err != nil {
			return err
		}
		server.URL = parsed
	case "bearer_token_env_var":
		parsed, err := parseJSONString(value)
		if err != nil {
			return err
		}
		server.BearerTokenEnvVar = parsed
	case "startup_timeout_sec":
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return err
		}
		server.StartupTimeoutSec = &parsed
	case "tool_timeout_sec":
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return err
		}
		server.ToolTimeoutSec = &parsed
	case "enabled":
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return err
		}
		server.Enabled = &parsed
	case "required":
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return err
		}
		server.Required = &parsed
	case "enabled_tools":
		parsed, err := parseJSONStringArray(value)
		if err != nil {
			return err
		}
		server.EnabledTools = parsed
	case "disabled_tools":
		parsed, err := parseJSONStringArray(value)
		if err != nil {
			return err
		}
		server.DisabledTools = parsed
	case "scopes":
		parsed, err := parseJSONStringArray(value)
		if err != nil {
			return err
		}
		server.Scopes = parsed
	}

	return nil
}

func parseTOMLSectionHeader(line string) ([]string, bool) {
	line = strings.TrimSpace(line)
	if len(line) < 3 || !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") || strings.HasPrefix(line, "[[") {
		return nil, false
	}

	path, err := splitTOMLPath(strings.TrimSpace(line[1 : len(line)-1]))
	if err != nil {
		return nil, false
	}
	return path, true
}

func codexMCPSectionTarget(path []string) (string, string) {
	if len(path) < 2 || path[0] != "mcp_servers" {
		return "", ""
	}

	server := path[1]
	if len(path) == 2 {
		return server, ""
	}
	if len(path) == 3 {
		switch path[2] {
		case "env", "http_headers", "env_http_headers":
			return server, path[2]
		}
	}

	return "", ""
}

func splitTOMLPath(path string) ([]string, error) {
	var parts []string
	for len(path) > 0 {
		path = strings.TrimSpace(path)
		if path == "" {
			break
		}

		consumedQuoted := false
		if path[0] == '"' {
			idx := 1
			escaped := false
			for idx < len(path) {
				if escaped {
					escaped = false
					idx++
					continue
				}
				switch path[idx] {
				case '\\':
					escaped = true
				case '"':
					segment, err := strconv.Unquote(path[:idx+1])
					if err != nil {
						return nil, err
					}
					parts = append(parts, segment)
					path = path[idx+1:]
					consumedQuoted = true
					break
				}
				idx++
			}
			if consumedQuoted {
				path = strings.TrimSpace(path)
				if path == "" {
					break
				}
				if path[0] != '.' {
					return nil, fmt.Errorf("非法 TOML 路径: %s", path)
				}
				path = path[1:]
				continue
			}
			return nil, fmt.Errorf("未闭合的 TOML 字符串: %s", path)
		}

		idx := strings.IndexByte(path, '.')
		if idx == -1 {
			parts = append(parts, strings.TrimSpace(path))
			break
		}
		parts = append(parts, strings.TrimSpace(path[:idx]))
		path = path[idx:]

		path = strings.TrimSpace(path)
		if path == "" {
			break
		}
		if path[0] != '.' {
			return nil, fmt.Errorf("非法 TOML 路径: %s", path)
		}
		path = path[1:]
	}

	return parts, nil
}

func splitTOMLAssignment(line string) (string, string, bool) {
	quoted := false
	escaped := false
	for i, r := range line {
		switch r {
		case '\\':
			if quoted {
				escaped = !escaped
				continue
			}
		case '"':
			if !escaped {
				quoted = !quoted
			}
		case '=':
			if !quoted {
				return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
			}
		}
		escaped = false
	}
	return "", "", false
}

func stripTOMLInlineComment(value string) string {
	quoted := false
	escaped := false
	for i, r := range value {
		switch r {
		case '\\':
			if quoted {
				escaped = !escaped
				continue
			}
		case '"':
			if !escaped {
				quoted = !quoted
			}
		case '#':
			if !quoted {
				return strings.TrimSpace(value[:i])
			}
		}
		escaped = false
	}
	return strings.TrimSpace(value)
}

func parseTOMLKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, "\"") {
		return strconv.Unquote(key)
	}
	return key, nil
}

func formatTOMLKey(key string) string {
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			return strconv.Quote(key)
		}
		return string(encoded)
	}
	return key
}

func parseJSONString(value string) (string, error) {
	var parsed string
	if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &parsed); err != nil {
		return "", err
	}
	return parsed, nil
}

func parseJSONStringArray(value string) ([]string, error) {
	var parsed []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}
