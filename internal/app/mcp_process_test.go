package app

import (
	"testing"

	"github.com/shichao402/Dec/internal/types"
)

func TestCmdlineMatchesServer(t *testing.T) {
	vikunja := types.MCPServer{
		Command: "dec-exec",
		Args:    []string{"--project-root", "/proj", "--p", "vikunja", "--", "npx", "-y", "vikunja-mcp"},
	}
	decBare := types.MCPServer{Command: "dec-mcp"}
	decWrapped := types.MCPServer{
		Command: "C:\\Users\\x\\.dec\\bin\\dec-exec.exe",
		Args:    []string{"--project-root", "C:\\Users\\x", "--", "dec-mcp"},
	}

	cases := []struct {
		name    string
		cmdline string
		server  types.MCPServer
		want    bool
	}{
		{
			name:    "vikunja hit",
			cmdline: `dec-exec --project-root /proj --p vikunja -- npx -y vikunja-mcp`,
			server:  vikunja,
			want:    true,
		},
		{
			name:    "vikunja miss other bundle",
			cmdline: `dec-exec --project-root /proj --p other -- npx -y vikunja-mcp`,
			server:  vikunja,
			want:    false,
		},
		{
			name:    "dec bare hit",
			cmdline: `C:\Users\x\.dec\bin\dec-mcp.exe`,
			server:  decBare,
			want:    true,
		},
		{
			name:    "dec wrapped hit",
			cmdline: `C:\Users\x\.dec\bin\dec-exec.exe --project-root C:\Users\x -- dec-mcp`,
			server:  decWrapped,
			want:    true,
		},
		{
			name:    "dec bare does not match vikunja",
			cmdline: `dec-mcp`,
			server:  vikunja,
			want:    false,
		},
		{
			name:    "node alone never matches",
			cmdline: `node C:\cursor\resources\app\out\main.js`,
			server:  vikunja,
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cmdlineMatchesServer(tc.cmdline, tc.server); got != tc.want {
				t.Fatalf("cmdlineMatchesServer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMCPServerContentEqualIgnoresEnabled(t *testing.T) {
	a := types.MCPServer{Command: "dec-mcp", Enabled: boolPtr(true)}
	b := types.MCPServer{Command: "dec-mcp", Enabled: boolPtr(false)}
	if !mcpServerContentEqual(a, b) {
		t.Fatal("Enabled 不同不应算内容变更")
	}
	b.Args = []string{"--x"}
	if mcpServerContentEqual(a, b) {
		t.Fatal("Args 不同应算内容变更")
	}
}

func TestKillProcessesMatchingServerSelective(t *testing.T) {
	origList, origKill := listMCPProcesses, killMCPProcess
	t.Cleanup(func() {
		listMCPProcesses, killMCPProcess = origList, origKill
	})

	var killed []int
	listMCPProcesses = func() ([]mcpProcess, error) {
		return []mcpProcess{
			{PID: 11, CmdLine: `dec-exec --project-root /p --p vikunja -- npx -y vikunja-mcp`},
			{PID: 22, CmdLine: `dec-exec --project-root /p --p other -- npx -y other-mcp`},
			{PID: 33, CmdLine: `dec-mcp`},
			{PID: 44, CmdLine: `node something`},
		}, nil
	}
	killMCPProcess = func(pid int) error {
		killed = append(killed, pid)
		return nil
	}

	n, errs := killProcessesMatchingServer(types.MCPServer{
		Command: "dec-exec",
		Args:    []string{"--project-root", "/p", "--p", "vikunja", "--", "npx", "-y", "vikunja-mcp"},
	})
	if n != 1 || len(errs) != 0 || len(killed) != 1 || killed[0] != 11 {
		t.Fatalf("vikunja kill = n=%d errs=%v killed=%v", n, errs, killed)
	}

	killed = nil
	n, errs = killProcessesMatchingServer(types.MCPServer{Command: "dec-mcp"})
	if n != 1 || len(errs) != 0 || len(killed) != 1 || killed[0] != 33 {
		t.Fatalf("dec-mcp kill = n=%d errs=%v killed=%v", n, errs, killed)
	}
}
