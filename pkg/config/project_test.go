package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func TestSaveAndLoadProjectConfig(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	cfg := &types.ProjectConfig{
		IDEs:   []string{"cursor"},
		Editor: "vim",
		Available: &types.AssetList{
			Rules: []types.AssetRef{
				{Name: "rule-a", Vault: "v1"},
				{Name: "rule-b", Vault: "v2"},
			},
		},
		Enabled: &types.AssetList{
			Rules: []types.AssetRef{
				{Name: "rule-a", Vault: "v1"},
			},
		},
	}

	if err := mgr.SaveProjectConfig(cfg); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(projectRoot, ".dec", "config.yaml"))
	if err != nil {
		t.Fatalf("读取保存后的配置失败: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "#   ides:") || !strings.Contains(content, "#   editor: code --wait") {
		t.Fatalf("项目配置头注释应包含 ides/editor 示例, 实际内容:\n%s", content)
	}
	if !strings.Contains(content, "version: v2") {
		t.Fatalf("保存后的配置应写入 version: v2, 实际内容:\n%s", content)
	}
	if !strings.Contains(content, "v1:") || !strings.Contains(content, "rule-a:") || !strings.Contains(content, "rules: true") {
		t.Fatalf("保存后的配置应使用 v2 的 vault/item/type 结构, 实际内容:\n%s", content)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	if loaded.Available.Count() != 2 {
		t.Fatalf("available 应有 2 个, 得到 %d", loaded.Available.Count())
	}
	if loaded.Enabled.Count() != 1 {
		t.Fatalf("enabled 应有 1 个, 得到 %d", loaded.Enabled.Count())
	}
	if loaded.Editor != "vim" {
		t.Fatalf("editor = %q, 期望 %q", loaded.Editor, "vim")
	}
	if loaded.IDEs[0] != "cursor" {
		t.Fatalf("ides[0] = %q, 期望 %q", loaded.IDEs[0], "cursor")
	}
	if loaded.Version != types.ProjectConfigVersionV2 {
		t.Fatalf("version = %q, 期望 %q", loaded.Version, types.ProjectConfigVersionV2)
	}
}

func TestLoadProjectConfig_Dedup(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	// 手写一个有重复的配置
	decDir := filepath.Join(projectRoot, ".dec")
	os.MkdirAll(decDir, 0755)
	content := `
version: v2
enabled:
  v1:
    rule-a:
      rules: true
      rules: true
`
	os.WriteFile(filepath.Join(decDir, "config.yaml"), []byte(content), 0644)

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	// v2 同一 vault/item/type 重复声明后应只保留 1 个
	if loaded.Enabled.Count() != 1 {
		t.Fatalf("去重后应有 1 个, 得到 %d", loaded.Enabled.Count())
	}
	if loaded.Enabled.Rules[0].Vault != "v1" {
		t.Fatalf("vault = %s, 期望 v1", loaded.Enabled.Rules[0].Vault)
	}
}

func TestLoadProjectConfig_MigratesV1ToV2(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	decDir := filepath.Join(projectRoot, ".dec")
	if err := os.MkdirAll(decDir, 0755); err != nil {
		t.Fatalf("创建 .dec 目录失败: %v", err)
	}

	legacy := `
ides:
  - cursor
editor: vim
available:
  rules:
    - name: shared-rule
      vault: team
  mcps:
    - name: postgres
      vault: infra
enabled:
  rules:
    - name: shared-rule
      vault: team
`
	configPath := filepath.Join(decDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(legacy), 0644); err != nil {
		t.Fatalf("写入 v1 配置失败: %v", err)
	}

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("加载并迁移 v1 配置失败: %v", err)
	}

	if loaded.Version != types.ProjectConfigVersionV2 {
		t.Fatalf("version = %q, 期望 %q", loaded.Version, types.ProjectConfigVersionV2)
	}
	if loaded.Editor != "vim" {
		t.Fatalf("editor = %q, 期望 vim", loaded.Editor)
	}
	if len(loaded.IDEs) != 1 || loaded.IDEs[0] != "cursor" {
		t.Fatalf("ides = %#v, 期望 [cursor]", loaded.IDEs)
	}
	if loaded.Available.FindAsset("rule", "shared-rule", "team") == nil {
		t.Fatal("迁移后应保留 available 中的 rule")
	}
	if loaded.Available.FindAsset("mcp", "postgres", "infra") == nil {
		t.Fatal("迁移后应保留 available 中的 mcp")
	}
	if loaded.Enabled.FindAsset("rule", "shared-rule", "team") == nil {
		t.Fatal("迁移后应保留 enabled 中的 rule")
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取迁移后的配置失败: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "version: v2") {
		t.Fatalf("迁移后配置应写入 version: v2, 实际内容:\n%s", content)
	}
	if !strings.Contains(content, "team:") || !strings.Contains(content, "shared-rule:") || !strings.Contains(content, "rules: true") {
		t.Fatalf("迁移后配置应使用 v2 结构, 实际内容:\n%s", content)
	}
	if strings.Contains(content, "- name:") {
		t.Fatalf("迁移后不应保留 v1 列表结构, 实际内容:\n%s", content)
	}
}

func TestSaveProjectConfig_DoesNotModifyGitignore(t *testing.T) {
	projectRoot := t.TempDir()
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	original := "node_modules/\n.cursor/\n"
	os.WriteFile(gitignorePath, []byte(original), 0644)

	mgr := NewProjectConfigManager(projectRoot)
	cfg := &types.ProjectConfig{}
	if err := mgr.SaveProjectConfig(cfg); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	data, _ := os.ReadFile(gitignorePath)
	if string(data) != original {
		t.Fatalf("不应修改 .gitignore")
	}
}

func TestEnsureVarsConfigTemplate_CreatesDefaultFile(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	created, err := mgr.EnsureVarsConfigTemplate()
	if err != nil {
		t.Fatalf("EnsureVarsConfigTemplate() 失败: %v", err)
	}
	if !created {
		t.Fatal("首次调用应创建 vars.yaml")
	}

	data, err := os.ReadFile(mgr.GetVarsPath())
	if err != nil {
		t.Fatalf("读取 vars.yaml 失败: %v", err)
	}
	content := string(data)
	if content == "" {
		t.Fatal("vars.yaml 不应为空")
	}
	if !containsAll(content, []string{"vars:", "assets:", "{{VAR_NAME}}", "skill:", "rule:", "mcp:"}) {
		t.Fatalf("vars.yaml 模板内容不完整: %q", content)
	}

	created, err = mgr.EnsureVarsConfigTemplate()
	if err != nil {
		t.Fatalf("EnsureVarsConfigTemplate() 二次调用失败: %v", err)
	}
	if created {
		t.Fatal("已有 vars.yaml 时不应重复创建")
	}
}

func containsAll(content string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(content, part) {
			return false
		}
	}
	return true
}
