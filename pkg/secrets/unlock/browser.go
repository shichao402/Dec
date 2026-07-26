package unlock

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// BrowserOpener 打开系统浏览器；测试时可注入 mock。
type BrowserOpener func(url string) error

var defaultBrowserOpener BrowserOpener = openSystemBrowser

func openSystemBrowser(url string) error {
	// 兜底：即便有调用方绕过 Run 的检查，也不允许测试进程弹出浏览器。
	if !WebUnlockAllowed() {
		return ErrWebUnlockBlocked
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// 使用绝对路径与 -g，避免 TUI 子进程 PATH 不完整或阻塞前台。
		cmd = exec.Command("/usr/bin/open", "-g", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开浏览器失败: %w", err)
	}
	return nil
}
