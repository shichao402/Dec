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

func TestInstallDecSkillForCodexInternalUsesInternalUserDir(t *testing.T) {
	homeDir := t.TempDir()
	setHomeForConfigTest(t, homeDir)

	if err := installDecSkillForIDE("codex-internal"); err != nil {
		t.Fatalf("安装 codex-internal 全局 Skill 失败: %v", err)
	}

	internalSkill := filepath.Join(homeDir, ".codex-internal", "skills", "dec", "SKILL.md")
	if _, err := os.Stat(internalSkill); err != nil {
		t.Fatalf("应写入 ~/.codex-internal/skills/dec/SKILL.md: %v", err)
	}

	wrongSkill := filepath.Join(homeDir, ".codex", "skills", "dec", "SKILL.md")
	if _, err := os.Stat(wrongSkill); !os.IsNotExist(err) {
		t.Fatalf("codex-internal 的用户级 Skill 不应写到 ~/.codex: %v", err)
	}
}

func TestInstallDecSkillForClaudeInternalUsesInternalUserDir(t *testing.T) {
	homeDir := t.TempDir()
	setHomeForConfigTest(t, homeDir)

	if err := installDecSkillForIDE("claude-internal"); err != nil {
		t.Fatalf("安装 claude-internal 全局 Skill 失败: %v", err)
	}

	internalSkill := filepath.Join(homeDir, ".claude-internal", "skills", "dec", "SKILL.md")
	if _, err := os.Stat(internalSkill); err != nil {
		t.Fatalf("应写入 ~/.claude-internal/skills/dec/SKILL.md: %v", err)
	}

	wrongSkill := filepath.Join(homeDir, ".claude", "skills", "dec", "SKILL.md")
	if _, err := os.Stat(wrongSkill); !os.IsNotExist(err) {
		t.Fatalf("claude-internal 的用户级 Skill 不应写到 ~/.claude: %v", err)
	}
}
