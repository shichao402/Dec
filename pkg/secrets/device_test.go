package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDeviceIdentifier_Persists(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	id1, err := EnsureDeviceIdentifier()
	if err != nil {
		t.Fatal(err)
	}
	if id1 == "" {
		t.Fatal("identifier 不应为空")
	}

	id2, err := EnsureDeviceIdentifier()
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("identifier = %q / %q, 期望一致", id1, id2)
	}

	path, err := DevicePath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("device.json 不应为空")
	}
}

func TestRememberTokenRoundTrip(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := RememberToken("user@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := SetRememberToken("user@example.com", "remember-abc"); err != nil {
		t.Fatal(err)
	}
	got, err := RememberToken("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "remember-abc" {
		t.Fatalf("RememberToken() = %q", got)
	}
	if err := ClearRememberToken("user@example.com"); err != nil {
		t.Fatal(err)
	}
	got, err = RememberToken("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("清除后 RememberToken() = %q", got)
	}
}

func TestKnownEmail_PrefersDeviceWhenConfigPlaceholder(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: https://vault.example.com\nemail: user@example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetRememberToken("saved@example.com", "token"); err != nil {
		t.Fatal(err)
	}
	if got := KnownEmail(); got != "saved@example.com" {
		t.Fatalf("KnownEmail() = %q, want saved@example.com", got)
	}
}
