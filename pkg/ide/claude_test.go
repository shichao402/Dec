package ide

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func TestMigrateLegacyClaudeProject(t *testing.T) {
	projectRoot := t.TempDir()

	legacyMCPPath := filepath.Join(projectRoot, ".claude-internal", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(legacyMCPPath), 0755); err != nil {
		t.Fatalf("创建 .claude-internal 目录失败: %v", err)
	}
	if err := os.WriteFile(legacyMCPPath, []byte(`{"mcpServers":{"legacy-internal":{"command":"npx","args":["-y","claude"]}}}`), 0644); err != nil {
		t.Fatalf("写入旧 .claude-internal/mcp.json 失败: %v", err)
	}

	currentMCPPath := filepath.Join(projectRoot, ".claude", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(currentMCPPath), 0755); err != nil {
		t.Fatalf("创建 .claude 目录失败: %v", err)
	}
	current := types.MCPConfig{MCPServers: map[string]types.MCPServer{"user": {Command: "uvx"}}}
	currentData, err := json.Marshal(current)
	if err != nil {
		t.Fatalf("序列化现有 Claude MCP 配置失败: %v", err)
	}
	if err := os.WriteFile(currentMCPPath, currentData, 0644); err != nil {
		t.Fatalf("写入现有 .claude/mcp.json 失败: %v", err)
	}

	legacySkill := filepath.Join(projectRoot, ".claude-internal", "skills", "dec-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(legacySkill), 0755); err != nil {
		t.Fatalf("创建旧 skill 目录失败: %v", err)
	}
	if err := os.WriteFile(legacySkill, []byte("legacy"), 0644); err != nil {
		t.Fatalf("写入旧 skill 失败: %v", err)
	}

	legacyRule := filepath.Join(projectRoot, ".claude-internal", "rules", "dec-rule.mdc")
	if err := os.MkdirAll(filepath.Dir(legacyRule), 0755); err != nil {
		t.Fatalf("创建旧 rule 目录失败: %v", err)
	}
	if err := os.WriteFile(legacyRule, []byte("# legacy"), 0644); err != nil {
		t.Fatalf("写入旧 rule 失败: %v", err)
	}

	notes, err := MigrateLegacyClaudeProject(projectRoot)
	if err != nil {
		t.Fatalf("迁移旧版 Claude 项目布局失败: %v", err)
	}
	if len(notes) != 3 {
		t.Fatalf("迁移说明条数 = %d, 期望 3, 实际: %#v", len(notes), notes)
	}

	data, err := os.ReadFile(currentMCPPath)
	if err != nil {
		t.Fatalf("读取迁移后的 .claude/mcp.json 失败: %v", err)
	}
	content := string(data)
	for _, want := range []string{"user", "legacy-internal"} {
		if !strings.Contains(content, want) {
			t.Fatalf("迁移后的 .claude/mcp.json 缺少 %q:\n%s", want, content)
		}
	}

	for _, newPath := range []string{
		filepath.Join(projectRoot, ".claude", "skills", "dec-skill", "SKILL.md"),
		filepath.Join(projectRoot, ".claude", "rules", "dec-rule.mdc"),
	} {
		if _, err := os.Stat(newPath); err != nil {
			t.Fatalf("迁移后的文件不存在 %s: %v", newPath, err)
		}
	}

	for _, oldPath := range []string{
		legacyMCPPath,
		filepath.Join(projectRoot, ".claude-internal", "skills", "dec-skill", "SKILL.md"),
		filepath.Join(projectRoot, ".claude-internal", "rules", "dec-rule.mdc"),
	} {
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Fatalf("旧路径应被移除 %s: %v", oldPath, err)
		}
	}

	loaded, err := Get("claude").LoadMCPConfig(projectRoot)
	if err != nil {
		t.Fatalf("读取迁移后的 Claude MCP 配置失败: %v", err)
	}
	for _, name := range []string{"user", "legacy-internal"} {
		if _, ok := loaded.MCPServers[name]; !ok {
			t.Fatalf("迁移后应存在 %s, 实际 %#v", name, loaded.MCPServers)
		}
	}
}
