package secrets

import "sync"

var (
	sessionMu sync.RWMutex
	session   string
)

// SetSession 写入进程内 Bitwarden session（禁止落盘）。
func SetSession(token string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	session = token
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

// ClearSession 清除进程内 session（测试用）。
func ClearSession() {
	SetSession("")
}
