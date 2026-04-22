package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/types"
)

// helper: 初始化一个干净的 DEC_HOME + 项目根目录，避免 ~/.dec 污染。
func setupProjectSettingsEnv(t *testing.T) (projectRoot string) {
	t.Helper()
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	return t.TempDir()
}

func TestLoadProjectSettings_NoConfig_InheritsGlobal(t *testing.T) {
	projectRoot := setupProjectSettingsEnv(t)

	if err := config.SaveGlobalConfig(&types.GlobalConfig{IDEs: []string{"cursor", "codex"}}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	state, err := LoadProjectSettings(projectRoot, nil)
	if err != nil {
		t.Fatalf("LoadProjectSettings() 失败: %v", err)
	}
	if state.ProjectConfigReady {
		t.Fatal("无 .dec/config.yaml 时 ProjectConfigReady 应为 false")
	}
	if state.OverrideActive {
		t.Fatal("无项目配置时不应处于覆盖状态")
	}
	if len(state.SelectedIDEs) != 0 {
		t.Fatalf("SelectedIDEs = %#v, 期望空", state.SelectedIDEs)
	}
	if len(state.GlobalIDEs) != 2 || state.GlobalIDEs[0] != "cursor" || state.GlobalIDEs[1] != "codex" {
		t.Fatalf("GlobalIDEs = %#v, 期望 [cursor codex]", state.GlobalIDEs)
	}
	if len(state.EffectiveIDEs) != 2 || state.EffectiveIDEs[0] != "cursor" || state.EffectiveIDEs[1] != "codex" {
		t.Fatalf("EffectiveIDEs = %#v, 期望回落到全局 [cursor codex]", state.EffectiveIDEs)
	}
	if len(state.AvailableIDEs) == 0 {
		t.Fatal("AvailableIDEs 不应为空")
	}
}

func TestLoadProjectSettings_WithOverride(t *testing.T) {
	projectRoot := setupProjectSettingsEnv(t)

	if err := config.SaveGlobalConfig(&types.GlobalConfig{IDEs: []string{"cursor"}}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs: []string{"codex", "claude"},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	state, err := LoadProjectSettings(projectRoot, nil)
	if err != nil {
		t.Fatalf("LoadProjectSettings() 失败: %v", err)
	}
	if !state.ProjectConfigReady {
		t.Fatal("存在 .dec/config.yaml 时 ProjectConfigReady 应为 true")
	}
	if !state.OverrideActive {
		t.Fatal("项目有 IDEs 时应处于覆盖状态")
	}
	if len(state.SelectedIDEs) != 2 || state.SelectedIDEs[0] != "codex" || state.SelectedIDEs[1] != "claude" {
		t.Fatalf("SelectedIDEs = %#v, 期望 [codex claude]", state.SelectedIDEs)
	}
	if len(state.EffectiveIDEs) != 2 || state.EffectiveIDEs[0] != "codex" || state.EffectiveIDEs[1] != "claude" {
		t.Fatalf("EffectiveIDEs = %#v, 期望使用项目级覆盖", state.EffectiveIDEs)
	}
	if len(state.GlobalIDEs) != 1 || state.GlobalIDEs[0] != "cursor" {
		t.Fatalf("GlobalIDEs = %#v, 期望 [cursor]", state.GlobalIDEs)
	}
}

func TestSaveProjectSettings_WritesIDEsAndPreservesOtherFields(t *testing.T) {
	projectRoot := setupProjectSettingsEnv(t)

	mgr := config.NewProjectConfigManager(projectRoot)
	// 先写入带 editor / enabled 的配置，确保 save IDEs 不清掉这些字段。
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		Editor: "vim",
		Enabled: &types.AssetList{
			Skills: []types.AssetRef{{Vault: "v", Name: "n"}},
		},
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := SaveProjectSettings(SaveProjectSettingsInput{
		ProjectRoot: projectRoot,
		IDEs:        []string{"cursor", "codex"},
	}, nil)
	if err != nil {
		t.Fatalf("SaveProjectSettings() 失败: %v", err)
	}
	if !result.OverrideActive {
		t.Fatal("保存非空 IDEs 后应为覆盖状态")
	}
	if len(result.SelectedIDEs) != 2 {
		t.Fatalf("SelectedIDEs = %#v, 期望 2 项", result.SelectedIDEs)
	}

	reloaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if len(reloaded.IDEs) != 2 || reloaded.IDEs[0] != "cursor" || reloaded.IDEs[1] != "codex" {
		t.Fatalf("reloaded.IDEs = %#v, 期望 [cursor codex]", reloaded.IDEs)
	}
	if reloaded.Editor != "vim" {
		t.Fatalf("Editor 字段应保留, 得到 %q", reloaded.Editor)
	}
	if reloaded.Enabled == nil || len(reloaded.Enabled.Skills) != 1 {
		t.Fatal("Enabled 资产应保留")
	}
}

func TestSaveProjectSettings_ClearOverrideRemovesField(t *testing.T) {
	projectRoot := setupProjectSettingsEnv(t)

	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:   []string{"cursor", "codex"},
		Editor: "vim",
	}); err != nil {
		t.Fatalf("SaveProjectConfig() 失败: %v", err)
	}

	result, err := SaveProjectSettings(SaveProjectSettingsInput{
		ProjectRoot:   projectRoot,
		ClearOverride: true,
	}, nil)
	if err != nil {
		t.Fatalf("SaveProjectSettings(ClearOverride) 失败: %v", err)
	}
	if result.OverrideActive {
		t.Fatal("清除覆盖后 OverrideActive 应为 false")
	}
	if len(result.SelectedIDEs) != 0 {
		t.Fatalf("SelectedIDEs = %#v, 期望为空", result.SelectedIDEs)
	}

	reloaded, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig() 失败: %v", err)
	}
	if len(reloaded.IDEs) != 0 {
		t.Fatalf("reloaded.IDEs = %#v, 期望已清除", reloaded.IDEs)
	}
	if reloaded.Editor != "vim" {
		t.Fatalf("Editor 字段应保留, 得到 %q", reloaded.Editor)
	}

	// 校验 YAML 文本不再包含 ides: 行。
	raw, err := os.ReadFile(filepath.Join(mgr.GetDecDir(), "config.yaml"))
	if err != nil {
		t.Fatalf("读取 config.yaml 失败: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ides:") && !strings.HasPrefix(trimmed, "#") {
			t.Fatalf("config.yaml 不应包含 ides: 字段, 命中行: %q\n完整内容:\n%s", line, string(raw))
		}
	}
}

