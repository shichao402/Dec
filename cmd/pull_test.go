package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateLegacyProjectLayoutsForCodex(t *testing.T) {
	projectRoot := t.TempDir()
	legacyPath := filepath.Join(projectRoot, ".codex-internal", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		t.Fatalf("创建旧目录失败: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"mcpServers":{"legacy":{"command":"npx"}}}`), 0644); err != nil {
		t.Fatalf("写入旧 mcp.json 失败: %v", err)
	}

	notes, err := migrateLegacyProjectLayouts(projectRoot, uniqueProjectIDEs(projectRoot, []string{"cursor", "codex-internal"}))
	if err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if len(notes) == 0 {
		t.Fatal("包含 codex-internal 时应触发迁移")
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("读取迁移结果失败: %v", err)
	}
	if !strings.Contains(string(data), `[mcp_servers.legacy]`) {
		t.Fatalf("迁移后的 .codex/config.toml 缺少 legacy 条目:\n%s", string(data))
	}
}

func TestMigrateLegacyProjectLayoutsForClaude(t *testing.T) {
	projectRoot := t.TempDir()
	legacyPath := filepath.Join(projectRoot, ".claude-internal", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		t.Fatalf("创建旧目录失败: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"mcpServers":{"legacy":{"command":"npx"}}}`), 0644); err != nil {
		t.Fatalf("写入旧 mcp.json 失败: %v", err)
	}

	notes, err := migrateLegacyProjectLayouts(projectRoot, uniqueProjectIDEs(projectRoot, []string{"claude-internal"}))
	if err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if len(notes) == 0 {
		t.Fatal("包含 claude-internal 时应触发迁移")
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, ".claude", "mcp.json"))
	if err != nil {
		t.Fatalf("读取迁移结果失败: %v", err)
	}
	if !strings.Contains(string(data), `"legacy"`) {
		t.Fatalf("迁移后的 .claude/mcp.json 缺少 legacy 条目:\n%s", string(data))
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".claude-internal", "mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("旧的 .claude-internal/mcp.json 应被移除: %v", err)
	}
}

func TestMigrateLegacyProjectLayoutsSkipsWithoutCodex(t *testing.T) {
	projectRoot := t.TempDir()
	notes, err := migrateLegacyProjectLayouts(projectRoot, uniqueProjectIDEs(projectRoot, []string{"cursor", "codebuddy"}))
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("不包含 codex IDE 时不应触发迁移, 得到 %#v", notes)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("不包含 codex IDE 时不应创建 .codex/config.toml: %v", err)
	}
}
