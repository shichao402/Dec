package secrets

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/config"
)

// DefaultSessionTTL 是一次解锁在本机的默认有效期，可由 ~/.dec/config.yaml 的
// session_timeout 覆盖。到期即丢弃进程内 session 与 vault key，云端 access_token
// 是否仍然有效不参与判断；保留了主密码时由 TryAutoUnlock 静默重登录。
const DefaultSessionTTL = 4 * time.Hour

var (
	sessionMu       sync.Mutex
	session         string
	userKey         []byte
	sessionDeadline time.Time
	sessionTimedOut bool
	sessionNow      = time.Now
	sessionChanged  = make(chan struct{})
	sessionTTL      = configuredSessionTTL
)

func configuredSessionTTL() time.Duration {
	cfg, err := config.LoadGlobalConfig()
	if err != nil || cfg == nil {
		return DefaultSessionTTL
	}
	ttl, err := time.ParseDuration(strings.TrimSpace(cfg.SessionTimeout))
	if err != nil || ttl <= 0 {
		return DefaultSessionTTL
	}
	return ttl
}

// SessionTTL 返回本机当前配置的解锁有效期。
func SessionTTL() time.Duration {
	return sessionTTL()
}

// SetSession 写入进程内 Bitwarden session（禁止落盘），到 session_timeout 后失效。
func SetSession(token string) {
	ttl := sessionTTL()
	sessionMu.Lock()
	defer sessionMu.Unlock()
	setSessionLocked(token, ttl)
	notifySessionChangedLocked()
}

func setSessionLocked(token string, ttl time.Duration) {
	session = token
	sessionTimedOut = false
	if token == "" {
		sessionDeadline = time.Time{}
		return
	}
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	sessionDeadline = sessionNow().Add(ttl)
}

func expireSessionLocked(timedOut bool) {
	session = ""
	userKey = nil
	sessionDeadline = time.Time{}
	sessionTimedOut = timedOut
	notifySessionChangedLocked()
}

func dropIfSessionExpiredLocked() {
	if session == "" {
		return
	}
	if !sessionDeadline.IsZero() && !sessionNow().Before(sessionDeadline) {
		expireSessionLocked(true)
	}
}

// SessionTimedOut 表示当前锁定是本机解锁有效期到点导致的，而不是云端 401、
// 服务刚启动或显式清理。
func SessionTimedOut() bool {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	dropIfSessionExpiredLocked()
	return sessionTimedOut
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
	expireSessionLocked(false)
	return true
}

// ClearSession 清除进程内 session、vault key 与保留的主密码（测试用）。
func ClearSession() {
	lockBypassForTest = false
	ClearRetainedPassword()
	sessionMu.Lock()
	defer sessionMu.Unlock()
	expireSessionLocked(false)
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
