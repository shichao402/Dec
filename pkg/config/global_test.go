package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/editor"
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
		IDEs:    []string{"cursor", "claude"},
		Editor:  "vim",
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
	if !strings.Contains(string(data), "#   ides:") || !strings.Contains(string(data), "#   editor: code --wait") {
		t.Fatalf("全局配置头注释应包含 ides/editor 示例, 实际内容:\n%s", string(data))
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
	if loaded.Editor != cfg.Editor {
		t.Fatalf("Editor = %q, 期望 %q", loaded.Editor, cfg.Editor)
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

func TestGetEffectiveIDEs_RejectsRemovedIDE(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	if _, err := GetEffectiveIDEs(&types.ProjectConfig{IDEs: []string{"windsurf"}}); err == nil {
		t.Fatal("项目级配置已移除的 IDE 时应返回错误")
	} else if !strings.Contains(err.Error(), "windsurf") {
		t.Fatalf("错误信息应包含 windsurf, 实际: %v", err)
	}

	if err := SaveGlobalConfig(&types.GlobalConfig{IDEs: []string{"trae"}}); err != nil {
		t.Fatalf("写入全局 IDE 配置失败: %v", err)
	}

	if _, err := GetEffectiveIDEs(&types.ProjectConfig{}); err == nil {
		t.Fatal("全局配置已移除的 IDE 时应返回错误")
	} else if !strings.Contains(err.Error(), "trae") {
		t.Fatalf("错误信息应包含 trae, 实际: %v", err)
	}
}

func TestGetEffectiveIDEs_AllowsCustomFallbackIDE(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	got, err := GetEffectiveIDEs(&types.ProjectConfig{IDEs: []string{"my-custom-ide"}})
	if err != nil {
		t.Fatalf("自定义 fallback IDE 不应报错: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"my-custom-ide"}) {
		t.Fatalf("自定义 fallback IDE = %#v, 期望 %#v", got, []string{"my-custom-ide"})
	}
}

func TestGetEffectiveEditor_PrefersProjectThenGlobalThenDefault(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	got, err := GetEffectiveEditor(&types.ProjectConfig{Editor: "vi"})
	if err != nil {
		t.Fatalf("GetEffectiveEditor() 返回错误: %v", err)
	}
	if got != "vi" {
		t.Fatalf("项目覆盖编辑器 = %q, 期望 %q", got, "vi")
	}

	if err := SaveGlobalConfig(&types.GlobalConfig{Editor: "vim"}); err != nil {
		t.Fatalf("写入全局编辑器配置失败: %v", err)
	}
	got, err = GetEffectiveEditor(&types.ProjectConfig{})
	if err != nil {
		t.Fatalf("GetEffectiveEditor() 返回错误: %v", err)
	}
	if got != "vim" {
		t.Fatalf("全局编辑器 = %q, 期望 %q", got, "vim")
	}

	if err := os.Remove(filepath.Join(decHome, "config.yaml")); err != nil {
		t.Fatalf("删除全局配置失败: %v", err)
	}
	got, err = GetEffectiveEditor(&types.ProjectConfig{})
	if err != nil {
		t.Fatalf("GetEffectiveEditor() 返回错误: %v", err)
	}
	if got != editor.DefaultCommand() {
		t.Fatalf("默认编辑器 = %q, 期望 %q", got, editor.DefaultCommand())
	}
}

func TestEnsureGlobalVarsTemplate_CreatesDefaultFile(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	created, err := EnsureGlobalVarsTemplate()
	if err != nil {
		t.Fatalf("EnsureGlobalVarsTemplate() 失败: %v", err)
	}
	if !created {
		t.Fatal("首次调用应创建 vars.yaml")
	}

	varsPath, err := GetGlobalVarsPath()
	if err != nil {
		t.Fatalf("GetGlobalVarsPath() 失败: %v", err)
	}
	data, err := os.ReadFile(varsPath)
	if err != nil {
		t.Fatalf("读取全局 vars.yaml 失败: %v", err)
	}
	content := string(data)
	if content == "" {
		t.Fatal("全局 vars.yaml 不应为空")
	}
	for _, part := range []string{"vars:", "assets:", "{{VAR_NAME}}", "skill:", "rule:", "mcp:"} {
		if !strings.Contains(content, part) {
			t.Fatalf("全局 vars.yaml 模板缺少 %q: %q", part, content)
		}
	}

	created, err = EnsureGlobalVarsTemplate()
	if err != nil {
		t.Fatalf("EnsureGlobalVarsTemplate() 二次调用失败: %v", err)
	}
	if created {
		t.Fatal("已有全局 vars.yaml 时不应重复创建")
	}
}
