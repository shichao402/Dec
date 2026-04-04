package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func TestSaveAndLoadProjectConfig(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	cfg := &types.ProjectConfig{
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
