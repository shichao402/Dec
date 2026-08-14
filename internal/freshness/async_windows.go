//go:build windows

package freshness

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// setDetached 让 Windows 子进程独立于父终端运行，且不弹出控制台窗口。
//
// CREATE_NEW_PROCESS_GROUP：独立 process group，父终端 Ctrl+C / Ctrl+Break
// 不会传播到子进程（没这个子进程会被 Ctrl+C 干掉）。
// CREATE_NO_WINDOW：子进程拿到不可见的独立 console。不能用 DETACHED_PROCESS，
// 那样子进程没有 console，它再拉起的 git 会自建控制台窗口。
func setDetached(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW
}
