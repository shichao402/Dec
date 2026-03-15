package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func TestManagedName(t *testing.T) {
	if got := ManagedName("create-api-test"); got != "dec-create-api-test" {
		t.Fatalf("托管名称错误: 期望 dec-create-api-test, 得到 %s", got)
	}

	if got := ManagedName("dec-create-api-test"); got != "dec-create-api-test" {
		t.Fatalf("托管名称不应重复添加前缀: 得到 %s", got)
	}
}

func TestPullSkillToAllIDEsUsesManagedName(t *testing.T) {
	vaultDir := t.TempDir()
	projectRoot := t.TempDir()

	skillDir := filepath.Join(vaultDir, "skills", "create-api-test")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("创建 skill 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: create-api-test\n---\n"), 0644); err != nil {
		t.Fatalf("写入 SKILL.md 失败: %v", err)
	}

	v := &Vault{
		Dir: vaultDir,
		Index: &VaultIndex{
			Items: []VaultItem{
				{
					Name: "create-api-test",
					Type: "skill",
					Path: filepath.Join("skills", "create-api-test"),
				},
			},
		},
	}

	localPaths, err := v.Pull("skill", "create-api-test", projectRoot, []string{"cursor", "windsurf"})
	if err != nil {
		t.Fatalf("Pull 失败: %v", err)
	}

	expectedPaths := []string{
		filepath.Join(".cursor", "skills", "dec-create-api-test"),
		filepath.Join(".windsurf", "skills", "dec-create-api-test"),
	}
	if len(localPaths) != len(expectedPaths) {
		t.Fatalf("返回路径数量错误: 期望 %d, 得到 %d", len(expectedPaths), len(localPaths))
	}
	for i := range expectedPaths {
		if localPaths[i] != expectedPaths[i] {
			t.Fatalf("返回路径错误: 期望 %s, 得到 %s", expectedPaths[i], localPaths[i])
		}
		if _, err := os.Stat(filepath.Join(projectRoot, expectedPaths[i], "SKILL.md")); err != nil {
			t.Fatalf("托管副本未写入: %s, err=%v", expectedPaths[i], err)
		}
	}

	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "create-api-test")); !os.IsNotExist(err) {
		t.Fatalf("不应创建未托管的原始名称目录")
	}
}

func TestPullMCPWritesManagedServerToLiveConfig(t *testing.T) {
	vaultDir := t.TempDir()
	projectRoot := t.TempDir()

	mcpPath := filepath.Join(vaultDir, "mcp", "postgres-tool.json")
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0755); err != nil {
		t.Fatalf("创建 vault MCP 目录失败: %v", err)
	}
	if err := os.WriteFile(mcpPath, []byte(`{"command":"npx","args":["-y","postgres-tool"]}`), 0644); err != nil {
		t.Fatalf("写入 vault MCP 失败: %v", err)
	}

	cursorConfigPath := filepath.Join(projectRoot, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(cursorConfigPath), 0755); err != nil {
		t.Fatalf("创建 cursor MCP 目录失败: %v", err)
	}
	existingConfig := types.MCPConfig{
		MCPServers: map[string]types.MCPServer{
			"user-helper": {
				Command: "npx",
				Args:    []string{"-y", "user-helper"},
			},
		},
	}
	data, err := json.Marshal(existingConfig)
	if err != nil {
		t.Fatalf("序列化现有 MCP 配置失败: %v", err)
	}
	if err := os.WriteFile(cursorConfigPath, data, 0644); err != nil {
		t.Fatalf("写入现有 MCP 配置失败: %v", err)
	}

	v := &Vault{
		Dir: vaultDir,
		Index: &VaultIndex{
			Items: []VaultItem{
				{
					Name: "postgres-tool",
					Type: "mcp",
					Path: filepath.Join("mcp", "postgres-tool.json"),
				},
			},
		},
	}

	localPaths, err := v.Pull("mcp", "postgres-tool", projectRoot, []string{"cursor", "codebuddy"})
	if err != nil {
		t.Fatalf("Pull MCP 失败: %v", err)
	}

	expectedPaths := []string{
		filepath.Join(".cursor", "mcp.json"),
		".mcp.json",
	}
	if len(localPaths) != len(expectedPaths) {
		t.Fatalf("返回路径数量错误: 期望 %d, 得到 %d", len(expectedPaths), len(localPaths))
	}
	for i := range expectedPaths {
		if localPaths[i] != expectedPaths[i] {
			t.Fatalf("返回路径错误: 期望 %s, 得到 %s", expectedPaths[i], localPaths[i])
		}
	}

	cursorData, err := os.ReadFile(cursorConfigPath)
	if err != nil {
		t.Fatalf("读取 cursor MCP 配置失败: %v", err)
	}
	var cursorConfig types.MCPConfig
	if err := json.Unmarshal(cursorData, &cursorConfig); err != nil {
		t.Fatalf("解析 cursor MCP 配置失败: %v", err)
	}
	if _, ok := cursorConfig.MCPServers["user-helper"]; !ok {
		t.Fatalf("用户 MCP 不应被覆盖")
	}
	if _, ok := cursorConfig.MCPServers["dec-postgres-tool"]; !ok {
		t.Fatalf("托管 MCP 未写入 cursor mcp.json")
	}

	codebuddyData, err := os.ReadFile(filepath.Join(projectRoot, ".mcp.json"))
	if err != nil {
		t.Fatalf("读取 codebuddy MCP 配置失败: %v", err)
	}
	var codebuddyConfig types.MCPConfig
	if err := json.Unmarshal(codebuddyData, &codebuddyConfig); err != nil {
		t.Fatalf("解析 codebuddy MCP 配置失败: %v", err)
	}
	if _, ok := codebuddyConfig.MCPServers["dec-postgres-tool"]; !ok {
		t.Fatalf("托管 MCP 未写入 codebuddy .mcp.json")
	}
}

func TestLoadMCPServerRejectsFullMCPConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"foo":{"command":"npx"}}}`), 0644); err != nil {
		t.Fatalf("写入测试 MCP 文件失败: %v", err)
	}

	if _, err := loadMCPServer(path); err == nil {
		t.Fatalf("完整 mcp.json 不应被视为单个 MCP server 片段")
	}
}
