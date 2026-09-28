// Package consoleopen launches the installed Dec Console with a non-sensitive
// flag. It never transports credentials or service tokens, and never opens a
// browser or OS URL handler.
package consoleopen

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

const UnlockLocalFlag = "--unlock-local"

var (
	ErrNonInteractive = errors.New("当前环境不可启动 Dec Console")
	launch            = startInstalled
)

// Available reports whether this process is allowed to open a desktop app.
// CI, tests and explicitly non-interactive processes must never show UI.
func Available() bool {
	if testing.Testing() ||
		strings.TrimSpace(os.Getenv("DEC_NO_CONSOLE_LAUNCH")) == "1" ||
		strings.TrimSpace(os.Getenv("CI")) != "" {
		return false
	}
	if runtime.GOOS == "linux" &&
		strings.TrimSpace(os.Getenv("DISPLAY")) == "" &&
		strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) == "" {
		return false
	}
	return true
}

func Open() error {
	if !Available() {
		return ErrNonInteractive
	}
	return launch()
}

func OpenUnlockLocal() error {
	if !Available() {
		return ErrNonInteractive
	}
	return launch(UnlockLocalFlag)
}

// OpenCredentialRequest 以与 unlock 相同的 flag 唤起 Console（ADR 0036 阶段 B，
// 方案 1：共享唤起意图）。Console 启动后先调 PullCredentialRequest：有挂起凭据
// 请求则渲染对应控件，无则回落解锁页。argv 永远不带业务参数与秘密。
func OpenCredentialRequest() error {
	if !Available() {
		return ErrNonInteractive
	}
	return launch(UnlockLocalFlag)
}

func startInstalled(args ...string) error {
	path, err := findConsoleExecutable()
	if err != nil {
		return err
	}
	return startDetached(path, args...)
}

// SetLaunchForTest replaces the Console launcher and returns a restore function.
func SetLaunchForTest(fn func() error) func() {
	old := launch
	launch = func(args ...string) error { return fn() }
	return func() { launch = old }
}
