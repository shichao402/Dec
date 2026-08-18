package secrets

import "sync"

var (
	sessionMu sync.RWMutex
	session   string
	userKey   []byte
)

// SetSession 写入进程内 Bitwarden session（禁止落盘）。
func SetSession(token string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	session = token
}

// SetUserKey 写入进程内 vault symmetric key（禁止落盘）。
func SetUserKey(key []byte) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if len(key) == 0 {
		userKey = nil
		return
	}
	copied := make([]byte, len(key))
	copy(copied, key)
	userKey = copied
}

// UserKey 返回当前进程内 vault symmetric key 副本。
func UserKey() []byte {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	if len(userKey) == 0 {
		return nil
	}
	copied := make([]byte, len(userKey))
	copy(copied, userKey)
	return copied
}

// HasUserKey 判定进程内是否已有 vault symmetric key。
func HasUserKey() bool {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	return len(userKey) == 64
}

// Session 返回当前进程内 session；无 session 时为空字符串。
func Session() string {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	return session
}

// HasSession 判定进程内是否已有 session。
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
	session = ""
	userKey = nil
	return true
}

// ClearSession 清除进程内 session 与 vault key（测试用）。
func ClearSession() {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	session = ""
	userKey = nil
}
