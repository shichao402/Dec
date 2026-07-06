package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIntegrationAuth(t *testing.T) {
	projectRoot := t.TempDir()
	authPath := IntegrationAuthPath(projectRoot)
	if err := os.MkdirAll(filepath.Dir(authPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`email: test@example.com
password: secret-pass
server_url: https://vault.bitwarden.eu
`), 0600); err != nil {
		t.Fatal(err)
	}

	auth, err := LoadIntegrationAuth(projectRoot)
	if err != nil {
		t.Fatalf("LoadIntegrationAuth() = %v", err)
	}
	if auth.Email != "test@example.com" || auth.Password != "secret-pass" {
		t.Fatalf("auth = %#v", auth)
	}
	if auth.ServerURL != "https://vault.bitwarden.eu" {
		t.Fatalf("ServerURL = %q", auth.ServerURL)
	}
}

func TestApplyIntegrationAuth_SetsEnvAndConfig(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	t.Setenv("DEC_BW_PASSWORD", "")

	projectRoot := t.TempDir()
	authPath := IntegrationAuthPath(projectRoot)
	if err := os.MkdirAll(filepath.Dir(authPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`email: integration@example.com
password: integration-pass
`), 0600); err != nil {
		t.Fatal(err)
	}

	auth, err := ApplyIntegrationAuth(projectRoot)
	if err != nil {
		t.Fatalf("ApplyIntegrationAuth() = %v", err)
	}
	if auth == nil {
		t.Fatal("auth = nil")
	}
	if os.Getenv("DEC_BW_PASSWORD") != "integration-pass" {
		t.Fatalf("DEC_BW_PASSWORD = %q", os.Getenv("DEC_BW_PASSWORD"))
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Email != "integration@example.com" {
		t.Fatalf("cfg.Email = %q", cfg.Email)
	}
}

func TestScanSecretsBundleFiles_SkipsIntegrationAuth(t *testing.T) {
	projectRoot := t.TempDir()
	dir := SecretsBundleDir(projectRoot, "dec")
	if err := os.MkdirAll(filepath.Join(dir, "integration"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "integration", "bitwarden.yaml"), []byte("password: x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "tokens"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokens", "api.key"), []byte("token"), 0644); err != nil {
		t.Fatal(err)
	}

	notes, err := ScanSecretsBundleFiles(projectRoot, "dec")
	if err != nil {
		t.Fatalf("ScanSecretsBundleFiles() = %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("notes len = %d, want 1", len(notes))
	}
	if notes[0].RelativePath != ".secrets/dec/tokens/api.key" {
		t.Fatalf("note path = %q", notes[0].RelativePath)
	}
}

func TestIsIntegrationAuthRelWithinBundle(t *testing.T) {
	if !IsIntegrationAuthRelWithinBundle("integration/bitwarden.yaml") {
		t.Fatal("expected integration auth path")
	}
	if IsIntegrationAuthRelWithinBundle("tokens/api.key") {
		t.Fatal("unexpected integration auth path")
	}
}
