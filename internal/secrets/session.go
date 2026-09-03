package secrets

import (
	"context"
	"sync"
	"time"
)

// DefaultSessionTTL 对齐 Bitwarden Identity 常见的 access_token expires_in（约 3600s）。
// 云端无法调该值；客户端到期后 HasSession 为假，须重新 EnsureSession。
const DefaultSessionTTL = time.Hour

var (
	sessionMu       sync.Mutex
	session         string
	userKey         []byte
	sessionDeadline time.Time
	sessionNow      = time.Now
	sessionChanged  = make(chan struct{})
)

// SetSession 写入进程内 Bitwarden session（禁止落盘），默认 DefaultSessionTTL 后失效。
func SetSession(token string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	setSessionLocked(token, DefaultSessionTTL)
	notifySessionChangedLocked()
}

func setSessionLocked(token string, ttl time.Duration) {
	session = token
	if token == "" {
		sessionDeadline = time.Time{}
		return
	}
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	sessionDeadline = sessionNow().Add(ttl)
}

func expireSessionLocked() {
	session = ""
	userKey = nil
	sessionDeadline = time.Time{}
	notifySessionChangedLocked()
}

func dropIfSessionExpiredLocked() {
	if session == "" {
		return
	}
	if !sessionDeadline.IsZero() && !sessionNow().Before(sessionDeadline) {
		expireSessionLocked()
	}
}

func sessionLiveLocked() bool {
	dropIfSessionExpiredLocked()
	return session != ""
}

// SetUserKey 写入进程内 vault symmetric key（禁止落盘）。
func SetUserKey(key []byte) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if len(key) == 0 {
		userKey = nil
		notifySessionChangedLocked()
		return
	}
	copied := make([]byte, len(key))
	copy(copied, key)
	userKey = copied
	notifySessionChangedLocked()
}

// UserKey 返回当前进程内 vault symmetric key 副本。
func UserKey() []byte {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	dropIfSessionExpiredLocked()
	if len(userKey) == 0 {
		return nil
	}
	copied := make([]byte, len(userKey))
	copy(copied, userKey)
	return copied
}

// HasUserKey 判定进程内是否已有 vault symmetric key。
func HasUserKey() bool {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	dropIfSessionExpiredLocked()
	return len(userKey) == 64
}

// Session 返回当前进程内未过期 session；无或已过期时为空字符串。
func Session() string {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if !sessionLiveLocked() {
		return ""
	}
	return session
}

// HasSession 判定进程内是否已有未过期 session。
func HasSession() bool {
	return Session() != ""
}

// InvalidateSession 清除仍等于 rejectedToken 的失效 session 与 vault key。
// 若其它并发请求已经刷新了 session，则保持新值不变。
func InvalidateSession(rejectedToken string) bool {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if session == "" || session != rejectedToken {
		return false
	}
	expireSessionLocked()
	return true
}

// ClearSession 清除进程内 session 与 vault key（测试用）。
func ClearSession() {
	lockBypassForTest = false
	sessionMu.Lock()
	defer sessionMu.Unlock()
	expireSessionLocked()
}

func notifySessionChangedLocked() {
	close(sessionChanged)
	sessionChanged = make(chan struct{})
}

// WaitForInstanceUnlock waits until both the Bitwarden session and vault key
// are present. Every state mutation broadcasts so no Authenticate race is lost.
func WaitForInstanceUnlock(ctx context.Context) error {
	for {
		sessionMu.Lock()
		if sessionLiveLocked() && len(userKey) == 64 {
			sessionMu.Unlock()
			return nil
		}
		changed := sessionChanged
		sessionMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
