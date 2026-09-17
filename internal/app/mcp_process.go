package app

import (
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/types"
)

// mcpProcess 是本机一条可匹配的进程快照。
type mcpProcess struct {
	PID     int
	CmdLine string
}

// listMCPProcesses / killMCPProcess 由各平台实现；测试可替换。
var (
	listMCPProcesses = listMCPProcessesOS
	killMCPProcess   = killMCPProcessOS
)

// cmdlineMatchesServer 判断进程命令行是否对应该条 MCP 配置。
// 按 command 基名 + args 顺序出现匹配，避免按进程名屠掉无关 node/npx。
func cmdlineMatchesServer(cmdline string, server types.MCPServer) bool {
	cmdline = strings.TrimSpace(cmdline)
	if cmdline == "" || strings.TrimSpace(server.Command) == "" {
		return false
	}
	needles := mcpMatchNeedles(server)
	if len(needles) == 0 {
		return false
	}
	lower := strings.ToLower(cmdline)
	pos := 0
	for _, needle := range needles {
		idx := strings.Index(lower[pos:], needle)
		if idx < 0 {
			return false
		}
		pos += idx + len(needle)
	}
	return true
}

func mcpMatchNeedles(server types.MCPServer) []string {
	base := filepath.Base(strings.TrimSpace(server.Command))
	base = strings.TrimSuffix(base, ".exe")
	if base == "" {
		return nil
	}
	out := []string{strings.ToLower(base)}
	for _, arg := range server.Args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		out = append(out, strings.ToLower(arg))
	}
	return out
}

// killProcessesMatchingServer 杀掉命令行对得上该 MCP 配置的进程。
// 错误只记日志语义由调用方处理；杀失败不阻断写配置。
func killProcessesMatchingServer(server types.MCPServer) (killed int, errs []string) {
	procs, err := listMCPProcesses()
	if err != nil {
		return 0, []string{err.Error()}
	}
	for _, proc := range procs {
		if proc.PID <= 0 || !cmdlineMatchesServer(proc.CmdLine, server) {
			continue
		}
		if err := killMCPProcess(proc.PID); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		killed++
	}
	return killed, errs
}
