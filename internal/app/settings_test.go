package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
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

func TestConnectRepoReturnsStructuredAuthRequiredWithoutPersisting(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	oldProbe := probeRepoForSettings
	probeRepoForSettings = func(string) error {
		return &repo.AuthenticationError{Host: "cnb.cool", Err: errors.New("credentials expired")}
	}
	t.Cleanup(func() { probeRepoForSettings = oldProbe })

	result, err := ConnectRepo("https://cnb.cool/example/private.git", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RepoAuthRequired || result.RepoHost != "cnb.cool" {
		t.Fatalf("result = %#v", result)
	}
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RepoURL != "" {
		t.Fatalf("认证确认前不得保存 repo_url，got %q", cfg.RepoURL)
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
	mcpData, err := os.ReadFile(filepath.Join(homeDir, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatalf("应为 cursor 安装内置 MCP 配置: %v", err)
	}
	var mcpCfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpData, &mcpCfg); err != nil {
		t.Fatalf("解析 cursor mcp.json 失败: %v", err)
	}
	decMCP, ok := mcpCfg.MCPServers["dec"]
	if !ok {
		t.Fatalf("cursor mcp.json 应包含 dec 条目: %#v", mcpCfg.MCPServers)
	}
	if decMCP.Command != "dec-mcp" || len(decMCP.Args) != 0 {
		t.Fatalf("dec MCP 配置 = %#v", decMCP)
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

func TestMergeJSONBuiltinMCPEntryPreservesExistingFields(t *testing.T) {
	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	existing := `{
  "mcpServers": {
    "custom": {
      "transportType": "streamable-http",
      "url": "https://example.com/mcp"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := mergeJSONBuiltinMCPEntry(configPath, "dec", types.MCPServer{
		Command: "dec",
		Args:    []string{"mcp", "--project-root", "${workspaceFolder}"},
	}); err != nil {
		t.Fatalf("mergeJSONBuiltinMCPEntry() = %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	servers, _ := parsed["mcpServers"].(map[string]any)
	custom, _ := servers["custom"].(map[string]any)
	if custom["transportType"] != "streamable-http" {
		t.Fatalf("custom server lost transportType: %#v", custom)
	}
	if _, ok := servers["dec"]; !ok {
		t.Fatalf("dec server missing: %#v", servers)
	}
}

func TestEnsureBuiltinIDEAssetsInstallsDecMCP(t *testing.T) {
	homeDir := t.TempDir()
	setEnvForProjectTest(t, "HOME", homeDir)
	warnings := EnsureBuiltinIDEAssets([]string{"cursor"}, nil)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	if !strings.Contains(string(data), `"dec"`) {
		t.Fatalf("mcp.json = %s", string(data))
	}
}

func TestEnsureBuiltinIDEAssetsSkipsRemovedInternalIDEs(t *testing.T) {
	homeDir := t.TempDir()
	setEnvForProjectTest(t, "HOME", homeDir)
	legacy := filepath.Join(homeDir, ".claude-internal")
	if err := os.MkdirAll(filepath.Join(legacy, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	warnings := EnsureBuiltinIDEAssets([]string{"claude-internal", "codex-internal", "cursor"}, nil)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("已移除的 ~/.claude-internal 应被删除, err=%v", err)
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	if !strings.Contains(string(data), `"dec"`) {
		t.Fatalf("mcp.json = %s", string(data))
	}
}

func setRuntimeGenerationForTest(t *testing.T, generation string) {
	t.Helper()
	previous := RuntimeGeneration()
	SetRuntimeGeneration(generation)
	t.Cleanup(func() { SetRuntimeGeneration(previous) })
}

func readCursorMCPConfig(t *testing.T, homeDir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(homeDir, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	return data
}

func decMCPEntry(t *testing.T, data []byte) types.MCPServer {
	t.Helper()
	var parsed struct {
		MCPServers map[string]types.MCPServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse mcp.json: %v", err)
	}
	server, ok := parsed.MCPServers[builtinDecMCPServerName]
	if !ok {
		t.Fatalf("mcp.json 缺少 dec 条目: %s", string(data))
	}
	return server
}

func TestEnsureBuiltinIDEAssetsMarksRuntimeGeneration(t *testing.T) {
	homeDir := t.TempDir()
	setEnvForProjectTest(t, "HOME", homeDir)
	setRuntimeGenerationForTest(t, "v1.13.73+0123456789abcdef")

	if warnings := EnsureBuiltinIDEAssets([]string{"cursor"}, nil); len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	got := decMCPEntry(t, readCursorMCPConfig(t, homeDir)).Env[builtinDecMCPRuntimeGenerationEnv]
	if got != "v1.13.73+0123456789abcdef" {
		t.Fatalf("%s = %q", builtinDecMCPRuntimeGenerationEnv, got)
	}
}

// 相同内容代号重复同步必须逐字节一致，否则 IDE 每次启动都会白白重启一次 dec-mcp。
func TestEnsureBuiltinIDEAssetsKeepsMCPEntryStableAcrossSameGeneration(t *testing.T) {
	homeDir := t.TempDir()
	setEnvForProjectTest(t, "HOME", homeDir)
	setRuntimeGenerationForTest(t, "v1.13.73+0123456789abcdef")

	EnsureBuiltinIDEAssets([]string{"cursor"}, nil)
	first := readCursorMCPConfig(t, homeDir)
	EnsureBuiltinIDEAssets([]string{"cursor"}, nil)
	second := readCursorMCPConfig(t, homeDir)

	if string(first) != string(second) {
		t.Fatalf("相同内容代号重复同步改写了配置:\n%s\n---\n%s", string(first), string(second))
	}
}

// 同版本换二进制必须让条目内容变化，这是开发期覆盖安装后重启 dec-mcp 的触发条件。
func TestEnsureBuiltinIDEAssetsRewritesMCPEntryOnGenerationChange(t *testing.T) {
	homeDir := t.TempDir()
	setEnvForProjectTest(t, "HOME", homeDir)

	setRuntimeGenerationForTest(t, "v1.13.73+0123456789abcdef")
	EnsureBuiltinIDEAssets([]string{"cursor"}, nil)
	before := readCursorMCPConfig(t, homeDir)

	setRuntimeGenerationForTest(t, "v1.13.73+fedcba9876543210")
	EnsureBuiltinIDEAssets([]string{"cursor"}, nil)
	after := readCursorMCPConfig(t, homeDir)

	if string(before) == string(after) {
		t.Fatalf("同版本换二进制后配置未变化: %s", string(before))
	}
	if got := decMCPEntry(t, after).Env[builtinDecMCPRuntimeGenerationEnv]; got != "v1.13.73+fedcba9876543210" {
		t.Fatalf("%s = %q", builtinDecMCPRuntimeGenerationEnv, got)
	}
}

// 内容代号未登记时不写标记，保持 dec-server 之外调用方的既有行为。
func TestEnsureBuiltinIDEAssetsOmitsMarkerWithoutRuntimeGeneration(t *testing.T) {
	homeDir := t.TempDir()
	setEnvForProjectTest(t, "HOME", homeDir)
	setRuntimeGenerationForTest(t, "")

	EnsureBuiltinIDEAssets([]string{"cursor"}, nil)
	if _, ok := decMCPEntry(t, readCursorMCPConfig(t, homeDir)).Env[builtinDecMCPRuntimeGenerationEnv]; ok {
		t.Fatalf("未登记内容代号时不应写入 %s", builtinDecMCPRuntimeGenerationEnv)
	}
}

func TestSaveGlobalSettings_DoesNotChangeRequires(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	setEnvForProjectTest(t, "HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"cli/dec.yaml": "name: cli\n",
		"woa/dec.yaml": "name: woa\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect() 失败: %v", err)
	}
	if _, err := SetWorkspaceRequires(
		context.Background(),
		NewWorkspace(WorkspaceUser, ""),
		types.RequiresSpec{"woa": types.RequiresVault, "cli": types.RequiresVault},
		nil,
	); err != nil {
		t.Fatalf("SetWorkspaceRequires() = %v", err)
	}

	if _, err := SaveGlobalSettings(SaveGlobalSettingsInput{
		RepoURL: remote,
		IDEs:    []string{"cursor"},
	}, nil); err != nil {
		t.Fatalf("SaveGlobalSettings() = %v", err)
	}

	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() = %v", err)
	}
	if len(globalConfig.Requires.VaultProjects()) != 2 ||
		globalConfig.Requires["woa"] != types.RequiresVault ||
		globalConfig.Requires["cli"] != types.RequiresVault {
		t.Fatalf("SaveGlobalSettings 不应改 requires: %#v", globalConfig.Requires)
	}

	state, err := LoadGlobalSettings(nil)
	if err != nil {
		t.Fatalf("LoadGlobalSettings() = %v", err)
	}
	if len(state.SubscribedProjects) != 2 {
		t.Fatalf("state.SubscribedProjects = %#v", state.SubscribedProjects)
	}
}

func TestListUserSecretBundleCandidates_MergesSources(t *testing.T) {
	got := listUserSecretBundleCandidates(
		[]string{"woa", "local-only"},
		[]string{"known-a"},
		[]string{"extra"},
		[]string{"cli"},
	)
	want := map[string]bool{"woa": true, "local-only": true, "known-a": true, "extra": true, "cli": true}
	for _, name := range got {
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("缺少候选 %#v, got %#v", want, got)
	}
}
