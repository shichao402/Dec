package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitProject_DoesNotModifyGitignore(t *testing.T) {
	projectRoot := t.TempDir()
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	original := "node_modules/\n.cursor/\n"
	if err := os.WriteFile(gitignorePath, []byte(original), 0644); err != nil {
		t.Fatalf("写入 .gitignore 失败: %v", err)
	}

	mgr := NewProjectConfigManager(projectRoot)
	if err := mgr.InitProject("vault-a", []string{"cursor"}); err != nil {
		t.Fatalf("初始化项目失败: %v", err)
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("读取 .gitignore 失败: %v", err)
	}
	if got := string(data); got != original {
		t.Fatalf("InitProject() 不应修改 .gitignore\n得到:\n%s\n期望:\n%s", got, original)
	}
}

func TestInitProject_DoesNotCreateGitignore(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	if err := mgr.InitProject("vault-a", []string{"cursor"}); err != nil {
		t.Fatalf("初始化项目失败: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectRoot, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("InitProject() 不应创建 .gitignore, 实际错误: %v", err)
	}
}
