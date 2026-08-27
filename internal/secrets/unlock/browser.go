package unlock

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/shichao402/Dec/internal/sysproc"
)

// BrowserOpener 打开系统浏览器；测试时可注入 mock。
type BrowserOpener func(url string) error

var defaultBrowserOpener BrowserOpener = openSystemBrowser

func openSystemBrowser(url string) error {
	// 兜底：即便有调用方绕过 Run 的检查，也不允许测试进程弹出浏览器。
	if !WebUnlockAllowed() {
		logWebUnlockDecision(false, "有调用方绕过 Run 直接请求打开系统浏览器")
		return ErrWebUnlockBlocked
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// 使用绝对路径与 -g，避免 TUI 子进程 PATH 不完整或阻塞前台。
		cmd = sysproc.Command("/usr/bin/open", "-g", url)
	case "windows":
		// 这里只是让 shell 转交系统默认浏览器，cmd 本身不该露脸。
		cmd = sysproc.Command("cmd", "/c", "start", "", url)
	default:
		cmd = sysproc.Command("xdg-open", url)
	}
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开浏览器失败: %w", err)
	}
	return nil
}
