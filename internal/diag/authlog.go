package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// 认证诊断写到 ${DEC_HOME}/logs/auth.log。
// 只记阶段和失败原因。主密码、TOTP、session、vault key、remember token 不得进入这里。

var (
	authLogMu sync.Mutex
	// access_token=、"twoFactorToken": 这类赋值抹掉。普通英文 "password is incorrect" 没有等号或冒号，保留。
	authSecretValue = regexp.MustCompile(`(?i)(access_token|refresh_token|twoFactorToken|two_factor_token|twoFactorRemember|masterpasswordhash|passwordhash|password|totp|session)(["']?\s*[:=]\s*["']?)[^\s"',}]+`)
)

// AuthLogPath 返回认证日志绝对路径。DEC_HOME 未设置时用 ~/.dec。
func AuthLogPath() (string, error) {
	root := os.Getenv("DEC_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".dec")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "logs", "auth.log"), nil
}

// AuthLog 追加一行认证诊断。写入失败不影响解锁本身。
func AuthLog(format string, args ...any) {
	line := redactAuth(fmt.Sprintf(format, args...))
	path, err := AuthLogPath()
	if err != nil {
		return
	}
	authLogMu.Lock()
	defer authLogMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s pid=%d %s\n", time.Now().Format(time.RFC3339), os.Getpid(), line)
}

func redactAuth(line string) string {
	return authSecretValue.ReplaceAllString(line, "${1}${2}[redacted]")
}
