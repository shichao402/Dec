package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/editor"
	"github.com/shichao402/Dec/internal/types"
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

// ADR 0009：用户平面启用列表从 ~/.dec/secrets/config.yaml 迁到 GlobalConfig.EnabledBundles。
func TestLoadGlobalConfig_MergesLegacySecretsEnabledBundles(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)
	writeLegacySecretsConfig(t, decHome, "server_url: https://vault.bitwarden.com\nuser_enabled_bundles:\n  - bundle/tencent-cloud\n  - woa\n  - woa\n")

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() 失败: %v", err)
	}
	want := []string{"tencent-cloud", "woa"}
	if !reflect.DeepEqual(cfg.EnabledBundles, want) {
		t.Fatalf("EnabledBundles = %#v, 期望 %#v", cfg.EnabledBundles, want)
	}
}

func TestLoadGlobalConfig_PrefersOwnEnabledBundles(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)
	writeLegacySecretsConfig(t, decHome, "user_enabled_bundles:\n  - legacy\n")
	if err := os.WriteFile(filepath.Join(decHome, "config.yaml"), []byte("enabled_bundles:\n  - current\n"), 0644); err != nil {
		t.Fatalf("写入全局配置失败: %v", err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() 失败: %v", err)
	}
	if !reflect.DeepEqual(cfg.EnabledBundles, []string{"current"}) {
		t.Fatalf("EnabledBundles = %#v, 期望 [current]", cfg.EnabledBundles)
	}
}

func TestSaveGlobalConfig_ClearsLegacySecretsEnabledBundles(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)
	legacyPath := writeLegacySecretsConfig(t, decHome,
		"server_url: https://vault.bitwarden.com\nemail: me@example.com\nuser_enabled_bundles:\n  - woa\nknown_secret_bundles:\n  - woa\n")

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() 失败: %v", err)
	}
	if err := SaveGlobalConfig(cfg); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	legacyData, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("读取旧 secrets 配置失败: %v", err)
	}
	if strings.Contains(string(legacyData), "user_enabled_bundles") {
		t.Fatalf("旧 secrets 配置应清理 user_enabled_bundles, 实际:\n%s", legacyData)
	}
	for _, keep := range []string{"me@example.com", "known_secret_bundles"} {
		if !strings.Contains(string(legacyData), keep) {
			t.Fatalf("旧 secrets 配置应保留 %q, 实际:\n%s", keep, legacyData)
		}
	}

	globalData, err := os.ReadFile(filepath.Join(decHome, "config.yaml"))
	if err != nil {
		t.Fatalf("读取全局配置失败: %v", err)
	}
	if !strings.Contains(string(globalData), "enabled_bundles:") {
		t.Fatalf("全局配置应写入 enabled_bundles, 实际:\n%s", globalData)
	}

	// 迁移后重新加载不应再依赖旧位置。
	names, err := UserEnabledBundles()
	if err != nil {
		t.Fatalf("UserEnabledBundles() 失败: %v", err)
	}
	if !reflect.DeepEqual(names, []string{"woa"}) {
		t.Fatalf("UserEnabledBundles() = %#v, 期望 [woa]", names)
	}
}

func TestNormalizeBundleNames_TrimsPrefixAndDuplicates(t *testing.T) {
	got := NormalizeBundleNames([]string{" woa ", "bundle/vikunja", "woa", "", "vikunja"})
	if !reflect.DeepEqual(got, []string{"woa", "vikunja"}) {
		t.Fatalf("NormalizeBundleNames = %#v", got)
	}
}

func writeLegacySecretsConfig(t *testing.T, decHome, content string) string {
	t.Helper()

	dir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建旧 secrets 目录失败: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("写入旧 secrets 配置失败: %v", err)
	}
	return path
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

