package ide

import (
	"sort"
	"testing"
)

func TestGetRegisteredIDE(t *testing.T) {
	cursor := Get("cursor")
	if cursor.Name() != "cursor" {
		t.Fatalf("期望 IDE 名称 cursor，得到 %s", cursor.Name())
	}
}

func TestGetUnregisteredIDEReturnsFallback(t *testing.T) {
	unknown := Get("unknown-ide")
	if unknown.Name() != "unknown-ide" {
		t.Fatalf("未注册 IDE 应返回 fallback，名称为 unknown-ide，得到 %s", unknown.Name())
	}
	// fallback 应使用 .unknown-ide 作为目录
	dir := unknown.RulesDir("/project")
	if dir != "/project/.unknown-ide/rules" {
		t.Fatalf("未注册 IDE 的 RulesDir 应为 /project/.unknown-ide/rules，得到 %s", dir)
	}
}

func TestIsValidRegistered(t *testing.T) {
	for _, name := range []string{"cursor", "codebuddy", "claude", "claude-internal", "codex", "codex-internal"} {
		if !IsValid(name) {
			t.Fatalf("已注册 IDE %s 应返回 IsValid=true", name)
		}
	}
}

func TestIsValidUnregistered(t *testing.T) {
	if IsValid("nonexistent") {
		t.Fatalf("未注册 IDE 应返回 IsValid=false")
	}
	if IsValid("windsurf") {
		t.Fatalf("已移除的 IDE windsurf 应返回 IsValid=false")
	}
	if IsValid("trae") {
		t.Fatalf("已移除的 IDE trae 应返回 IsValid=false")
	}
	if IsValid("") {
		t.Fatalf("空字符串应返回 IsValid=false")
	}
}

func TestListContainsAllRegistered(t *testing.T) {
	names := List()
	sort.Strings(names)

	expected := []string{"claude", "claude-internal", "codebuddy", "codex", "codex-internal", "cursor"}
	if len(names) != len(expected) {
		t.Fatalf("期望 %d 个 IDE，得到 %d 个: %v", len(expected), len(names), names)
	}
	for i := range expected {
		if names[i] != expected[i] {
			t.Fatalf("IDE 列表不匹配: 期望 %v，得到 %v", expected, names)
		}
	}
}

func TestCodebuddyMCPConfigPath(t *testing.T) {
	cb := Get("codebuddy")
	path := cb.MCPConfigPath("/project")
	if path != "/project/.mcp.json" {
		t.Fatalf("CodeBuddy MCP 配置路径应为 /project/.mcp.json，得到 %s", path)
	}
}

func TestCursorMCPConfigPath(t *testing.T) {
	cursor := Get("cursor")
	path := cursor.MCPConfigPath("/project")
	if path != "/project/.cursor/mcp.json" {
		t.Fatalf("Cursor MCP 配置路径应为 /project/.cursor/mcp.json，得到 %s", path)
	}
}

func TestCodexMCPConfigPath(t *testing.T) {
	codex := Get("codex")
	path := codex.MCPConfigPath("/project")
	if path != "/project/.codex/config.toml" {
		t.Fatalf("Codex MCP 配置路径应为 /project/.codex/config.toml，得到 %s", path)
	}
}

func TestCodexInternalProjectUsesCodexPath(t *testing.T) {
	impl := Get("codex-internal")
	if rulesDir := impl.RulesDir("/project"); rulesDir != "/project/.codex/rules" {
		t.Fatalf("codex-internal 项目级 RulesDir 应复用 /project/.codex/rules，得到 %s", rulesDir)
	}
	if skillsDir := impl.SkillsDir("/project"); skillsDir != "/project/.codex/skills" {
		t.Fatalf("codex-internal 项目级 SkillsDir 应复用 /project/.codex/skills，得到 %s", skillsDir)
	}
	if path := impl.MCPConfigPath("/project"); path != "/project/.codex/config.toml" {
		t.Fatalf("codex-internal 项目级 MCP 配置应复用 /project/.codex/config.toml，得到 %s", path)
	}
}

func TestIDEDirectoryStructure(t *testing.T) {
	tests := []struct {
		ide       string
		rulesDir  string
		skillsDir string
	}{
		{"cursor", "/project/.cursor/rules", "/project/.cursor/skills"},
		{"codebuddy", "/project/.codebuddy/rules", "/project/.codebuddy/skills"},
		{"claude", "/project/.claude/rules", "/project/.claude/skills"},
		{"claude-internal", "/project/.claude-internal/rules", "/project/.claude-internal/skills"},
		{"codex", "/project/.codex/rules", "/project/.codex/skills"},
		{"codex-internal", "/project/.codex/rules", "/project/.codex/skills"},
	}

	for _, tt := range tests {
		impl := Get(tt.ide)
		if impl.RulesDir("/project") != tt.rulesDir {
			t.Fatalf("%s RulesDir: 期望 %s，得到 %s", tt.ide, tt.rulesDir, impl.RulesDir("/project"))
		}
		if impl.SkillsDir("/project") != tt.skillsDir {
			t.Fatalf("%s SkillsDir: 期望 %s，得到 %s", tt.ide, tt.skillsDir, impl.SkillsDir("/project"))
		}
	}
}
