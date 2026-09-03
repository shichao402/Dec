package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func configureAuthTest(t *testing.T) {
	t.Helper()
	ClearSession()
	t.Cleanup(ClearSession)
	t.Setenv("DEC_BW_PASSWORD", "")
	home := t.TempDir()
	t.Setenv("DEC_HOME", home)
	dir := filepath.Join(home, "secrets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("server_url: https://vault.example.com\nemail: user@example.com\n")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	oldOpen, oldAvailable := openConsoleUnlock, consoleAvailable
	oldLast := consoleLaunchState.last
	t.Cleanup(func() {
		openConsoleUnlock = oldOpen
		consoleAvailable = oldAvailable
		consoleLaunchState.Lock()
		consoleLaunchState.last = oldLast
		consoleLaunchState.Unlock()
	})
	consoleAvailable = func() bool { return true }
	consoleLaunchState.Lock()
	consoleLaunchState.last = time.Time{}
	consoleLaunchState.Unlock()
}

func unlockInstanceForTest() {
	SetUserKey(make([]byte, 64))
	SetSession("console-session")
}

func TestEnsureSessionSkipsWhenInstanceUnlocked(t *testing.T) {
	configureAuthTest(t)
	unlockInstanceForTest()
	if err := EnsureSession(context.Background(), nil); err != nil {
		t.Fatalf("EnsureSession() = %v", err)
	}
}

func TestEnsureSessionNonInteractiveRequiresConsole(t *testing.T) {
	configureAuthTest(t)
	err := EnsureSession(context.Background(), nil)
	if !errors.Is(err, ErrConsoleUnlockRequired) {
		t.Fatalf("EnsureSession() = %v", err)
	}
}

func TestEnsureSessionOpensConsoleAndWaits(t *testing.T) {
	configureAuthTest(t)
	opened := make(chan struct{}, 1)
	openConsoleUnlock = func() error {
		opened <- struct{}{}
		return nil
	}
	done := make(chan error, 1)
	go func() {
		done <- EnsureSession(context.Background(), &EnsureSessionOpts{
			InteractiveLocal: true,
			UnlockTimeout:    time.Second,
		})
	}()
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("Console was not opened")
	}
	unlockInstanceForTest()
	if err := <-done; err != nil {
		t.Fatalf("EnsureSession() = %v", err)
	}
}

func TestEnsureSessionConcurrentLaunchIsDebounced(t *testing.T) {
	configureAuthTest(t)
	var calls atomic.Int32
	openConsoleUnlock = func() error {
		calls.Add(1)
		return nil
	}
	const waiters = 4
	done := make(chan error, waiters)
	for range waiters {
		go func() {
			done <- EnsureSession(context.Background(), &EnsureSessionOpts{
				InteractiveLocal: true,
				UnlockTimeout:    time.Second,
			})
		}()
	}
	time.Sleep(30 * time.Millisecond)
	unlockInstanceForTest()
	for range waiters {
		if err := <-done; err != nil {
			t.Fatalf("EnsureSession() = %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("open calls = %d, want 1", got)
	}
}

func TestEnsureSessionConsoleTimeout(t *testing.T) {
	configureAuthTest(t)
	openConsoleUnlock = func() error { return nil }
	err := EnsureSession(context.Background(), &EnsureSessionOpts{
		InteractiveLocal: true,
		UnlockTimeout:    20 * time.Millisecond,
	})
	if !errors.Is(err, ErrConsoleUnlockRequired) || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("EnsureSession() = %v", err)
	}
}

func TestEnsureSessionCancel(t *testing.T) {
	configureAuthTest(t)
	openConsoleUnlock = func() error { return nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := EnsureSession(ctx, &EnsureSessionOpts{InteractiveLocal: true, UnlockTimeout: time.Second})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureSession() = %v", err)
	}
}

func TestEnsureSessionProgrammaticDoesNotOpenConsole(t *testing.T) {
	configureAuthTest(t)
	t.Setenv("DEC_BW_PASSWORD", "dev-password")
	stub := NewStubAuthenticator("dev-password", "", "programmatic-session")
	oldFactory := authenticatorFactory
	authenticatorFactory = func() Authenticator {
		return &keySettingAuthenticator{Authenticator: stub}
	}
	t.Cleanup(func() { authenticatorFactory = oldFactory })
	var calls atomic.Int32
	openConsoleUnlock = func() error {
		calls.Add(1)
		return nil
	}
	if err := EnsureSession(context.Background(), &EnsureSessionOpts{InteractiveLocal: true}); err != nil {
		t.Fatalf("EnsureSession() = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("programmatic authentication opened Console")
	}
}

type keySettingAuthenticator struct{ Authenticator }

func (a *keySettingAuthenticator) Unlock(ctx context.Context, email, password string) (string, bool, error) {
	token, need2FA, err := a.Authenticator.Unlock(ctx, email, password)
	if err == nil && !need2FA {
		SetUserKey(make([]byte, 64))
	}
	return token, need2FA, err
}
