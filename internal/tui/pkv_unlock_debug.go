package tui

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// pkvUnlockDebug 仅在环境变量 DEC_DEBUG_UNLOCK=1 时启用，把 pkv unlock 关键节点
// 写入 <home>/.dec/pkv-unlock-debug.log，用于在 Windows 上定位 stdin race 假设。
//
// 写入失败完全静默，绝不影响主流程；普通用户不开启时也是 zero-cost 路径。
type pkvUnlockDebugLogger struct {
	mu      sync.Mutex
	file    *os.File
	enabled bool
	once    sync.Once
}

var pkvDebug = &pkvUnlockDebugLogger{}

func (l *pkvUnlockDebugLogger) ensure() {
	l.once.Do(func() {
		if os.Getenv("DEC_DEBUG_UNLOCK") != "1" {
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		dir := filepath.Join(home, ".dec")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(dir, "pkv-unlock-debug.log"),
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		l.file = f
		l.enabled = true
		fmt.Fprintf(l.file, "\n========== %s session start (os=%s arch=%s) ==========\n",
			time.Now().Format(time.RFC3339Nano), runtime.GOOS, runtime.GOARCH)
	})
}

// log 单行写入；失败完全静默。
func (l *pkvUnlockDebugLogger) log(format string, args ...any) {
	l.ensure()
	if !l.enabled {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.file, "[%s] ", time.Now().Format("15:04:05.000"))
	fmt.Fprintf(l.file, format, args...)
	if len(format) == 0 || format[len(format)-1] != '\n' {
		fmt.Fprintln(l.file)
	}
}

// dumpBuffer 把 buf 前 maxBytes 字节做 hex+ascii 双视图。Windows 上 readLoop
// 抢字节会留下蛛丝马迹（出现 ANSI 转义、键码字节、或正常 ASCII 之外的乱码）。
func (l *pkvUnlockDebugLogger) dumpBuffer(label string, data []byte, maxBytes int) {
	l.ensure()
	if !l.enabled {
		return
	}
	if maxBytes > len(data) {
		maxBytes = len(data)
	}
	slice := data[:maxBytes]
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.file, "[%s] %s len=%d head_hex=%s head_ascii=%q\n",
		time.Now().Format("15:04:05.000"), label, len(data), hex.EncodeToString(slice), string(slice))
}
