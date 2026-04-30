//go:build !windows

package freshness

import (
	"os/exec"
	"syscall"
)

// setDetached 让子进程成为新会话 leader，和父进程的会话、终端、进程组解耦。
// 父进程退出后子进程继续存活，直到 RunBackgroundCheck 自己执行完。
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
