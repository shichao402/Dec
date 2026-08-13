package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 启动诊断：固定写到 %TEMP%/dec-startup.log（或 TMPDIR）。
// 专供排查「Loading project overview…」卡死；不走 TUI logs，避免渲染前看不见。

var (
	startupOnce sync.Once
	startupMu   sync.Mutex
	startupFile *os.File
	startupT0   = time.Now()
)

// StartupLogPath 返回诊断日志绝对路径。
func StartupLogPath() string {
	return filepath.Join(os.TempDir(), "dec-startup.log")
}

func ensureStartupFile() {
	startupOnce.Do(func() {
		path := StartupLogPath()
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		startupFile = f
		pid := os.Getpid()
		_, _ = fmt.Fprintf(f, "\n===== dec startup pid=%d t0=%s =====\n", pid, startupT0.Format(time.RFC3339Nano))
		_ = f.Sync()
	})
}

// StartupLog 追加一行带相对启动时刻的诊断日志。
func StartupLog(format string, args ...any) {
	ensureStartupFile()
	startupMu.Lock()
	defer startupMu.Unlock()
	if startupFile == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	elapsed := time.Since(startupT0).Milliseconds()
	_, _ = fmt.Fprintf(startupFile, "+%6dms | %s\n", elapsed, msg)
	_ = startupFile.Sync()
}

// StartupSpan 记录起止与耗时。
func StartupSpan(name string) func(result string) {
	StartupLog("BEGIN %s", name)
	start := time.Now()
	return func(result string) {
		StartupLog("END   %s elapsed=%dms %s", name, time.Since(start).Milliseconds(), result)
	}
}
