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

// 集成凭据落在 dec/private/project 同步根内（commit e96cd86），因此会随该项目
// 的 push 同步到远端——这是刻意的：已有 vault 访问能力的开发机据此恢复测试账号。
func TestIntegrationAuthPath_SyncsWithDecPScope(t *testing.T) {
	projectRoot := t.TempDir()
	authPath := IntegrationAuthPath(projectRoot)
	if err := os.MkdirAll(filepath.Dir(authPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte("password: x"), 0600); err != nil {
		t.Fatal(err)
	}

	target, err := NewPSyncTarget("dec", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	result, err := PushBundle(t.Context(), &StubClient{}, PushBundleRequest{
		ProjectRoot: projectRoot,
		Target:      target,
	})
	if err != nil {
		t.Fatalf("PushBundle() = %v", err)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "integration/bitwarden.yaml" {
		t.Fatalf("Paths = %#v, 期望集成凭据随 dec/private/project 推送", result.Paths)
	}
}
