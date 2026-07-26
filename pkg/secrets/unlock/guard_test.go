package unlock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWebUnlockBlockedUnderGoTest(t *testing.T) {
	if WebUnlockAllowed() {
		t.Fatal("go test 进程默认不应允许 web unlock")
	}
}

func TestWebUnlockAllowedRespectsEnvOverrides(t *testing.T) {
	t.Setenv(EnvAllowWebUnlock, "1")
	if !WebUnlockAllowed() {
		t.Fatalf("%s=1 时应允许 web unlock", EnvAllowWebUnlock)
	}

	// 显式允许优先于显式禁止，便于真机手工验证。
	t.Setenv(EnvNoWebUnlock, "1")
	if !WebUnlockAllowed() {
		t.Fatalf("%s=1 应优先于 %s=1", EnvAllowWebUnlock, EnvNoWebUnlock)
	}

	t.Setenv(EnvAllowWebUnlock, "")
	if WebUnlockAllowed() {
		t.Fatalf("%s=1 时应禁止 web unlock", EnvNoWebUnlock)
	}
}

// Run 未注入 OpenBrowser 时会走系统浏览器；测试进程必须直接拒绝，
// 否则每次 go test 都会弹出解锁页要求人工输入主密码。
func TestRunRefusesRealBrowserUnderGoTest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := Run(ctx, Options{
		Authenticator: NewStubAuthenticator("pw", "", "sess-blocked"),
	})
	if !errors.Is(err, ErrWebUnlockBlocked) {
		t.Fatalf("Run() = %v, 期望 ErrWebUnlockBlocked", err)
	}
}

func TestOpenSystemBrowserRefusesUnderGoTest(t *testing.T) {
	if err := openSystemBrowser("https://example.com"); !errors.Is(err, ErrWebUnlockBlocked) {
		t.Fatalf("openSystemBrowser() = %v, 期望 ErrWebUnlockBlocked", err)
	}
}
