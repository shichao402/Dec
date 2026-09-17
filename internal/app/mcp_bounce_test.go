package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/ide"
	"github.com/shichao402/Dec/internal/types"
)

func TestBounceInstallMCPSequence(t *testing.T) {
	origList, origKill := listMCPProcesses, killMCPProcess
	t.Cleanup(func() {
		listMCPProcesses, killMCPProcess = origList, origKill
	})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	project := t.TempDir()
	cursor := ide.Get("cursor")
	if cursor == nil {
		t.Fatal("cursor IDE 未注册")
	}

	var writes []string
	var killed []int
	listMCPProcesses = func() ([]mcpProcess, error) {
		return []mcpProcess{
			{PID: 99, CmdLine: `dec-exec --project-root ` + project + ` --p demo -- npx -y old-mcp`},
		}, nil
	}
	killMCPProcess = func(pid int) error {
		killed = append(killed, pid)
		cfg, err := cursor.LoadMCPConfigForPlane(ide.PlaneProject, project, home)
		if err != nil {
			t.Fatalf("kill 时读配置: %v", err)
		}
		server := cfg.MCPServers["dec-demo"]
		if server.Enabled == nil || *server.Enabled {
			t.Fatal("杀进程前条目应为 enabled=false")
		}
		writes = append(writes, "kill")
		return nil
	}

	old := types.MCPServer{
		Command: "dec-exec",
		Args:    []string{"--project-root", project, "--p", "demo", "--", "npx", "-y", "old-mcp"},
	}
	cfg := &types.MCPConfig{MCPServers: map[string]types.MCPServer{"dec-demo": old}}
	if err := cursor.WriteMCPConfigForPlane(ide.PlaneProject, project, home, cfg); err != nil {
		t.Fatal(err)
	}
	writes = append(writes, "seed")

	next := types.MCPServer{
		Command: "dec-exec",
		Args:    []string{"--project-root", project, "--p", "demo", "--", "npx", "-y", "new-mcp"},
	}
	bounced, err := bounceInstallMCP(cursor, ide.PlaneProject, project, home, "dec-demo", next)
	if err != nil {
		t.Fatal(err)
	}
	if !bounced {
		t.Fatal("内容变更应 bounce")
	}
	if len(killed) != 1 || killed[0] != 99 {
		t.Fatalf("应杀掉匹配进程, killed=%v", killed)
	}

	final, err := cursor.LoadMCPConfigForPlane(ide.PlaneProject, project, home)
	if err != nil {
		t.Fatal(err)
	}
	got := final.MCPServers["dec-demo"]
	if got.Args[len(got.Args)-1] != "new-mcp" {
		t.Fatalf("最终 args = %#v", got.Args)
	}
	if got.Enabled != nil && !*got.Enabled {
		t.Fatal("最终不应保持关闭")
	}

	// 再装一次相同内容：不 bounce、不杀。
	killed = nil
	bounced, err = bounceInstallMCP(cursor, ide.PlaneProject, project, home, "dec-demo", next)
	if err != nil {
		t.Fatal(err)
	}
	if bounced || len(killed) != 0 {
		t.Fatalf("相同内容不应 bounce: bounced=%v killed=%v", bounced, killed)
	}

	mcpPath := cursor.MCPConfigPath(project)
	if _, err := os.Stat(mcpPath); err != nil {
		t.Fatalf("mcp.json 应存在: %v (%s)", err, mcpPath)
	}
	_ = filepath.Base(mcpPath)
}

func TestBounceInstallMCPKeepsUserDisabled(t *testing.T) {
	origList, origKill := listMCPProcesses, killMCPProcess
	t.Cleanup(func() {
		listMCPProcesses, killMCPProcess = origList, origKill
	})
	listMCPProcesses = func() ([]mcpProcess, error) { return nil, nil }
	killMCPProcess = func(pid int) error { return nil }

	home := t.TempDir()
	project := t.TempDir()
	cursor := ide.Get("cursor")
	old := types.MCPServer{Command: "dec-mcp", Enabled: boolPtr(false)}
	cfg := &types.MCPConfig{MCPServers: map[string]types.MCPServer{"dec": old}}
	if err := cursor.WriteMCPConfigForPlane(ide.PlaneProject, project, home, cfg); err != nil {
		t.Fatal(err)
	}
	next := types.MCPServer{Command: "dec-mcp", Args: []string{"--fresh"}}
	bounced, err := bounceInstallMCP(cursor, ide.PlaneProject, project, home, "dec", next)
	if err != nil || !bounced {
		t.Fatalf("bounced=%v err=%v", bounced, err)
	}
	final, err := cursor.LoadMCPConfigForPlane(ide.PlaneProject, project, home)
	if err != nil {
		t.Fatal(err)
	}
	got := final.MCPServers["dec"]
	if got.Enabled == nil || *got.Enabled {
		t.Fatal("用户关闭的条目最终应仍保持 Enabled=false")
	}
}

func TestBounceRemoveMCP(t *testing.T) {
	origList, origKill := listMCPProcesses, killMCPProcess
	t.Cleanup(func() {
		listMCPProcesses, killMCPProcess = origList, origKill
	})
	var killed int
	listMCPProcesses = func() ([]mcpProcess, error) {
		return []mcpProcess{{PID: 7, CmdLine: `dec-mcp`}}, nil
	}
	killMCPProcess = func(pid int) error {
		killed = pid
		return nil
	}

	home := t.TempDir()
	project := t.TempDir()
	cursor := ide.Get("cursor")
	cfg := &types.MCPConfig{MCPServers: map[string]types.MCPServer{
		"dec": {Command: "dec-mcp"},
	}}
	if err := cursor.WriteMCPConfigForPlane(ide.PlaneProject, project, home, cfg); err != nil {
		t.Fatal(err)
	}
	removed, bounced, err := bounceRemoveMCP(cursor, ide.PlaneProject, project, home, "dec")
	if err != nil || !removed || !bounced {
		t.Fatalf("removed=%v bounced=%v err=%v", removed, bounced, err)
	}
	if killed != 7 {
		t.Fatalf("killed=%d", killed)
	}
	final, err := cursor.LoadMCPConfigForPlane(ide.PlaneProject, project, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := final.MCPServers["dec"]; ok {
		t.Fatal("条目应已删除")
	}
}
