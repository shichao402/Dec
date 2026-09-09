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
	launch            = launchInstalledConsole
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

func OpenUnlockLocal() error {
	if !Available() {
		return ErrNonInteractive
	}
	return launch()
}

func launchInstalledConsole() error {
	path, err := findConsoleExecutable()
	if err != nil {
		return err
	}
	return startDetached(path, UnlockLocalFlag)
}

// SetLaunchForTest replaces the Console launcher and returns a restore function.
func SetLaunchForTest(fn func() error) func() {
	old := launch
	launch = fn
	return func() { launch = old }
}
