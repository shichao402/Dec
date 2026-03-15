package service

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/types"
)

func TestCleanManagedSkills(t *testing.T) {
	projectRoot := t.TempDir()
	svc := &SyncServiceV2{projectRoot: projectRoot}
	ideImpl := ide.Get("cursor")

	skillsDir := filepath.Join(projectRoot, ".cursor", "skills")

	managedDir := filepath.Join(skillsDir, "dec-my-skill")
	if err := os.MkdirAll(managedDir, 0755); err != nil {
		t.Fatalf("创建托管 skill 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managedDir, "SKILL.md"), []byte("managed"), 0644); err != nil {
		t.Fatalf("写入托管 skill 失败: %v", err)
	}

	userDir := filepath.Join(skillsDir, "my-local-skill")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatalf("创建用户 skill 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "SKILL.md"), []byte("local"), 0644); err != nil {
		t.Fatalf("写入用户 skill 失败: %v", err)
	}

	if err := svc.cleanManagedSkills(ideImpl); err != nil {
		t.Fatalf("清理托管 skill 失败: %v", err)
	}

	if _, err := os.Stat(managedDir); !os.IsNotExist(err) {
		t.Fatalf("dec-* 托管 skill 应被清理")
	}
	if _, err := os.Stat(userDir); err != nil {
		t.Fatalf("用户 skill 不应被清理: %v", err)
	}
}

func TestCleanManagedRules(t *testing.T) {
	projectRoot := t.TempDir()
	svc := &SyncServiceV2{projectRoot: projectRoot}
	ideImpl := ide.Get("cursor")

	rulesDir := filepath.Join(projectRoot, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(rulesDir, "dec-my-rule.mdc"), []byte("managed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "user-rule.mdc"), []byte("user"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := svc.cleanManagedRules(ideImpl); err != nil {
		t.Fatalf("清理托管规则失败: %v", err)
	}

	if _, err := os.Stat(filepath.Join(rulesDir, "dec-my-rule.mdc")); !os.IsNotExist(err) {
		t.Fatalf("dec-* 规则应被清理")
	}
	if _, err := os.Stat(filepath.Join(rulesDir, "user-rule.mdc")); err != nil {
		t.Fatalf("用户规则不应被清理: %v", err)
	}
}

func TestSyncWithUnavailableVaultKeepsManagedAssets(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	projectRoot := t.TempDir()
	configDir := filepath.Join(projectRoot, ".dec", "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ides.yaml"), []byte("ides:\n  - cursor\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "vault.yaml"), []byte("vault_skills:\n  - reusable-skill\nvault_rules:\n  - my-rule\n"), 0644); err != nil {
		t.Fatal(err)
	}

	managedSkill := filepath.Join(projectRoot, ".cursor", "skills", "dec-reusable-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(managedSkill), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedSkill, []byte("managed-skill"), 0644); err != nil {
		t.Fatal(err)
	}

	managedRule := filepath.Join(projectRoot, ".cursor", "rules", "dec-my-rule.mdc")
	if err := os.MkdirAll(filepath.Dir(managedRule), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedRule, []byte("managed-rule"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := &SyncServiceV2{
		projectRoot: projectRoot,
		configMgr:   config.NewProjectConfigManagerV2(projectRoot),
	}

	if _, err := svc.Sync(); err == nil {
		t.Fatalf("期望 vault 不可用时 sync 返回错误")
	}

	if _, err := os.Stat(managedSkill); err != nil {
		t.Fatalf("sync 失败时不应清空已存在托管 skill: %v", err)
	}
	if _, err := os.Stat(managedRule); err != nil {
		t.Fatalf("sync 失败时不应清空已存在托管 rule: %v", err)
	}
}

func TestSyncFallsBackToLocalVaultWhenRefreshFails(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	decHome := os.Getenv("DEC_HOME")
	vaultDir := filepath.Join(decHome, "vault")
	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		t.Fatal(err)
	}
	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = vaultDir
	if output, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("初始化 vault git 仓库失败: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	gitCmd = exec.Command("git", "remote", "add", "origin", "https://invalid.example.com/nonexistent.git")
	gitCmd.Dir = vaultDir
	if output, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("设置远程仓库失败: %v (%s)", err, strings.TrimSpace(string(output)))
	}

	skillDir := filepath.Join(vaultDir, "skills", "offline-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: offline-skill\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	indexJSON := `{
  "version": "v1",
  "items": [
    {
      "name": "offline-skill",
      "type": "skill",
      "path": "skills/offline-skill",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z"
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(vaultDir, "vault.json"), []byte(indexJSON), 0644); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	configDir := filepath.Join(projectRoot, ".dec", "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ides.yaml"), []byte("ides:\n  - cursor\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "vault.yaml"), []byte("vault_skills:\n  - offline-skill\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := &SyncServiceV2{
		projectRoot: projectRoot,
		configMgr:   config.NewProjectConfigManagerV2(projectRoot),
	}

	result, err := svc.Sync()
	if err != nil {
		t.Fatalf("sync 应在 refresh 失败时回退到本地 vault: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("期望返回 refresh 回退 warning")
	}
	if !strings.Contains(result.Warnings[0], "已回退到本地缓存") {
		t.Fatalf("warning 内容不符合预期: %v", result.Warnings)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-offline-skill", "SKILL.md")); err != nil {
		t.Fatalf("应使用本地 vault 完成 skill 同步: %v", err)
	}
}

func TestSyncRestoresManagedAssetsOnSyncFailure(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	decHome := os.Getenv("DEC_HOME")
	vaultDir := filepath.Join(decHome, "vault")
	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		t.Fatal(err)
	}
	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = vaultDir
	if output, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("初始化 vault git 仓库失败: %v (%s)", err, strings.TrimSpace(string(output)))
	}

	skillDir := filepath.Join(vaultDir, "skills", "new-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: new-skill\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vaultDir, "rules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "rules", "new-rule.mdc"), []byte("# new rule\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vaultDir, "mcp"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "mcp", "broken-tool.json"), []byte(`{"args":["missing-command"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	indexJSON := `{
  "version": "v1",
  "items": [
    {
      "name": "new-skill",
      "type": "skill",
      "path": "skills/new-skill",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z"
    },
    {
      "name": "new-rule",
      "type": "rule",
      "path": "rules/new-rule.mdc",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z"
    },
    {
      "name": "broken-tool",
      "type": "mcp",
      "path": "mcp/broken-tool.json",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z"
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(vaultDir, "vault.json"), []byte(indexJSON), 0644); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	configDir := filepath.Join(projectRoot, ".dec", "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ides.yaml"), []byte("ides:\n  - cursor\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "vault.yaml"), []byte("vault_skills:\n  - new-skill\nvault_rules:\n  - new-rule\nvault_mcps:\n  - broken-tool\n"), 0644); err != nil {
		t.Fatal(err)
	}

	managedSkill := filepath.Join(projectRoot, ".cursor", "skills", "dec-old-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(managedSkill), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedSkill, []byte("old skill"), 0644); err != nil {
		t.Fatal(err)
	}
	managedRule := filepath.Join(projectRoot, ".cursor", "rules", "dec-old-rule.mdc")
	if err := os.MkdirAll(filepath.Dir(managedRule), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedRule, []byte("old rule"), 0644); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(projectRoot, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0755); err != nil {
		t.Fatal(err)
	}
	originalMCP := `{"mcpServers":{"user-helper":{"command":"npx","args":["-y","user-helper"]},"dec-old-helper":{"command":"npx","args":["-y","old-helper"]}}}`
	if err := os.WriteFile(mcpPath, []byte(originalMCP), 0644); err != nil {
		t.Fatal(err)
	}

	svc := &SyncServiceV2{
		projectRoot: projectRoot,
		configMgr:   config.NewProjectConfigManagerV2(projectRoot),
	}

	if _, err := svc.Sync(); err == nil {
		t.Fatalf("期望 sync 因损坏 MCP 失败")
	}

	data, err := os.ReadFile(managedSkill)
	if err != nil || string(data) != "old skill" {
		t.Fatalf("失败后应恢复旧 skill, err=%v content=%q", err, string(data))
	}
	data, err = os.ReadFile(managedRule)
	if err != nil || string(data) != "old rule" {
		t.Fatalf("失败后应恢复旧 rule, err=%v content=%q", err, string(data))
	}
	data, err = os.ReadFile(mcpPath)
	if err != nil || string(data) != originalMCP {
		t.Fatalf("失败后应恢复旧 MCP 配置, err=%v content=%q", err, string(data))
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-new-skill")); !os.IsNotExist(err) {
		t.Fatalf("失败后不应保留新 skill 副本")
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "rules", "dec-new-rule.mdc")); !os.IsNotExist(err) {
		t.Fatalf("失败后不应保留新 rule 副本")
	}
}

func TestSyncRemovesUndeclaredManagedMCPs(t *testing.T) {
	projectRoot := t.TempDir()
	configDir := filepath.Join(projectRoot, ".dec", "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ides.yaml"), []byte("ides:\n  - cursor\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "vault.yaml"), []byte("# empty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cursorMCPPath := filepath.Join(projectRoot, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(cursorMCPPath), 0755); err != nil {
		t.Fatal(err)
	}

	initialConfig := types.MCPConfig{
		MCPServers: map[string]types.MCPServer{
			"user-helper": {
				Command: "npx",
				Args:    []string{"-y", "user-helper"},
			},
			"dec-old-helper": {
				Command: "npx",
				Args:    []string{"-y", "old-helper"},
			},
		},
	}
	data, err := json.Marshal(initialConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursorMCPPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	svc := &SyncServiceV2{
		projectRoot: projectRoot,
		configMgr:   config.NewProjectConfigManagerV2(projectRoot),
	}

	if _, err := svc.Sync(); err != nil {
		t.Fatalf("sync 失败: %v", err)
	}

	finalData, err := os.ReadFile(cursorMCPPath)
	if err != nil {
		t.Fatal(err)
	}
	var finalConfig types.MCPConfig
	if err := json.Unmarshal(finalData, &finalConfig); err != nil {
		t.Fatal(err)
	}

	if _, ok := finalConfig.MCPServers["user-helper"]; !ok {
		t.Fatalf("用户 MCP 不应被删除")
	}
	if _, ok := finalConfig.MCPServers["dec-old-helper"]; ok {
		t.Fatalf("未声明的 dec-* MCP 应被删除")
	}
}
