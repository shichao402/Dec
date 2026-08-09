package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadConfig_AppliesDefaultServerURL(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	if cfg.ServerURL != DefaultServerURL {
		t.Fatalf("ServerURL = %q, 期望 %q", cfg.ServerURL, DefaultServerURL)
	}
}

func TestLoadConfig_PreservesExplicitServerURL(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: https://vault.bitwarden.eu\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	if cfg.ServerURL != "https://vault.bitwarden.eu" {
		t.Fatalf("ServerURL = %q", cfg.ServerURL)
	}
}

func TestLoadConfig_MigratesEmptyServerURL(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("email: user@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	if cfg.ServerURL != DefaultServerURL {
		t.Fatalf("ServerURL = %q, 期望默认 %q", cfg.ServerURL, DefaultServerURL)
	}
}

func TestIsConfigured_TrueWithDefault(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	configured, err := IsConfigured()
	if err != nil {
		t.Fatalf("IsConfigured() = %v", err)
	}
	if !configured {
		t.Fatal("IsConfigured() = false, 期望 true（默认 server_url）")
	}
}

func TestSaveConfig_WritesDefaultAndComments(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	if err := SaveConfig(&Config{}); err != nil {
		t.Fatalf("SaveConfig() = %v", err)
	}

	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "vault.bitwarden.com") {
		t.Fatalf("应包含默认 server_url 注释或值: %s", content)
	}
	if !strings.Contains(content, "vault.bitwarden.eu") {
		t.Fatalf("应包含 EU 服务器注释: %s", content)
	}
	if !strings.Contains(content, "vault.example.com") {
		t.Fatalf("应包含自托管示例注释: %s", content)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != DefaultServerURL {
		t.Fatalf("ServerURL = %q", cfg.ServerURL)
	}
}

func TestResolveBinding_DefaultSameName(t *testing.T) {
	cfg := &Config{}
	binding := cfg.ResolveBinding("vikunja")
	if binding.SecretsBundleName != "vikunja" {
		t.Fatalf("SecretsBundleName = %q, 期望 vikunja", binding.SecretsBundleName)
	}
	if binding.DecBundleName != "vikunja" {
		t.Fatalf("DecBundleName = %q", binding.DecBundleName)
	}
}

func TestResolveBinding_ExplicitBindingWins(t *testing.T) {
	cfg := &Config{
		Bundles: []BundleBinding{{
			DecBundleName:     "vikunja",
			SecretsBundleName: "custom_vault",
		}},
	}
	binding := cfg.ResolveBinding("vikunja")
	if binding.SecretsBundleName != "custom_vault" {
		t.Fatalf("SecretsBundleName = %q", binding.SecretsBundleName)
	}
}

func TestMigrateConfigIfNeeded_MigratesLegacyFolderField(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: https://vault.example.com\nemail: user@example.com\nbundles:\n  - dec_bundle: vikunja\n    folder: custom_vault\n"), 0600); err != nil {
		t.Fatal(err)
	}

	changed, err := MigrateConfigIfNeeded()
	if err != nil {
		t.Fatalf("MigrateConfigIfNeeded() = %v", err)
	}
	if !changed {
		t.Fatal("应迁移废弃的 folder 字段")
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Bundles) != 1 || cfg.Bundles[0].SecretsBundleName != "custom_vault" {
		t.Fatalf("cfg.Bundles = %#v", cfg.Bundles)
	}
	if cfg.Bundles[0].Folder != "" {
		t.Fatalf("folder 字段应已清空: %#v", cfg.Bundles[0])
	}
}

func TestSaveEmail_PreservesOtherFields(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	initial := "server_url: https://vault.example.com\nproject_secrets: myproj\n"
	configPath := filepath.Join(secretsDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	if err := SaveEmail("real-user@example.com"); err != nil {
		t.Fatalf("SaveEmail() = %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Email != "real-user@example.com" {
		t.Fatalf("Email = %q", cfg.Email)
	}
	if cfg.ServerURL != "https://vault.example.com" {
		t.Fatalf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.ProjectSecrets != "myproj" {
		t.Fatalf("ProjectSecrets = %q", cfg.ProjectSecrets)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	skipUnlessUnixFileMode(t)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("权限 = %o, 期望 0600", info.Mode().Perm())
	}
}

func TestSaveEmail_SkipsPlaceholder(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(secretsDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("server_url: https://vault.example.com\nemail: real@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveEmail("user@example.com"); err != nil {
		t.Fatalf("SaveEmail() = %v", err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Email != "real@example.com" {
		t.Fatalf("Email = %q, 占位邮箱不应覆盖已有配置", cfg.Email)
	}
}
