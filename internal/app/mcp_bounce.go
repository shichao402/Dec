package app

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/ide"
	"github.com/shichao402/Dec/internal/types"
)

func boolPtr(v bool) *bool { return &v }

// mcpServerContentEqual 比较会驱动进程/会话的字段；忽略 Enabled。
func mcpServerContentEqual(a, b types.MCPServer) bool {
	a = normalizeMCPServerForCompare(a)
	b = normalizeMCPServerForCompare(b)
	return reflect.DeepEqual(a, b)
}

func normalizeMCPServerForCompare(s types.MCPServer) types.MCPServer {
	s.Enabled = nil
	s.Command = strings.TrimSpace(s.Command)
	s.Cwd = strings.TrimSpace(s.Cwd)
	s.URL = strings.TrimSpace(s.URL)
	if len(s.Args) == 0 {
		s.Args = nil
	}
	if len(s.Env) == 0 {
		s.Env = nil
	}
	if len(s.EnvVars) == 0 {
		s.EnvVars = nil
	}
	if len(s.HTTPHeaders) == 0 {
		s.HTTPHeaders = nil
	}
	if len(s.EnvHTTPHeaders) == 0 {
		s.EnvHTTPHeaders = nil
	}
	if len(s.EnabledTools) == 0 {
		s.EnabledTools = nil
	}
	if len(s.DisabledTools) == 0 {
		s.DisabledTools = nil
	}
	if len(s.Scopes) == 0 {
		s.Scopes = nil
	}
	s.Extra = nil
	return s
}

func appendUniqueSorted(dst []string, name string) []string {
	if name == "" {
		return dst
	}
	for _, existing := range dst {
		if existing == name {
			return dst
		}
	}
	dst = append(dst, name)
	sort.Strings(dst)
	return dst
}

// bounceInstallMCP 关 → 杀 → 开。内容未变则不 bounce，返回 bounced=false。
// 用户原先 Enabled=false 的条目最终仍保持关闭，不强行打开。
func bounceInstallMCP(ideImpl ide.IDE, plane ide.Plane, projectRoot, homeDir, managed string, next types.MCPServer) (bounced bool, err error) {
	existingConfig, err := ideImpl.LoadMCPConfigForPlane(plane, projectRoot, homeDir)
	if err != nil {
		return false, fmt.Errorf("加载 IDE MCP 配置失败: %w", err)
	}
	if existingConfig.MCPServers == nil {
		existingConfig.MCPServers = make(map[string]types.MCPServer)
	}
	old, hadOld := existingConfig.MCPServers[managed]
	if hadOld && mcpServerContentEqual(old, next) {
		return false, nil
	}

	keepDisabled := hadOld && old.Enabled != nil && !*old.Enabled

	if hadOld {
		disabled := old
		disabled.Enabled = boolPtr(false)
		existingConfig.MCPServers[managed] = disabled
		if err := ideImpl.WriteMCPConfigForPlane(plane, projectRoot, homeDir, existingConfig); err != nil {
			return false, fmt.Errorf("关闭 MCP %s 失败: %w", managed, err)
		}
		_, _ = killProcessesMatchingServer(old)
	}

	if keepDisabled {
		next.Enabled = boolPtr(false)
	} else {
		next.Enabled = nil
	}
	existingConfig.MCPServers[managed] = next
	if err := ideImpl.WriteMCPConfigForPlane(plane, projectRoot, homeDir, existingConfig); err != nil {
		return false, fmt.Errorf("写入 MCP %s 失败: %w", managed, err)
	}
	return true, nil
}

// bounceRemoveMCP 关 → 杀 → 删条目。
func bounceRemoveMCP(ideImpl ide.IDE, plane ide.Plane, projectRoot, homeDir, managed string) (removed bool, bounced bool, err error) {
	existingConfig, err := ideImpl.LoadMCPConfigForPlane(plane, projectRoot, homeDir)
	if err != nil {
		return false, false, nil
	}
	old, exists := existingConfig.MCPServers[managed]
	if !exists {
		return false, false, nil
	}

	disabled := old
	disabled.Enabled = boolPtr(false)
	existingConfig.MCPServers[managed] = disabled
	if err := ideImpl.WriteMCPConfigForPlane(plane, projectRoot, homeDir, existingConfig); err != nil {
		return false, false, fmt.Errorf("关闭 MCP %s 失败: %w", managed, err)
	}
	_, _ = killProcessesMatchingServer(old)

	delete(existingConfig.MCPServers, managed)
	if err := ideImpl.WriteMCPConfigForPlane(plane, projectRoot, homeDir, existingConfig); err != nil {
		return false, false, fmt.Errorf("删除 MCP %s 失败: %w", managed, err)
	}
	return true, true, nil
}
