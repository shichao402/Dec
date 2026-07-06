package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
	"gopkg.in/yaml.v3"
)

func TestListSecretsMetadata_LocalOnlyNoContent(t *testing.T) {
	projectRoot := t.TempDir()
	secretsDir := filepath.Join(projectRoot, ".secrets", "vikunja_workflow", "mise", "conf.d")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	secretFile := filepath.Join(secretsDir, "vikunja.toml")
	if err := os.WriteFile(secretFile, []byte("TOP_SECRET_TOKEN=never-expose\n"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := ListSecretsMetadata(context.Background(), projectRoot, false, nil)
	if err != nil {
		t.Fatalf("ListSecretsMetadata() = %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files = %#v", result.Files)
	}
	file := result.Files[0]
	if file.SecretsBundle != "vikunja_workflow" {
		t.Fatalf("bundle = %q", file.SecretsBundle)
	}
	if !file.LocalExists || file.LocalSizeBytes == 0 {
		t.Fatalf("local meta = %#v", file)
	}
}

func TestListSecretsMetadata_IncludeRemoteUsesStubWithoutContent(t *testing.T) {
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
	secrets.SetUserKey(bytes.Repeat([]byte{0x01}, 64))
	t.Cleanup(secrets.ClearSession)

	orig := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"vikunja_workflow": {{RelativePath: "mise/conf.d/vikunja.toml", Content: "SECRET=1"}},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = orig })

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		EnabledBundles: []string{"vikunja"},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := ListSecretsMetadata(context.Background(), projectRoot, true, nil)
	if err != nil {
		t.Fatalf("ListSecretsMetadata() = %v", err)
	}
	if !result.RemoteChecked {
		t.Fatalf("expected remote checked, got %#v", result)
	}
	foundRemote := false
	for _, f := range result.Files {
		if f.RemoteExists != nil && *f.RemoteExists {
			foundRemote = true
		}
	}
	if !foundRemote {
		t.Fatalf("expected remote file metadata: %#v", result.Files)
	}
}

func TestMCPSessionUnlockTimeout(t *testing.T) {
	if MCPSessionUnlockTimeout != 3*time.Minute {
		t.Fatalf("MCPSessionUnlockTimeout = %v", MCPSessionUnlockTimeout)
	}
}
