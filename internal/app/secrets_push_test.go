package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

func TestPushSecretsBundles_UsesDefaultServerWithoutConfigFile(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"default"},
	}); err != nil {
		t.Fatal(err)
	}

	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	var events []OperationEvent
	result, err := PushSecretsBundles(context.Background(), projectRoot, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("PushSecretsBundles() = %v", err)
	}
	if result.SkippedReason == "Bitwarden 未配置" {
		t.Fatalf("默认 server_url 时不应跳过: SkippedReason = %q", result.SkippedReason)
	}
	if containsScopeMessage(events, "push.secrets", "Bitwarden 未配置") {
		t.Fatalf("默认 server_url 时不应跳过 secrets: %#v", events)
	}
}

// push 递归扫描 .secrets 同步根并推送到远端 folder。
func TestPushSecretsBundles_UpdatesFromSyncRoot(t *testing.T) {
	setupSecretsConfigForPushTest(t)

	// 本仓库平面只推绑定项目那个 Project（<name>/private/local）。
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"dec/private/local": {
			{RelativePath: ".env/vikunja.env", Content: "VIKUNJA_API_TOKEN=old\n"},
			{RelativePath: "config/private.yaml", Content: "old"},
		},
	}}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "dec",
		EnabledBundles: []string{"vikunja", "dec"},
	}); err != nil {
		t.Fatal(err)
	}
	writeProjectFileForPushTest(t, projectRoot, ".secrets/dec/.env/vikunja.env", "VIKUNJA_API_TOKEN=abc\n")
	writeProjectFileForPushTest(t, projectRoot, ".secrets/dec/config/private.yaml", "token: abc\n")

	result, err := PushSecretsBundles(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("PushSecretsBundles() = %v", err)
	}
	if result.CreatedCount != 0 || result.UpdatedCount != 2 {
		t.Fatalf("result = %#v, 期望 2 条更新", result)
	}
	notes := map[string]string{}
	for _, note := range stub.NotesByFolder["dec/private/local"] {
		notes[note.RelativePath] = note.Content
	}
	if notes[".env/vikunja.env"] != "VIKUNJA_API_TOKEN=abc\n" || notes["config/private.yaml"] != "token: abc\n" {
		t.Fatalf("远端未被同步根覆盖: %#v", notes)
	}
}

func TestPushSecretsBundles_ReportsMissingLocalWithoutDeleting(t *testing.T) {
	setupSecretsConfigForPushTest(t)

	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"dec/private/local": {{RelativePath: "config/private.yaml", Content: "只在远端存在"}},
	}}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "dec",
		EnabledBundles: []string{"dec"},
	}); err != nil {
		t.Fatal(err)
	}

	var events []OperationEvent
	result, err := PushSecretsBundles(context.Background(), projectRoot, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("PushSecretsBundles() = %v", err)
	}
	if len(result.MissingLocal) != 1 || result.MissingLocal[0] != "config/private.yaml" {
		t.Fatalf("MissingLocal = %#v", result.MissingLocal)
	}
	if len(stub.NotesByFolder["dec/private/local"]) != 1 {
		t.Fatal("本地缺文件不应导致远端 note 被删")
	}
	if !containsScopeMessage(events, "push.secrets", "Remote 页") {
		t.Fatalf("应提示删除要走 Remote 页: %#v", events)
	}
}

// 用户平面 push 只扫 ~/.dec/secrets，不得把项目内 .secrets 推上去（ADR 0009 平面隔离）。
func TestPushWorkspaceSecretsBundles_UserPlaneSkipsProjectSecrets(t *testing.T) {
	decHome := setupSecretsConfigForPushTest(t)

	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"tencent-cloud/private/global": {{RelativePath: ".env/tencent.env", Content: "TOKEN=old\n"}},
		"dec/private/local":            {{RelativePath: "config/private.yaml", Content: "old"}},
	}}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	if err := config.SaveGlobalConfig(&types.GlobalConfig{
		EnabledBundles: []string{"tencent-cloud"},
	}); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "dec",
		EnabledBundles: []string{"dec"},
	}); err != nil {
		t.Fatal(err)
	}
	// 本仓库平面的落地文件：本轮 push 必须完全无视它们。
	writeProjectFileForPushTest(t, projectRoot, ".secrets/dec/config/private.yaml", "token: from-project\n")

	machineEnv := filepath.Join(decHome, "secrets", "tencent-cloud", ".env", "tencent.env")
	if err := os.MkdirAll(filepath.Dir(machineEnv), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(machineEnv, []byte("TOKEN=new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := PushWorkspaceSecretsBundles(context.Background(),
		NewWorkspace(WorkspaceUser, projectRoot), nil)
	if err != nil {
		t.Fatalf("PushWorkspaceSecretsBundles() = %v", err)
	}
	if result.UpdatedCount != 1 {
		t.Fatalf("UpdatedCount = %d, 期望只推本机平面那 1 条: %#v", result.UpdatedCount, result)
	}
	if got := stub.NotesByFolder["tencent-cloud/private/global"][0].Content; got != "TOKEN=new\n" {
		t.Fatalf("本机平面 secret 未被推送: %q", got)
	}
	if got := stub.NotesByFolder["dec/private/local"][0].Content; got != "old" {
		t.Fatalf("本仓库平面地址被本机 push 改写了: %q", got)
	}
}

func setupSecretsConfigForPushTest(t *testing.T) string {
	t.Helper()
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(secrets.Config{ServerURL: "https://vault.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	secrets.SetSession("test-session")
	secrets.SetUserKey(make([]byte, 64))
	t.Cleanup(secrets.ClearSession)
	return decHome
}

func writeProjectFileForPushTest(t *testing.T, projectRoot, rel, content string) {
	t.Helper()
	path := filepath.Join(projectRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
