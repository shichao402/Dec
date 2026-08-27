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

// 测试二进制内的禁止必须是无条件的：任何「强制允许」开关都会随 shell 环境
// 继承进 go test，把弹窗保护静默关掉（曾因此让整包测试卡在等人输密码）。
func TestWebUnlockCannotBeReEnabledByEnvInTests(t *testing.T) {
	for _, name := range []string{
		"DEC_ALLOW_WEB_UNLOCK",
		"DEC_WEB_UNLOCK",
		"DEC_FORCE_WEB_UNLOCK",
		EnvNoWebUnlock,
	} {
		t.Setenv(name, "1")
	}
	if WebUnlockAllowed() {
		t.Fatal("测试二进制内不得被任何环境变量放行 web unlock")
	}
}

func TestWebUnlockAllowedHonorsNoWebUnlockForSubprocesses(t *testing.T) {
	t.Setenv(EnvNoWebUnlock, "1")
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
