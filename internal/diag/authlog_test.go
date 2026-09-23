package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthLogRedactsSecretsAndWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DEC_HOME", home)
	AuthLog("login failed: access_token=%s password is incorrect password=%s", "secret-token", "hunter2")
	body, err := os.ReadFile(filepath.Join(home, "logs", "auth.log"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "secret-token") {
		t.Fatalf("token leaked: %s", text)
	}
	if strings.Contains(text, "hunter2") {
		t.Fatalf("password leaked: %s", text)
	}
	if !strings.Contains(text, "password is incorrect") {
		t.Fatalf("reason dropped: %s", text)
	}
	if !strings.Contains(text, "access_token=[redacted]") && !strings.Contains(text, "access_token: [redacted]") {
		t.Fatalf("token not redacted: %s", text)
	}
}
