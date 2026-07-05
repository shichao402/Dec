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

func TestPushSecretsBundles_PushesProjectSecrets(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := secrets.Config{ServerURL: "https://vault.example.com"}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	secrets.SetSession("test-session")
	secrets.SetUserKey(make([]byte, 64))
	t.Cleanup(secrets.ClearSession)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName: "Dec",
	}); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(secrets.SecretsBundleDir(projectRoot, "Dec"), "tokens", "deploy.key")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("ssh-key"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := PushSecretsBundles(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("PushSecretsBundles() = %v", err)
	}
	if result.CreatedCount != 1 {
		t.Fatalf("CreatedCount = %d, want 1", result.CreatedCount)
	}
}

func TestPushSecretsBundles_PushesLocalFiles(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := secrets.Config{ServerURL: "https://vault.example.com"}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	secrets.SetSession("test-session")
	secrets.SetUserKey(make([]byte, 64))
	t.Cleanup(secrets.ClearSession)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secrets.SecretsBundleDir(projectRoot, "vikunja_workflow"), 0755); err != nil {
		t.Fatal(err)
	}
	confPath := filepath.Join(secrets.SecretsBundleDir(projectRoot, "vikunja_workflow"), "mise", "conf.d", "vikunja.toml")
	if err := os.MkdirAll(filepath.Dir(confPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(confPath, []byte("[env]\nX=1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := PushSecretsBundles(context.Background(), projectRoot, nil)
	if err != nil {
		t.Fatalf("PushSecretsBundles() = %v", err)
	}
	if result.CreatedCount != 1 {
		t.Fatalf("CreatedCount = %d, want 1", result.CreatedCount)
	}
}
