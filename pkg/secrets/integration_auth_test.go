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

// 集成凭据不再需要「扫描时跳过」的特判：push 只推远端 folder 里有 note 的路径，
// 本地文件不会被自动发现，`.secrets/dec/integration/` 因此天然不参与同步。
func TestIntegrationAuthPath_StaysOutOfSyncScope(t *testing.T) {
	projectRoot := t.TempDir()
	authPath := IntegrationAuthPath(projectRoot)
	if err := os.MkdirAll(filepath.Dir(authPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte("password: x"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := PushBundle(t.Context(), &StubClient{}, PushBundleRequest{
		ProjectRoot:   projectRoot,
		DecBundleName: "dec",
		Binding:       BundleBinding{SecretsBundleName: "dec"},
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if len(result.Paths) != 0 {
		t.Fatalf("Paths = %#v, 集成凭据不应被推送", result.Paths)
	}
}
