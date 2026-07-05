package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
)

func TestMigrateEnabledSecretsBundles_EmitsEvents(t *testing.T) {
	projectRoot := t.TempDir()
	legacyPath := filepath.Join(projectRoot, ".config", "mise", "conf.d", "vikunja.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("[env]\nTOKEN=abc\n"), 0600); err != nil {
		t.Fatal(err)
	}

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"vikunja_workflow": {{
					RelativePath: ".config/mise/conf.d/vikunja.toml",
					Content:      "[env]\nTOKEN=abc\n",
				}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	var events []OperationEvent
	err := migrateEnabledSecretsBundles(context.Background(), projectRoot, []string{"vikunja"}, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("migrateEnabledSecretsBundles() = %v", err)
	}
	if !containsScopeMessage(events, "migrate.secrets", "移动本地") {
		t.Fatalf("应发出本地迁移事件: %#v", events)
	}
	if !containsScopeMessage(events, "migrate.secrets", "重命名 Bitwarden note") {
		t.Fatalf("应发出 Bitwarden 重命名事件: %#v", events)
	}
}

func TestPullEnabledSecretsBundles_IncludesProjectSecrets(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: https://vault.example.com\nemail: user@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}
	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{
			NotesByFolder: map[string][]secrets.SecureNote{
				"Dec": {{
					RelativePath: ".secrets/Dec/tokens/api.key",
					Content:      "token-value",
				}},
			},
		}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName: "Dec",
	}); err != nil {
		t.Fatal(err)
	}

	summary, err := pullEnabledSecretsBundles(context.Background(), projectRoot, nil, nil)
	if err != nil {
		t.Fatalf("pullEnabledSecretsBundles() = %v", err)
	}
	if summary.NoteCount != 1 {
		t.Fatalf("NoteCount = %d, want 1", summary.NoteCount)
	}
	target := filepath.Join(projectRoot, ".secrets", "Dec", "tokens", "api.key")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("project secrets 文件应落地: %v", err)
	}
}

func TestPullProjectAssets_RunsSecretsMigration(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: https://vault.example.com\nemail: user@example.com\nbundles:\n  - dec_bundle: vikunja\n    secrets_bundle: vikunja_workflow\n"), 0644); err != nil {
		t.Fatal(err)
	}
	secrets.SetSession("test-session")
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)

	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	projectRoot := t.TempDir()
	legacyPath := filepath.Join(projectRoot, ".config", "mise", "conf.d", "vikunja.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("[env]\nTOKEN=abc\n"), 0600); err != nil {
		t.Fatal(err)
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		IDEs:           []string{"cursor"},
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}

	var events []OperationEvent
	_, err := pullEnabledSecretsBundles(context.Background(), projectRoot, []string{"vikunja"}, ReporterFunc(func(event OperationEvent) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("pullEnabledSecretsBundles() = %v", err)
	}
	if !containsScopeMessage(events, "migrate.secrets", "移动本地") {
		t.Fatalf("pull 前应迁移本地文件: %#v", events)
	}
	target := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", "mise", "conf.d", "vikunja.toml")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("迁移后目标应存在: %v", err)
	}
}
