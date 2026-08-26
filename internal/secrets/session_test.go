package secrets

import (
	"testing"
	"time"
)

func TestHasSession_ExpiresAfterDefaultTTL(t *testing.T) {
	t.Cleanup(func() {
		sessionNow = time.Now
		ClearSession()
	})

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	sessionNow = func() time.Time { return now }

	SetSession("tok")
	SetUserKey(make([]byte, 64))
	if !HasSession() || !HasUserKey() {
		t.Fatal("刚写入应未过期")
	}
	if Session() != "tok" {
		t.Fatalf("Session() = %q", Session())
	}

	now = now.Add(DefaultSessionTTL - time.Second)
	if !HasSession() {
		t.Fatal("差 1s 到期仍应有效")
	}

	now = now.Add(time.Second)
	if HasSession() {
		t.Fatal("满 DefaultSessionTTL 应过期")
	}
	if Session() != "" {
		t.Fatal("过期后 Session 应为空")
	}
	if HasUserKey() || UserKey() != nil {
		t.Fatal("与 session 一同写入的 userKey 过期应清掉")
	}
}

func TestSetSession_EmptyClears(t *testing.T) {
	t.Cleanup(ClearSession)
	SetSession("tok")
	SetSession("")
	if HasSession() {
		t.Fatal("空 token 不应视为有效 session")
	}
}
