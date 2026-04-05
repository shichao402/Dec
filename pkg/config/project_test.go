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
}

func TestLoadProjectConfig_Dedup(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	// 手写一个有重复的配置
	decDir := filepath.Join(projectRoot, ".dec")
	os.MkdirAll(decDir, 0755)
	content := `
enabled:
  rules:
    - name: rule-a
      vault: v1
    - name: rule-a
      vault: v2
`
	os.WriteFile(filepath.Join(decDir, "config.yaml"), []byte(content), 0644)

	loaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	// 去重后应只有 1 个，以靠后的为准
	if loaded.Enabled.Count() != 1 {
		t.Fatalf("去重后应有 1 个, 得到 %d", loaded.Enabled.Count())
	}
	if loaded.Enabled.Rules[0].Vault != "v2" {
		t.Fatalf("应以靠后的为准, 得到 vault=%s", loaded.Enabled.Rules[0].Vault)
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
