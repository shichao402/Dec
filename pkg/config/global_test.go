package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shichao402/Dec/pkg/types"
)

func setEnvForGlobalTest(t *testing.T, key, value string) {
	t.Helper()

	oldValue, existed := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("设置环境变量失败: %v", err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, oldValue)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestLoadGlobalConfig_MergesLegacyLocalIDEs(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	if err := os.WriteFile(filepath.Join(decHome, "config.yaml"), []byte("repo_url: https://example.com/repo.git\n"), 0644); err != nil {
		t.Fatalf("写入全局配置失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(decHome, "local"), 0755); err != nil {
		t.Fatalf("创建旧本机配置目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(decHome, "local", "config.yaml"), []byte("ides:\n  - cursor\n  - codebuddy\n"), 0644); err != nil {
		t.Fatalf("写入旧本机配置失败: %v", err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() 失败: %v", err)
	}

	if cfg.RepoURL != "https://example.com/repo.git" {
		t.Fatalf("RepoURL = %q, 期望 %q", cfg.RepoURL, "https://example.com/repo.git")
	}
	wantIDEs := []string{"cursor", "codebuddy"}
	if !reflect.DeepEqual(cfg.IDEs, wantIDEs) {
		t.Fatalf("IDEs = %#v, 期望 %#v", cfg.IDEs, wantIDEs)
	}
}

func TestSaveGlobalConfig_RemovesLegacyLocalConfig(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	legacyDir := filepath.Join(decHome, "local")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("创建旧本机配置目录失败: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, "config.yaml")
	if err := os.WriteFile(legacyPath, []byte("ides:\n  - old-ide\n"), 0644); err != nil {
		t.Fatalf("写入旧本机配置失败: %v", err)
	}

	cfg := &types.GlobalConfig{
		RepoURL: "https://example.com/repo.git",
		IDEs:    []string{"cursor", "windsurf"},
	}
	if err := SaveGlobalConfig(cfg); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	globalPath := filepath.Join(decHome, "config.yaml")
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("读取全局配置失败: %v", err)
	}
	if string(data) == "" {
		t.Fatalf("全局配置不应为空")
	}

	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("旧本机配置应被清理, 实际错误: %v", err)
	}

	loaded, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("重新加载全局配置失败: %v", err)
	}
	if loaded.RepoURL != cfg.RepoURL {
		t.Fatalf("RepoURL = %q, 期望 %q", loaded.RepoURL, cfg.RepoURL)
	}
	if !reflect.DeepEqual(loaded.IDEs, cfg.IDEs) {
		t.Fatalf("IDEs = %#v, 期望 %#v", loaded.IDEs, cfg.IDEs)
	}
}

func TestGetEffectiveIDEs_PrefersProjectThenGlobalThenDefault(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	got, err := GetEffectiveIDEs(&types.ProjectConfig{IDEs: []string{"claude"}})
	if err != nil {
		t.Fatalf("GetEffectiveIDEs() 返回错误: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("项目覆盖 IDE = %#v, 期望 %#v", got, []string{"claude"})
	}

	if err := SaveGlobalConfig(&types.GlobalConfig{IDEs: []string{"cursor", "codebuddy"}}); err != nil {
		t.Fatalf("写入全局 IDE 配置失败: %v", err)
	}
	got, err = GetEffectiveIDEs(&types.ProjectConfig{})
	if err != nil {
		t.Fatalf("GetEffectiveIDEs() 返回错误: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"cursor", "codebuddy"}) {
		t.Fatalf("全局 IDE = %#v, 期望 %#v", got, []string{"cursor", "codebuddy"})
	}

	if err := os.Remove(filepath.Join(decHome, "config.yaml")); err != nil {
		t.Fatalf("删除全局配置失败: %v", err)
	}
	got, err = GetEffectiveIDEs(&types.ProjectConfig{})
	if err != nil {
		t.Fatalf("GetEffectiveIDEs() 返回错误: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"cursor"}) {
		t.Fatalf("默认 IDE = %#v, 期望 %#v", got, []string{"cursor"})
	}
}
