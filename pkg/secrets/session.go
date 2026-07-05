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

// ClearSession 清除进程内 session 与 vault key（测试用）。
func ClearSession() {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	session = ""
	userKey = nil
}
