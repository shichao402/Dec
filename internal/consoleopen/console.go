// Package consoleopen opens the installed Dec Console at a non-sensitive
// application intent. It never transports credentials or service tokens.
package consoleopen

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

const UnlockLocalURI = "dec://unlock/local"

var (
	ErrNonInteractive = errors.New("当前环境不可启动 Dec Console")
	openURI           = openSystemURI
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
	return openURI(UnlockLocalURI)
}

// SetOpenURIForTest replaces the OS launcher and returns a restore function.
func SetOpenURIForTest(fn func(string) error) func() {
	old := openURI
	openURI = fn
	return func() { openURI = old }
}
