package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func setHomeForConfigTest(t *testing.T, home string) {
	t.Helper()

	oldHome, hadHome := os.LookupEnv("HOME")
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("设置 HOME 失败: %v", err)
	}
	t.Cleanup(func() {
		if hadHome {
			_ = os.Setenv("HOME", oldHome)
		} else {
			_ = os.Unsetenv("HOME")
		}
	})
}

func TestInstallBuiltinAssetsForCodexInternalUsesInternalUserDir(t *testing.T) {
	homeDir := t.TempDir()
	setHomeForConfigTest(t, homeDir)

	if err := installBuiltinAssetsForIDE("codex-internal"); err != nil {
		t.Fatalf("安装 codex-internal 内置资产失败: %v", err)
	}

	for _, skillName := range []string{"dec", "dec-extract-asset"} {
		internalSkill := filepath.Join(homeDir, ".codex-internal", "skills", skillName, "SKILL.md")
		if _, err := os.Stat(internalSkill); err != nil {
			t.Fatalf("应写入 ~/.codex-internal/skills/%s/SKILL.md: %v", skillName, err)
		}
	}

	wrongSkill := filepath.Join(homeDir, ".codex", "skills", "dec", "SKILL.md")
	if _, err := os.Stat(wrongSkill); !os.IsNotExist(err) {
		t.Fatalf("codex-internal 的用户级内置 skill 不应写到 ~/.codex: %v", err)
	}
}

func TestInstallBuiltinAssetsForClaudeInternalUsesInternalUserDir(t *testing.T) {
	homeDir := t.TempDir()
	setHomeForConfigTest(t, homeDir)

	if err := installBuiltinAssetsForIDE("claude-internal"); err != nil {
		t.Fatalf("安装 claude-internal 内置资产失败: %v", err)
	}

	for _, skillName := range []string{"dec", "dec-extract-asset"} {
		internalSkill := filepath.Join(homeDir, ".claude-internal", "skills", skillName, "SKILL.md")
		if _, err := os.Stat(internalSkill); err != nil {
			t.Fatalf("应写入 ~/.claude-internal/skills/%s/SKILL.md: %v", skillName, err)
		}
	}

	wrongSkill := filepath.Join(homeDir, ".claude", "skills", "dec", "SKILL.md")
	if _, err := os.Stat(wrongSkill); !os.IsNotExist(err) {
		t.Fatalf("claude-internal 的用户级内置 skill 不应写到 ~/.claude: %v", err)
	}
}

func TestInstallBuiltinAssetsReplacesSkillDirectory(t *testing.T) {
	homeDir := t.TempDir()
	setHomeForConfigTest(t, homeDir)

	staleFile := filepath.Join(homeDir, ".cursor", "skills", "dec", "stale.txt")
	if err := os.MkdirAll(filepath.Dir(staleFile), 0755); err != nil {
		t.Fatalf("创建旧目录失败: %v", err)
	}
	if err := os.WriteFile(staleFile, []byte("old"), 0644); err != nil {
		t.Fatalf("写入旧文件失败: %v", err)
	}

	if err := installBuiltinAssetsForIDE("cursor"); err != nil {
		t.Fatalf("安装 cursor 内置资产失败: %v", err)
	}

	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatalf("重装内置 skill 时应清理旧文件: %v", err)
	}
}
