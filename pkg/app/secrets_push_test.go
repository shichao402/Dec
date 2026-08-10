package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
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

	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"bundle/vikunja": {{RelativePath: "env/vikunja.env", Content: "VIKUNJA_API_TOKEN=old\n"}},
		"Dec":     {{RelativePath: "config/private.yaml", Content: "old"}},
	}}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName:    "Dec",
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}
	writeProjectFileForPushTest(t, projectRoot, ".secrets/bundles/vikunja/env/vikunja.env", "VIKUNJA_API_TOKEN=abc\n")
	writeProjectFileForPushTest(t, projectRoot, ".secrets/project/config/private.yaml", "token: abc\n")

	result, err := PushSecretsBundles(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("PushSecretsBundles() = %v", err)
	}
	if result.CreatedCount != 0 || result.UpdatedCount != 2 {
		t.Fatalf("result = %#v, 期望 2 条更新", result)
	}
	if got := stub.NotesByFolder["bundle/vikunja"][0].Content; got != "VIKUNJA_API_TOKEN=abc\n" {
		t.Fatalf("bundle secret 未被本地覆盖: %q", got)
	}
	if got := stub.NotesByFolder["Dec"][0].Content; got != "token: abc\n" {
		t.Fatalf("project secret 未被本地覆盖: %q", got)
	}
}

func TestPushSecretsBundles_ReportsMissingLocalWithoutDeleting(t *testing.T) {
	setupSecretsConfigForPushTest(t)

	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"Dec": {{RelativePath: "config/private.yaml", Content: "只在远端存在"}},
	}}
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "Dec"}); err != nil {
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
	if len(stub.NotesByFolder["Dec"]) != 1 {
		t.Fatal("本地缺文件不应导致远端 note 被删")
	}
	if !containsScopeMessage(events, "push.secrets", "Remote 页") {
		t.Fatalf("应提示删除要走 Remote 页: %#v", events)
	}
}

func setupSecretsConfigForPushTest(t *testing.T) {
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
