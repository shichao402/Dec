package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

func TestLoadGlobalSettingsReflectsConnectedRepoAndDefaults(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, nil)
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote, IDEs: []string{"cursor", "codex"}, Editor: "vim"}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}
	if _, err := config.EnsureGlobalVarsTemplate(); err != nil {
		t.Fatalf("EnsureGlobalVarsTemplate() 失败: %v", err)
	}

	state, err := LoadGlobalSettings(nil)
	if err != nil {
		t.Fatalf("LoadGlobalSettings() 失败: %v", err)
	}
	if !state.RepoConnected {
		t.Fatal("应识别已连接仓库")
	}
	if state.RepoURL != remote || state.ConnectedRepoURL != remote {
		t.Fatalf("RepoURL = %q, ConnectedRepoURL = %q, 期望 %q", state.RepoURL, state.ConnectedRepoURL, remote)
	}
	if len(state.SelectedIDEs) != 2 || state.SelectedIDEs[0] != "cursor" || state.SelectedIDEs[1] != "codex" {
		t.Fatalf("SelectedIDEs = %#v, 期望 [cursor codex]", state.SelectedIDEs)
	}
	if !state.VarsFileReady {
		t.Fatal("应识别本机 vars 模板已存在")
	}
	if state.ConfiguredEditor != "vim" {
		t.Fatalf("ConfiguredEditor = %q, 期望 %q", state.ConfiguredEditor, "vim")
	}
}

func TestConnectRepoPersistsGlobalConfig(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, nil)

	var events []OperationEvent
	result, err := ConnectRepo(remote, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("ConnectRepo() 失败: %v", err)
	}
	if result.RepoURL != remote {
		t.Fatalf("RepoURL = %q, 期望 %q", result.RepoURL, remote)
	}
	if strings.TrimSpace(result.BareRepo) == "" {
		t.Fatal("BareRepo 不应为空")
	}
	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() 失败: %v", err)
	}
	if globalConfig.RepoURL != remote {
		t.Fatalf("全局 repo_url = %q, 期望 %q", globalConfig.RepoURL, remote)
	}
	if len(events) != 2 {
		t.Fatalf("事件数 = %d, 期望 2", len(events))
	}
}

func TestSaveGlobalSettingsConfiguresAllSupportedIDEsByDefault(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	homeDir := t.TempDir()
	setEnvForProjectTest(t, "HOME", homeDir)
	remote := setupRemoteBareRepoProjectTest(t, nil)
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}
	if err := config.SaveGlobalConfig(&types.GlobalConfig{RepoURL: remote}); err != nil {
		t.Fatalf("SaveGlobalConfig() 失败: %v", err)
	}

	result, err := SaveGlobalSettings(SaveGlobalSettingsInput{}, nil)
	if err != nil {
		t.Fatalf("SaveGlobalSettings() 失败: %v", err)
	}
	if len(result.IDEs) == 0 {
		t.Fatal("默认应配置全部已注册 IDE")
	}
	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() 失败: %v", err)
	}
	if len(globalConfig.IDEs) != len(result.IDEs) {
		t.Fatalf("保存后的 IDE 数量 = %d, 期望 %d", len(globalConfig.IDEs), len(result.IDEs))
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".cursor", "skills", "dec", "SKILL.md")); err != nil {
		t.Fatalf("应为 cursor 安装内置 skill: %v", err)
	}
	if !result.VarsCreated {
		t.Fatal("首次保存应创建本机 vars 模板")
	}
}

func TestSaveGlobalSettingsFallsBackToConnectedRemote(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	homeDir := t.TempDir()
	setEnvForProjectTest(t, "HOME", homeDir)
	remote := setupRemoteBareRepoProjectTest(t, nil)
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}

	result, err := SaveGlobalSettings(SaveGlobalSettingsInput{IDEs: []string{"cursor"}}, nil)
	if err != nil {
		t.Fatalf("SaveGlobalSettings() 失败: %v", err)
	}
	if result.RepoURL != remote {
		t.Fatalf("RepoURL = %q, 期望 %q", result.RepoURL, remote)
	}
}

func TestSaveGlobalSettingsRejectsExplicitEmptyIDESelection(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	_, err := SaveGlobalSettings(SaveGlobalSettingsInput{RepoURL: "https://example.com/repo.git", IDEs: []string{}}, nil)
	if err == nil {
		t.Fatal("显式空 IDE 选择时应返回错误")
	}
	if !strings.Contains(err.Error(), "至少选择一个 IDE") {
		t.Fatalf("错误信息应提示至少选择一个 IDE, 实际: %v", err)
	}
}

func TestSaveGlobalSettingsRejectsUnknownIDE(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	_, err := SaveGlobalSettings(SaveGlobalSettingsInput{RepoURL: "https://example.com/repo.git", IDEs: []string{"unknown-ide"}}, nil)
	if err == nil {
		t.Fatal("未知 IDE 时应返回错误")
	}
	if !strings.Contains(err.Error(), "不支持的 IDE") {
		t.Fatalf("错误信息应提示 IDE 不支持, 实际: %v", err)
	}
}
