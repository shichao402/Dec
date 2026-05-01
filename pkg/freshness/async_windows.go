//go:build windows

package freshness

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// setDetached 让 Windows 子进程脱离父控制台、独立一个 process group。
//
// DETACHED_PROCESS：不继承父控制台，父进程退出不会把子进程一起带走。
// CREATE_NEW_PROCESS_GROUP：独立 process group，父终端 Ctrl+C / Ctrl+Break
// 不会传播到子进程（没这个子进程会被 Ctrl+C 干掉）。
//
// 这两个 flag 组合等价于 Unix 分支里的 Setsid——让子进程完全脱离父会话。
func setDetached(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP
}
