//go:build !windows

package sysproc

import "os/exec"

// Hide 在非 Windows 平台无需处理：子进程不会带出图形窗口。
func Hide(cmd *exec.Cmd) {}
