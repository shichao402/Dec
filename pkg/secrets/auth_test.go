package secrets

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestEnsureSession_WebUnlockWithoutEmail(t *testing.T) {
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

	origFactory := authenticatorFactory
	authenticatorFactory = func() unlock.Authenticator {
		return unlock.NewStubAuthenticator("pw", "", "sess-no-email")
	}
	t.Cleanup(func() { authenticatorFactory = origFactory })

	origRun := unlockRun
	unlockRun = func(ctx context.Context, opts unlock.Options) error {
		opts.OpenBrowser = func(openURL string) error {
			resp, err := http.PostForm(openURL, url.Values{
				"email":    {"user@example.com"},
				"password": {"pw"},
			})
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
	if Session() != "sess-no-email" {
		t.Fatalf("Session() = %q", Session())
	}
}

func TestEnsureSession_WebUnlock_SavesEmail(t *testing.T) {
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

	origFactory := authenticatorFactory
	authenticatorFactory = func() unlock.Authenticator {
		return unlock.NewStubAuthenticator("pw", "", "sess-from-web")
	}
	t.Cleanup(func() { authenticatorFactory = origFactory })

	origRun := unlockRun
	unlockRun = func(ctx context.Context, opts unlock.Options) error {
		opts.OpenBrowser = func(openURL string) error {
			resp, err := http.PostForm(openURL, url.Values{
				"email":    {"saved@example.com"},
				"password": {"pw"},
			})
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
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Email != "saved@example.com" {
		t.Fatalf("Email = %q, 期望 saved@example.com", cfg.Email)
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
			resp, err := http.PostForm(openURL, url.Values{
				"email":    {"user@example.com"},
				"password": {"pw"},
			})
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

func TestEnsureSession_ProgrammaticUnlock(t *testing.T) {
	ClearSession()
	t.Cleanup(ClearSession)

	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	t.Setenv("DEC_BW_PASSWORD", "dev-password")
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: https://vault.example.com\nemail: dev@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SetRememberToken("dev@example.com", "remember-token"); err != nil {
		t.Fatal(err)
	}

	origFactory := authenticatorFactory
	authenticatorFactory = func() unlock.Authenticator {
		return unlock.NewStubAuthenticator("dev-password", "", "sess-programmatic")
	}
	t.Cleanup(func() { authenticatorFactory = origFactory })

	webUnlockCalled := false
	statuses := []string{}
	origRun := unlockRun
	unlockRun = func(ctx context.Context, opts unlock.Options) error {
		webUnlockCalled = true
		return unlock.Run(ctx, opts)
	}
	t.Cleanup(func() { unlockRun = origRun })

	if err := EnsureSession(context.Background(), &EnsureSessionOpts{
		OnStatus: func(message string) {
			statuses = append(statuses, message)
		},
	}); err != nil {
		t.Fatalf("EnsureSession() = %v", err)
	}
	if webUnlockCalled {
		t.Fatal("DEC_BW_PASSWORD 已设置且登录成功时不应触发 web unlock")
	}
	if len(statuses) == 0 || statuses[len(statuses)-1] != "[auth] session ready" {
		t.Fatalf("statuses = %v", statuses)
	}
	if Session() != "sess-programmatic" {
		t.Fatalf("Session() = %q", Session())
	}
}

func TestEnsureSession_ProgrammaticFails_NoWebUnlock(t *testing.T) {
	ClearSession()
	t.Cleanup(ClearSession)

	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	t.Setenv("DEC_BW_PASSWORD", "dev-password")
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: https://vault.example.com\nemail: dev@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origFactory := authenticatorFactory
	authenticatorFactory = func() unlock.Authenticator {
		return unlock.NewStubAuthenticator("dev-password", "123456", "sess-2fa")
	}
	t.Cleanup(func() { authenticatorFactory = origFactory })

	webUnlockCalled := false
	origRun := unlockRun
	unlockRun = func(ctx context.Context, opts unlock.Options) error {
		webUnlockCalled = true
		return unlock.Run(ctx, opts)
	}
	t.Cleanup(func() { unlockRun = origRun })

	err := EnsureSession(context.Background(), nil)
	if err == nil {
		t.Fatal("EnsureSession() 应失败")
	}
	if webUnlockCalled {
		t.Fatal("DEC_BW_PASSWORD 已设置且需 2FA 时不应触发 web unlock")
	}
	if !strings.Contains(err.Error(), "2FA") {
		t.Fatalf("EnsureSession() = %v, 期望 2FA 相关错误", err)
	}
}

func TestEnsureSession_UnlockTimeoutOpt(t *testing.T) {
	ClearSession()
	t.Cleanup(ClearSession)

	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	t.Setenv("DEC_BW_UNLOCK_TIMEOUT", "5m")
	secretsDir := filepath.Join(decHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "config.yaml"), []byte("server_url: https://vault.example.com\nemail: user@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var gotTimeout time.Duration
	origRun := unlockRun
	unlockRun = func(ctx context.Context, opts unlock.Options) error {
		if deadline, ok := ctx.Deadline(); ok {
			gotTimeout = time.Until(deadline)
		}
		<-ctx.Done()
		return ctx.Err()
	}
	t.Cleanup(func() { unlockRun = origRun })

	_ = EnsureSession(context.Background(), &EnsureSessionOpts{
		UnlockTimeout: 200 * time.Millisecond,
	})
	if gotTimeout <= 0 || gotTimeout > 500*time.Millisecond {
		t.Fatalf("unlock ctx timeout = %v, want ~200ms", gotTimeout)
	}
}

func TestEnsureSession_WebUnlockTimeout(t *testing.T) {
	ClearSession()
	t.Cleanup(ClearSession)

	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	t.Setenv("DEC_BW_UNLOCK_TIMEOUT", "100ms")
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

	statuses := []string{}
	err := EnsureSession(context.Background(), &EnsureSessionOpts{
		OnStatus: func(message string) {
			statuses = append(statuses, message)
		},
	})
	if err == nil {
		t.Fatal("EnsureSession() 应超时失败")
	}
	if !strings.Contains(err.Error(), "超时") {
		t.Fatalf("EnsureSession() = %v, 期望超时错误", err)
	}
	foundTimeoutLog := false
	for _, s := range statuses {
		if s == "[auth] web unlock: timeout - no user input" {
			foundTimeoutLog = true
		}
	}
	if !foundTimeoutLog {
		t.Fatalf("未记录超时日志: %v", statuses)
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