func TestSaveProjectSettings_RejectsEmptyWithoutClear(t *testing.T) {
	projectRoot := setupProjectSettingsEnv(t)

	_, err := SaveProjectSettings(SaveProjectSettingsInput{
		ProjectRoot: projectRoot,
		IDEs:        []string{},
	}, nil)
	if err == nil {
		t.Fatal("空 IDEs 且未设置 ClearOverride 时应报错")
	}
	if !strings.Contains(err.Error(), "至少选择一个 IDE") {
		t.Fatalf("错误信息应提示至少选择一个 IDE, 实际: %v", err)
	}
}

func TestSaveProjectSettings_RejectsUnknownIDE(t *testing.T) {
	projectRoot := setupProjectSettingsEnv(t)

	_, err := SaveProjectSettings(SaveProjectSettingsInput{
		ProjectRoot: projectRoot,
		IDEs:        []string{"unknown-ide"},
	}, nil)
	if err == nil {
		t.Fatal("未知 IDE 时应报错")
	}
	if !strings.Contains(err.Error(), "不支持的 IDE") {
		t.Fatalf("错误信息应提示 IDE 不支持, 实际: %v", err)
	}
}

func TestSaveProjectSettings_RequiresProjectRoot(t *testing.T) {
	_, err := SaveProjectSettings(SaveProjectSettingsInput{
		ProjectRoot:   "",
		ClearOverride: true,
	}, nil)
	if err == nil {
		t.Fatal("空项目根目录时应报错")
	}
	if !strings.Contains(err.Error(), "项目根目录不能为空") {
		t.Fatalf("错误信息应提示项目根目录不能为空, 实际: %v", err)
	}
}
