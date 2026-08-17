//go:build !windows

package mcp

import (
	"os"
	"syscall"
)

func exitSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// processAlive 用 signal 0 探测目标进程是否存活（不实际投递信号）。
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
