//go:build !windows

package consoleopen

import (
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

func startDetached(path string, args ...string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 Dec Console 失败: %w", err)
	}
	return cmd.Process.Release()
}
