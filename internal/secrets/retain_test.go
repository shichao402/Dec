package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/types"
)

// countingAuthenticator 记录 Unlock 次数，并像真实 BWAuthenticator 那样落 vault key。
type countingAuthenticator struct {
	password string
	token    string
	err      error
	calls    int
}

func (a *countingAuthenticator) Unlock(_ context.Context, email, password string) (string, bool, error) {
	a.calls++
	if a.err != nil {
		return "", false, a.err
	}
	if password != a.password {
		return "", false, errors.New("主密码不正确")
	}
	SetUserKey(make([]byte, 64))
	return a.token, false, nil
}

func (a *countingAuthenticator) Verify2FA(context.Context, string, bool) (string, error) {
	return a.token, nil
}

func useCountingAuthenticator(t *testing.T, auth *countingAuthenticator) {
	t.Helper()
	old := authenticatorFactory
	authenticatorFactory = func() Authenticator { return auth }
	t.Cleanup(func() { authenticatorFactory = old })
}

// expireSessionForTest 模拟解锁有效期到点：只丢 session 与 vault key，
// 保留的主密码照旧留在进程内存里。
func expireSessionForTest() {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	expireSessionLocked(true)
}

func configureRetainTest(t *testing.T) {
	t.Helper()
	ClearSession()
	t.Cleanup(ClearSession)
	home := t.TempDir()
	t.Setenv("DEC_HOME", home)
	dir := filepath.Join(home, "secrets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("server_url: https://vault.example.com\nemail: alice@dec.test\n")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRetainedPasswordUnlocksAfterTimeout 锁住核心口径：解锁有效期到了必须过期，
// 但保留了主密码就由服务自己重新登录，不需要人工回到解锁页。
func TestRetainedPasswordUnlocksAfterTimeout(t *testing.T) {
	configureRetainTest(t)
	auth := &countingAuthenticator{password: "secret", token: "tok-1"}
	useCountingAuthenticator(t, auth)

	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	sessionNow = func() time.Time { return now }
	t.Cleanup(func() { sessionNow = time.Now })

	if _, err := UnlockWithPassword(context.Background(), "alice@dec.test", "secret", "", false, true); err != nil {
		t.Fatal(err)
	}
	if !InstanceUnlocked() {
		t.Fatal("人工解锁后应处于解锁态")
	}

	now = now.Add(DefaultSessionTTL)
	if InstanceUnlocked() {
		t.Fatal("到解锁有效期必须过期，云端会话是否还在不作数")
	}

	auth.token = "tok-2"
	if !TryAutoUnlock(context.Background()) {
		t.Fatal("保留了主密码应能静默重解锁")
	}
	if Session() != "tok-2" {
		t.Fatalf("Session() = %q，应换成重新登录取得的 token", Session())
	}
	if auth.calls != 2 {
		t.Fatalf("Unlock 调用 %d 次，应为人工一次 + 静默一次", auth.calls)
	}
}

func TestAutoUnlockSkippedWithoutRetention(t *testing.T) {
	configureRetainTest(t)
	auth := &countingAuthenticator{password: "secret", token: "tok-1"}
	useCountingAuthenticator(t, auth)

	if _, err := UnlockWithPassword(context.Background(), "alice@dec.test", "secret", "", false, false); err != nil {
		t.Fatal(err)
	}
	expireSessionForTest()

	if TryAutoUnlock(context.Background()) {
		t.Fatal("没勾保存主密码就不该有静默重解锁")
	}
	if auth.calls != 1 {
		t.Fatalf("Unlock 调用 %d 次，静默路径不该再登录", auth.calls)
	}
}

func TestAutoUnlockAfterTimeoutCanBeDisabled(t *testing.T) {
	configureRetainTest(t)
	disabled := false
	if err := config.SaveGlobalConfig(&types.GlobalConfig{AutoReunlockOnTimeout: &disabled}); err != nil {
		t.Fatal(err)
	}
	auth := &countingAuthenticator{password: "secret", token: "tok-1"}
	useCountingAuthenticator(t, auth)

	if _, err := UnlockWithPassword(context.Background(), "alice@dec.test", "secret", "", false, true); err != nil {
		t.Fatal(err)
	}
	expireSessionForTest()

	if TryAutoUnlock(context.Background()) {
		t.Fatal("关闭策略后，解锁有效期到点不应静默重解锁")
	}
	if auth.calls != 1 {
		t.Fatalf("Unlock 调用 %d 次，到期后应等待人工确认", auth.calls)
	}
	if !HasRetainedPassword() {
		t.Fatal("关闭到期自动重解锁不应丢弃进程内密码，云端 401 仍需使用")
	}
}

func TestCloudSessionFailureStillAutoUnlocksWhenTimeoutPolicyDisabled(t *testing.T) {
	configureRetainTest(t)
	disabled := false
	if err := config.SaveGlobalConfig(&types.GlobalConfig{AutoReunlockOnTimeout: &disabled}); err != nil {
		t.Fatal(err)
	}
	auth := &countingAuthenticator{password: "secret", token: "tok-1"}
	useCountingAuthenticator(t, auth)

	if _, err := UnlockWithPassword(context.Background(), "alice@dec.test", "secret", "", false, true); err != nil {
		t.Fatal(err)
	}
	if !InvalidateSession("tok-1") {
		t.Fatal("应清除被云端拒绝的 session")
	}
	auth.token = "tok-2"

	if !TryAutoUnlock(context.Background()) {
		t.Fatal("有效期内云端 session 失效仍应静默恢复")
	}
	if auth.calls != 2 {
		t.Fatalf("Unlock 调用 %d 次，应包含一次云端失效后的静默重登录", auth.calls)
	}
}

func TestRetainedPasswordDroppedAfterRepeatedFailures(t *testing.T) {
	configureRetainTest(t)
	auth := &countingAuthenticator{password: "secret", token: "tok-1"}
	useCountingAuthenticator(t, auth)

	if _, err := UnlockWithPassword(context.Background(), "alice@dec.test", "secret", "", false, true); err != nil {
		t.Fatal(err)
	}
	auth.err = errors.New("Bitwarden 登录失败: 主密码不正确")

	for i := 0; i < retainedFailureLimit; i++ {
		expireSessionForTest()
		if TryAutoUnlock(context.Background()) {
			t.Fatalf("第 %d 次静默解锁不该成功", i+1)
		}
	}
	if HasRetainedPassword() {
		t.Fatal("连续失败到上限后应丢弃保留的主密码，避免逼近账号锁定")
	}
}

func TestSessionTTLFollowsConfig(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	if err := config.SaveGlobalConfig(&types.GlobalConfig{SessionTimeout: "30m"}); err != nil {
		t.Fatal(err)
	}
	if got := SessionTTL(); got != 30*time.Minute {
		t.Fatalf("SessionTTL() = %v，应取配置的 session_timeout", got)
	}

	if err := config.SaveGlobalConfig(&types.GlobalConfig{SessionTimeout: "不是时长"}); err != nil {
		t.Fatal(err)
	}
	if got := SessionTTL(); got != DefaultSessionTTL {
		t.Fatalf("SessionTTL() = %v，无效配置应回落默认", got)
	}
}
