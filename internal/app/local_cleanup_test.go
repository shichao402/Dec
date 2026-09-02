package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
)

func TestCleanupLocalInstallationPreservesRuntimeAndSharedConfig(t *testing.T) {
	home := t.TempDir()
	decHome := filepath.Join(home, ".dec")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DEC_HOME", decHome)

	project := filepath.Join(home, "work")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := config.RegisterManagedProject(project, "work"); err != nil {
		t.Fatal(err)
	}
	mustWriteCleanupFile(t, filepath.Join(decHome, "bin", "dec-server"), "runtime")
	mustWriteCleanupFile(t, filepath.Join(decHome, "cache", "asset"), "cache")
	mustWriteCleanupFile(t, filepath.Join(decHome, "secrets", "device.json"), `{"device_identifier":"keep"}`)
	mustWriteCleanupFile(t, filepath.Join(decHome, "secrets", "p", ".env", "global.env"), "GLOBAL=1\n")
	mustWriteCleanupFile(t, filepath.Join(project, ".dec", "config.yaml"), "kind: project\n")
	mustWriteCleanupFile(t, filepath.Join(project, ".secrets", "p", ".env", "x.env"), "X=1\n")
	mustWriteCleanupFile(t, filepath.Join(project, filepath.FromSlash(".secrets/dec/integration/bitwarden.yaml")), "email: keep\n")
	mustWriteCleanupFile(t, filepath.Join(project, filepath.FromSlash(".secrets/.integration/dec-home/secrets/device.json")), `{"keep":true}`)
	mustWriteCleanupFile(t, filepath.Join(home, ".cursor", "skills", "dec-owned", "SKILL.md"), "# owned\n")
	mustWriteCleanupFile(t, filepath.Join(home, ".cursor", "skills", "user-owned", "SKILL.md"), "# user\n")

	mcpPath := filepath.Join(home, ".cursor", "mcp.json")
	mcp := map[string]any{
		"mcpServers": map[string]any{
			"dec":  map[string]any{"command": "dec-mcp"},
			"user": map[string]any{"command": "user-mcp"},
		},
		"other": true,
	}
	data, _ := json.Marshal(mcp)
	mustWriteCleanupFile(t, mcpPath, string(data))

	sshConfig := filepath.Join(home, ".ssh", "config")
	mustWriteCleanupFile(t, sshConfig, "Include config.d/dec.conf\n\nHost personal\n  User me\n")
	mustWriteCleanupFile(t, filepath.Join(home, ".ssh", "config.d", "dec.conf"), "Host managed\n")

	preview, err := PreviewLocalCleanup()
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) == 0 {
		t.Fatal("预览应发现清理项")
	}

	result, err := CleanupLocalInstallation(context.Background(), LocalCleanupInput{Confirmed: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) == 0 || len(result.Modified) == 0 {
		t.Fatalf("清理结果不完整: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(decHome, "bin", "dec-server")); err != nil {
		t.Fatalf("运行时应保留: %v", err)
	}
	for _, path := range []string{
		filepath.Join(decHome, "cache"),
		filepath.Join(decHome, "secrets"),
		filepath.Join(project, ".dec"),
		filepath.Join(project, ".secrets", "p"),
		filepath.Join(home, ".cursor", "skills", "dec-owned"),
		filepath.Join(home, ".ssh", "config.d", "dec.conf"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("应删除 %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "skills", "user-owned", "SKILL.md")); err != nil {
		t.Fatalf("用户 Skill 应保留: %v", err)
	}
	for _, path := range []string{
		filepath.Join(project, filepath.FromSlash(".secrets/dec/integration/bitwarden.yaml")),
		filepath.Join(project, filepath.FromSlash(".secrets/.integration/dec-home/secrets/device.json")),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("受保护文件应保留 %s: %v", path, err)
		}
	}
	mcpData, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mcpData), `"dec"`) || !strings.Contains(string(mcpData), `"user"`) ||
		!strings.Contains(string(mcpData), `"other"`) {
		t.Fatalf("共享 MCP 配置清理错误: %s", mcpData)
	}
	sshData, err := os.ReadFile(sshConfig)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sshData), "dec.conf") || !strings.Contains(string(sshData), "Host personal") {
		t.Fatalf("SSH 主配置清理错误: %s", sshData)
	}
}

func TestCleanupLocalInstallationRequiresConfirmation(t *testing.T) {
	if _, err := CleanupLocalInstallation(context.Background(), LocalCleanupInput{}, nil); err == nil {
		t.Fatal("未确认不应执行清理")
	}
}

func mustWriteCleanupFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
