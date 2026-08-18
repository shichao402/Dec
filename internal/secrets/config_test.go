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
	if binding.SecretsBundleName != "bundle/vikunja" {
		t.Fatalf("SecretsBundleName = %q, 期望 bundle/vikunja", binding.SecretsBundleName)
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

func TestNormalizeBundleNames(t *testing.T) {
	got := NormalizeBundleNames([]string{"  woa ", "bundle/vikunja", "woa", ""})
	if len(got) != 2 || got[0] != "woa" || got[1] != "vikunja" {
		t.Fatalf("NormalizeBundleNames = %#v", got)
	}
}

func TestLoadSaveConfig_ClearsLegacyUserEnabledBundles(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	configPath := filepath.Join(decHome, "secrets", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	legacy := "server_url: https://vault.bitwarden.com\nemail: a@b.com\nuser_enabled_bundles:\n  - woa\n  - bundle/vikunja\n"
	if err := os.WriteFile(configPath, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	peek, err := PeekLegacyUserEnabledBundles()
	if err != nil {
		t.Fatalf("PeekLegacyUserEnabledBundles() = %v", err)
	}
	if len(peek) != 2 || peek[0] != "woa" || peek[1] != "vikunja" {
		t.Fatalf("peek = %#v", peek)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	names := cfg.LegacyUserEnabledBundleNames()
	if len(names) != 2 || names[0] != "woa" || names[1] != "vikunja" {
		t.Fatalf("LegacyUserEnabledBundleNames = %#v", names)
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() = %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "user_enabled_bundles") {
		t.Fatalf("SaveConfig 后不应再持久化 user_enabled_bundles:\n%s", data)
	}
	peekAfter, err := PeekLegacyUserEnabledBundles()
	if err != nil {
		t.Fatal(err)
	}
	if len(peekAfter) != 0 {
		t.Fatalf("清空后 peek = %#v", peekAfter)
	}
}

func TestRememberSecretBundles_MergesIdempotent(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	if err := RememberSecretBundles([]string{"woa", "bundle/cli"}); err != nil {
		t.Fatalf("RememberSecretBundles() = %v", err)
	}
	if err := RememberSecretBundles([]string{"woa", "vikunja"}); err != nil {
		t.Fatalf("RememberSecretBundles() 二次 = %v", err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.KnownSecretBundleNames()
	if len(got) != 3 || got[0] != "woa" || got[1] != "cli" || got[2] != "vikunja" {
		t.Fatalf("KnownSecretBundleNames = %#v", got)
	}
}

func TestForgetSecretBundles_RemovesIdempotent(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	if err := RememberSecretBundles([]string{"pkv", "woa", "vikunja"}); err != nil {
		t.Fatalf("RememberSecretBundles() = %v", err)
	}
	if err := ForgetSecretBundles([]string{"pkv", "missing"}); err != nil {
		t.Fatalf("ForgetSecretBundles() = %v", err)
	}
	if err := ForgetSecretBundles([]string{"pkv"}); err != nil {
		t.Fatalf("ForgetSecretBundles() 二次 = %v", err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.KnownSecretBundleNames()
	if len(got) != 2 || got[0] != "woa" || got[1] != "vikunja" {
		t.Fatalf("KnownSecretBundleNames = %#v", got)
	}
}

func TestRememberSecretBundleMembers_OverwritesAndClears(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	if err := RememberSecretBundleMembers("agents-board", []string{".env/foo.env", ".sshkey/deploy", ".env/foo.env"}); err != nil {
		t.Fatalf("RememberSecretBundleMembers() = %v", err)
	}
	got := SecretBundleMembers("agents-board")
	if len(got) != 2 || got[0] != ".env/foo.env" || got[1] != ".sshkey/deploy" {
		t.Fatalf("首次缓存 = %#v", got)
	}

	if err := RememberSecretBundleMembers("bundle/agents-board", []string{".env/bar.env"}); err != nil {
		t.Fatalf("覆盖写入 = %v", err)
	}
	got = SecretBundleMembers("agents-board")
	if len(got) != 1 || got[0] != ".env/bar.env" {
		t.Fatalf("覆盖后应只剩新路径: %#v", got)
	}

	if err := RememberSecretBundleMembers("agents-board", nil); err != nil {
		t.Fatalf("空列表写入 = %v", err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := cfg.KnownSecretBundleMembers["agents-board"]
	if !ok {
		t.Fatal("空远端仍应留下 key，避免下次读到陈旧路径被当成「从未刷新」")
	}
	if len(paths) != 0 {
		t.Fatalf("空列表应写成 [], got %#v", paths)
	}
}

func TestForgetSecretBundles_DropsCachedMembers(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	if err := RememberSecretBundles([]string{"pkv", "woa"}); err != nil {
		t.Fatal(err)
	}
	if err := RememberAllSecretBundleMembers(map[string][]string{
		"pkv": {".env/keep.env"},
		"woa": {".sshkey/deploy"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ForgetSecretBundles([]string{"pkv"}); err != nil {
		t.Fatal(err)
	}
	if got := SecretBundleMembers("pkv"); len(got) != 0 {
		t.Fatalf("Forget 后 pkv 成员缓存应清空: %#v", got)
	}
	if got := SecretBundleMembers("woa"); len(got) != 1 || got[0] != ".sshkey/deploy" {
		t.Fatalf("未 Forget 的 woa 应保留: %#v", got)
	}
}