func TestGetEffectiveIDEs_IgnoresRemovedIDE(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	got, err := GetEffectiveIDEs(&types.ProjectConfig{IDEs: []string{"cursor", "windsurf", "cursor", "trae"}})
	if err != nil {
		t.Fatalf("GetEffectiveIDEs() 返回错误: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"cursor"}) {
		t.Fatalf("过滤后的 IDE = %#v, 期望 %#v", got, []string{"cursor"})
	}

	if err := SaveGlobalConfig(&types.GlobalConfig{IDEs: []string{"trae", "claude", "trae"}}); err != nil {
		t.Fatalf("写入全局 IDE 配置失败: %v", err)
	}
	got, err = GetEffectiveIDEs(&types.ProjectConfig{})
	if err != nil {
		t.Fatalf("GetEffectiveIDEs() 返回错误: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("全局过滤后的 IDE = %#v, 期望 %#v", got, []string{"claude"})
	}

	if err := SaveGlobalConfig(&types.GlobalConfig{IDEs: []string{"trae"}}); err != nil {
		t.Fatalf("写入全局 IDE 配置失败: %v", err)
	}
	got, err = GetEffectiveIDEs(&types.ProjectConfig{})
	if err != nil {
		t.Fatalf("GetEffectiveIDEs() 返回错误: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"cursor"}) {
		t.Fatalf("仅剩已移除 IDE 时应回退默认 cursor, 实际 %#v", got)
	}
}

func TestResolveEffectiveIDEs_WarnsWhenProjectContainsRemovedIDE(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	selection, err := ResolveEffectiveIDEs(&types.ProjectConfig{IDEs: []string{"cursor", "windsurf", "trae"}})
	if err != nil {
		t.Fatalf("ResolveEffectiveIDEs() 返回错误: %v", err)
	}
	if !reflect.DeepEqual(selection.IDEs, []string{"cursor"}) {
		t.Fatalf("过滤后的 IDE = %#v, 期望 %#v", selection.IDEs, []string{"cursor"})
	}
	if len(selection.Warnings) != 1 {
		t.Fatalf("应返回 1 条警告，得到 %#v", selection.Warnings)
	}
	if !strings.Contains(selection.Warnings[0], "windsurf") || !strings.Contains(selection.Warnings[0], "trae") {
		t.Fatalf("警告应包含已移除的 IDE 名称, 实际: %v", selection.Warnings)
	}
}

func TestResolveEffectiveIDEs_WarnsWhenProjectFallsBackToGlobal(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	if err := SaveGlobalConfig(&types.GlobalConfig{IDEs: []string{"claude"}}); err != nil {
		t.Fatalf("写入全局 IDE 配置失败: %v", err)
	}

	selection, err := ResolveEffectiveIDEs(&types.ProjectConfig{IDEs: []string{"windsurf"}})
	if err != nil {
		t.Fatalf("ResolveEffectiveIDEs() 返回错误: %v", err)
	}
	if !reflect.DeepEqual(selection.IDEs, []string{"claude"}) {
		t.Fatalf("回退后的 IDE = %#v, 期望 %#v", selection.IDEs, []string{"claude"})
	}
	if len(selection.Warnings) != 1 {
		t.Fatalf("应返回 1 条警告，得到 %#v", selection.Warnings)
	}
	if !strings.Contains(selection.Warnings[0], "windsurf") || !strings.Contains(selection.Warnings[0], "将回退到全局配置") {
		t.Fatalf("警告应包含 windsurf 与全局回退说明, 实际: %v", selection.Warnings)
	}
}

func TestResolveEffectiveIDEs_WarnsWhenGlobalFallsBackToDefault(t *testing.T) {
	decHome := t.TempDir()
	setEnvForGlobalTest(t, "DEC_HOME", decHome)

	if err := SaveGlobalConfig(&types.GlobalConfig{IDEs: []string{"trae"}}); err != nil {
		t.Fatalf("写入全局 IDE 配置失败: %v", err)
	}

	selection, err := ResolveEffectiveIDEs(&types.ProjectConfig{})
	if err != nil {
		t.Fatalf("ResolveEffectiveIDEs() 返回错误: %v", err)
	}
	if !reflect.DeepEqual(selection.IDEs, []string{"cursor"}) {
		t.Fatalf("默认 IDE = %#v, 期望 %#v", selection.IDEs, []string{"cursor"})
	}
	if len(selection.Warnings) != 1 {
		t.Fatalf("应返回 1 条警告，得到 %#v", selection.Warnings)
	}
	if !strings.Contains(selection.Warnings[0], "trae") || !strings.Contains(selection.Warnings[0], "将回退到默认 IDE cursor") {
		t.Fatalf("警告应包含 trae 与默认回退说明, 实际: %v", selection.Warnings)
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
