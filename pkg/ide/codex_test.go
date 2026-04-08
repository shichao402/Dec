package ide

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func TestCodexWriteMCPConfigPreservesExistingConfig(t *testing.T) {
	projectRoot := t.TempDir()
	configPath := filepath.Join(projectRoot, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	existing := `model = "gpt-5.4"

[projects."/tmp/project"]
trust_level = "trusted"

[mcp_servers.user]
command = "npx"
args = ["-y", "user-mcp"]
`
	if err := os.WriteFile(configPath, []byte(existing), 0644); err != nil {
		t.Fatalf("写入现有 config.toml 失败: %v", err)
	}

	startupTimeout := 20
	enabled := true
	config := &types.MCPConfig{MCPServers: map[string]types.MCPServer{
		"dec-vikunja": {
			Command:           "npx",
			Args:              []string{"-y", "@aimbitgmbh/vikunja-mcp"},
			Env:               map[string]string{"VIKUNJA_URL": "{{VIKUNJA_URL}}", "VIKUNJA_API_TOKEN": "{{VIKUNJA_API_TOKEN}}"},
			EnvVars:           []string{"OPENAI_API_KEY"},
			StartupTimeoutSec: &startupTimeout,
			Enabled:           &enabled,
		},
	}}

	impl := Get("codex")
	if err := impl.WriteMCPConfig(projectRoot, config); err != nil {
		t.Fatalf("写入 Codex MCP 配置失败: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取 Codex config.toml 失败: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`model = "gpt-5.4"`,
		`[projects."/tmp/project"]`,
		`[mcp_servers.user]`,
		`[mcp_servers.dec-vikunja]`,
		`args = ["-y","@aimbitgmbh/vikunja-mcp"]`,
		`env_vars = ["OPENAI_API_KEY"]`,
		`startup_timeout_sec = 20`,
		`enabled = true`,
		`[mcp_servers.dec-vikunja.env]`,
		`VIKUNJA_URL = "{{VIKUNJA_URL}}"`,
		`VIKUNJA_API_TOKEN = "{{VIKUNJA_API_TOKEN}}"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("config.toml 缺少 %q:\n%s", want, content)
		}
	}

	loaded, err := impl.LoadMCPConfig(projectRoot)
	if err != nil {
		t.Fatalf("重新加载 Codex MCP 配置失败: %v", err)
	}
	server, ok := loaded.MCPServers["dec-vikunja"]
	if !ok {
		t.Fatalf("应能读取 dec-vikunja 条目: %#v", loaded.MCPServers)
	}
	if server.Command != "npx" {
		t.Fatalf("Command = %q, 期望 %q", server.Command, "npx")
	}
	if len(server.Args) != 2 || server.Args[1] != "@aimbitgmbh/vikunja-mcp" {
		t.Fatalf("Args = %#v, 期望包含 @aimbitgmbh/vikunja-mcp", server.Args)
	}
	if server.Env["VIKUNJA_URL"] != "{{VIKUNJA_URL}}" {
		t.Fatalf("Env[VIKUNJA_URL] = %q", server.Env["VIKUNJA_URL"])
	}
	if _, ok := loaded.MCPServers["user"]; !ok {
		t.Fatalf("应能保留并读取用户自定义 MCP 条目: %#v", loaded.MCPServers)
	}
}

func TestCodexWriteMCPConfigReplacesManagedSectionsOnly(t *testing.T) {
	projectRoot := t.TempDir()
	configPath := filepath.Join(projectRoot, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	existing := `[mcp_servers.user]
command = "user"

[mcp_servers.dec-old]
command = "old"
`
	if err := os.WriteFile(configPath, []byte(existing), 0644); err != nil {
		t.Fatalf("写入现有 config.toml 失败: %v", err)
	}

	impl := Get("codex")
	if err := impl.WriteMCPConfig(projectRoot, &types.MCPConfig{MCPServers: map[string]types.MCPServer{
		"dec-new": {Command: "new"},
	}}); err != nil {
		t.Fatalf("写入 Codex MCP 配置失败: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取 Codex config.toml 失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `[mcp_servers.user]`) {
		t.Fatalf("用户配置不应被移除:\n%s", content)
	}
	if strings.Contains(content, `[mcp_servers.dec-old]`) {
		t.Fatalf("旧的托管 section 应被替换:\n%s", content)
	}
	if !strings.Contains(content, `[mcp_servers.dec-new]`) {
		t.Fatalf("新的托管 section 未写入:\n%s", content)
	}
}

func TestMigrateLegacyCodexProject(t *testing.T) {
	projectRoot := t.TempDir()

	legacyCodexPath := filepath.Join(projectRoot, ".codex", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(legacyCodexPath), 0755); err != nil {
		t.Fatalf("创建 .codex 目录失败: %v", err)
	}
	if err := os.WriteFile(legacyCodexPath, []byte(`{"mcpServers":{"legacy-codex":{"command":"npx","args":["-y","codex"]}}}`), 0644); err != nil {
		t.Fatalf("写入旧 .codex/mcp.json 失败: %v", err)
	}

	legacyInternalPath := filepath.Join(projectRoot, ".codex-internal", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(legacyInternalPath), 0755); err != nil {
		t.Fatalf("创建 .codex-internal 目录失败: %v", err)
	}
	if err := os.WriteFile(legacyInternalPath, []byte(`{"mcpServers":{"legacy-internal":{"command":"uvx","args":["internal"]}}}`), 0644); err != nil {
		t.Fatalf("写入旧 .codex-internal/mcp.json 失败: %v", err)
	}

	legacySkill := filepath.Join(projectRoot, ".codex-internal", "skills", "dec-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(legacySkill), 0755); err != nil {
		t.Fatalf("创建旧 skill 目录失败: %v", err)
	}
	if err := os.WriteFile(legacySkill, []byte("legacy"), 0644); err != nil {
		t.Fatalf("写入旧 skill 失败: %v", err)
	}

	legacyRule := filepath.Join(projectRoot, ".codex-internal", "rules", "dec-rule.mdc")
	if err := os.MkdirAll(filepath.Dir(legacyRule), 0755); err != nil {
		t.Fatalf("创建旧 rule 目录失败: %v", err)
	}
	if err := os.WriteFile(legacyRule, []byte("# legacy"), 0644); err != nil {
		t.Fatalf("写入旧 rule 失败: %v", err)
	}

	notes, err := MigrateLegacyCodexProject(projectRoot)
	if err != nil {
		t.Fatalf("迁移旧版 Codex 项目布局失败: %v", err)
	}
	if len(notes) != 4 {
		t.Fatalf("迁移说明条数 = %d, 期望 4, 实际: %#v", len(notes), notes)
	}

	configPath := filepath.Join(projectRoot, ".codex", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取迁移后的 Codex config.toml 失败: %v", err)
	}
	content := string(data)
	for _, want := range []string{`[mcp_servers.legacy-codex]`, `[mcp_servers.legacy-internal]`} {
		if !strings.Contains(content, want) {
			t.Fatalf("迁移后的 config.toml 缺少 %q:\n%s", want, content)
		}
	}

	for _, oldPath := range []string{legacyCodexPath, legacyInternalPath} {
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Fatalf("旧文件应被移除 %s: %v", oldPath, err)
		}
	}

	for _, newPath := range []string{
		filepath.Join(projectRoot, ".codex", "skills", "dec-skill", "SKILL.md"),
		filepath.Join(projectRoot, ".codex", "rules", "dec-rule.mdc"),
	} {
		if _, err := os.Stat(newPath); err != nil {
			t.Fatalf("迁移后的文件不存在 %s: %v", newPath, err)
		}
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".codex-internal", "skills", "dec-skill", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("旧 skill 路径应已移除: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".codex-internal", "rules", "dec-rule.mdc")); !os.IsNotExist(err) {
		t.Fatalf("旧 rule 路径应已移除: %v", err)
	}

	loaded, err := Get("codex").LoadMCPConfig(projectRoot)
	if err != nil {
		t.Fatalf("读取迁移后的 Codex MCP 配置失败: %v", err)
	}
	for _, name := range []string{"legacy-codex", "legacy-internal"} {
		if _, ok := loaded.MCPServers[name]; !ok {
			t.Fatalf("迁移后应存在 %s, 实际 %#v", name, loaded.MCPServers)
		}
	}
}
