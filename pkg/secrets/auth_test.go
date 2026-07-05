package secrets

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/pkg/secrets/unlock"
)

func TestEnsureSession_SkipsWhenPresent(t *testing.T) {
	SetSession("existing")
	t.Cleanup(ClearSession)

	if err := EnsureSession(context.Background(), nil); err != nil {
		t.Fatalf("EnsureSession() = %v", err)
	}
	if Session() != "existing" {
		t.Fatalf("Session() = %q", Session())
	}
}

func TestEnsureSession_RequiresEmail(t *testing.T) {
	ClearSession()
	t.Cleanup(ClearSession)

	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: https://vault.example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := EnsureSession(context.Background(), nil)
	if err == nil || err.Error() != "Bitwarden email 未配置" {
		t.Fatalf("EnsureSession() = %v, 期望 email 未配置错误", err)
	}
}

func TestEnsureSession_WebUnlock(t *testing.T) {
	ClearSession()
	t.Cleanup(ClearSession)

	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	configYAML := "server_url: https://vault.example.com\nemail: user@example.com\n"
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	origFactory := authenticatorFactory
	authenticatorFactory = func() unlock.Authenticator {
		return unlock.NewStubAuthenticator("pw", "", "sess-from-web")
	}
	t.Cleanup(func() { authenticatorFactory = origFactory })

	origRun := unlockRun
	unlockRun = func(ctx context.Context, opts unlock.Options) error {
		opts.OpenBrowser = func(openURL string) error {
			resp, err := http.PostForm(openURL, url.Values{"password": {"pw"}})
			if err != nil {
				return err
			}
			resp.Body.Close()
			return nil
		}
		return unlock.Run(ctx, opts)
	}
	t.Cleanup(func() { unlockRun = origRun })

	if err := EnsureSession(context.Background(), nil); err != nil {
		t.Fatalf("EnsureSession() = %v", err)
	}
	if Session() != "sess-from-web" {
		t.Fatalf("Session() = %q", Session())
	}
}

func TestEnsureSession_Cancel(t *testing.T) {
	ClearSession()
	t.Cleanup(ClearSession)

	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: https://vault.example.com\nemail: user@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origRun := unlockRun
	unlockRun = func(ctx context.Context, opts unlock.Options) error {
		<-ctx.Done()
		return ctx.Err()
	}
	t.Cleanup(func() { unlockRun = origRun })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := EnsureSession(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureSession() = %v, 期望 context.Canceled", err)
	}
}
