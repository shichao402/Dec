package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitProject_DoesNotDuplicateGitignoreEntry(t *testing.T) {
	projectRoot := t.TempDir()
	mgr := NewProjectConfigManager(projectRoot)

	if err := mgr.InitProject("vault-a", []string{"cursor"}); err != nil {
		t.Fatalf("首次初始化项目失败: %v", err)
	}
	if err := mgr.InitProject("vault-a", []string{"cursor"}); err != nil {
		t.Fatalf("再次初始化项目失败: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, ".gitignore"))
	if err != nil {
		t.Fatalf("读取 .gitignore 失败: %v", err)
	}
	if got := strings.Count(string(data), ".dec/"); got != 1 {
		t.Fatalf(".gitignore 中 .dec/ 应只出现一次，实际为 %d 次:\n%s", got, string(data))
	}
}

func TestInitProject_RecognizesTrimmedGitignoreEntry(t *testing.T) {
	projectRoot := t.TempDir()
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("node_modules/\n   .dec/   \n"), 0644); err != nil {
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
	if got := strings.Count(string(data), ".dec/"); got != 1 {
		t.Fatalf("带空格的 .dec/ 规则应被识别，实际为 %d 次:\n%s", got, string(data))
	}
}
