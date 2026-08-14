//go:build windows

package sysproc

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Hide 让子进程使用不可见的控制台。
//
// CREATE_NO_WINDOW 与 DETACHED_PROCESS / CREATE_NEW_CONSOLE 互斥，设置后者会
// 让本 flag 失效，因此调用方不要再叠加那两个 flag。
func Hide(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
