package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProjectIDEs(t *testing.T) {
	projectRoot := t.TempDir()
	configDir := filepath.Join(projectRoot, ".dec", "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("创建配置目录失败: %v", err)
	}

	idesConfig := "ides:\n  - cursor\n  - windsurf\n  - codebuddy\n"
	if err := os.WriteFile(filepath.Join(configDir, "ides.yaml"), []byte(idesConfig), 0644); err != nil {
		t.Fatalf("写入 IDE 配置失败: %v", err)
	}

	ideNames := resolveProjectIDEs(projectRoot)
	expected := []string{"cursor", "windsurf", "codebuddy"}

	if len(ideNames) != len(expected) {
		t.Fatalf("IDE 数量错误: 期望 %d, 得到 %d", len(expected), len(ideNames))
	}

	for i := range expected {
		if ideNames[i] != expected[i] {
			t.Fatalf("IDE 名称错误: 期望 %s, 得到 %s", expected[i], ideNames[i])
		}
	}
}

func TestRunVaultPullAddsItemToVaultConfig(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	vaultDir := filepath.Join(decHome, "vault")
	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		t.Fatalf("创建 vault 目录失败: %v", err)
	}
	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = vaultDir
	if output, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("初始化 vault git 仓库失败: %v (%s)", err, strings.TrimSpace(string(output)))
	}

	skillDir := filepath.Join(vaultDir, "skills", "create-api-test")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("创建 vault skill 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: create-api-test\n---\n"), 0644); err != nil {
		t.Fatalf("写入 vault skill 失败: %v", err)
	}

	indexJSON := `{
  "version": "v1",
  "items": [
    {
      "name": "create-api-test",
      "type": "skill",
      "path": "skills/create-api-test",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z"
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(vaultDir, "vault.json"), []byte(indexJSON), 0644); err != nil {
		t.Fatalf("写入 vault 索引失败: %v", err)
	}

	projectRoot := t.TempDir()
	configDir := filepath.Join(projectRoot, ".dec", "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("创建项目配置目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ides.yaml"), []byte("ides:\n  - cursor\n  - windsurf\n"), 0644); err != nil {
		t.Fatalf("写入 IDE 配置失败: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取当前目录失败: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWD)
	}()
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("切换项目目录失败: %v", err)
	}

	if err := runVaultPull(nil, []string{"skill", "create-api-test"}); err != nil {
		t.Fatalf("runVaultPull 失败: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "skills", "dec-create-api-test", "SKILL.md")); err != nil {
		t.Fatalf("Cursor skill 未同步: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".windsurf", "skills", "dec-create-api-test", "SKILL.md")); err != nil {
		t.Fatalf("Windsurf skill 未同步: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(configDir, "vault.yaml"))
	if err != nil {
		t.Fatalf("读取 vault.yaml 失败: %v", err)
	}
	if !strings.Contains(string(data), "vault_skills:\n  - create-api-test") {
		t.Fatalf("vault.yaml 未自动声明 pulled skill:\n%s", string(data))
	}
}
