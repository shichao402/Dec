package secrets

import (
	"context"
	"testing"
)

func TestUnlockWithPasswordStubSuccess(t *testing.T) {
	t.Cleanup(ClearSession)
	ClearSession()
	authenticatorFactory = func() Authenticator {
		return NewStubAuthenticator("secret", "", "rpc-session")
	}
	t.Cleanup(func() { authenticatorFactory = defaultAuthenticator })

	cfgDir := t.TempDir()
	t.Setenv("DEC_HOME", cfgDir)
	if err := SaveConfig(&Config{ServerURL: DefaultServerURL, Email: "alice@dec.test"}); err != nil {
		t.Fatal(err)
	}

	result, err := UnlockWithPassword(context.Background(), "alice@dec.test", "secret", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Need2FA {
		t.Fatal("不应需要 2FA")
	}
	if !InstanceUnlocked() {
		t.Fatal("解锁后 InstanceUnlocked 应为真")
	}
	if Session() != "rpc-session" {
		t.Fatalf("session = %q", Session())
	}
}

func TestUnlockWithPasswordWrongSecret(t *testing.T) {
	t.Cleanup(ClearSession)
	ClearSession()
	authenticatorFactory = func() Authenticator {
		return NewStubAuthenticator("secret", "", "rpc-session")
	}
	t.Cleanup(func() { authenticatorFactory = defaultAuthenticator })

	t.Setenv("DEC_HOME", t.TempDir())
	if err := SaveConfig(&Config{ServerURL: DefaultServerURL, Email: "alice@dec.test"}); err != nil {
		t.Fatal(err)
	}

	_, err := UnlockWithPassword(context.Background(), "alice@dec.test", "nope", "", false)
	if err == nil {
		t.Fatal("错误密码应失败")
	}
	if InstanceUnlocked() {
		t.Fatal("失败后仍应锁定")
	}
}

func TestUnlockWithPasswordTwoFactor(t *testing.T) {
	t.Cleanup(ClearSession)
	ClearSession()
	stub := NewStubAuthenticator("secret", "123456", "rpc-session")
	authenticatorFactory = func() Authenticator { return stub }
	t.Cleanup(func() { authenticatorFactory = defaultAuthenticator })

	t.Setenv("DEC_HOME", t.TempDir())
	if err := SaveConfig(&Config{ServerURL: DefaultServerURL, Email: "alice@dec.test"}); err != nil {
		t.Fatal(err)
	}

	first, err := UnlockWithPassword(context.Background(), "alice@dec.test", "secret", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Need2FA {
		t.Fatal("应需要 2FA")
	}
	if InstanceUnlocked() {
		t.Fatal("仅密码阶段不应解锁")
	}

	second, err := UnlockWithPassword(context.Background(), "", "", "123456", false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Need2FA || !InstanceUnlocked() {
		t.Fatalf("2FA 后应解锁: %#v unlocked=%v", second, InstanceUnlocked())
	}
}
