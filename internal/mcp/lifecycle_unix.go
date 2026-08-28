//go:build !windows

package mcp

import (
	"os"
	"syscall"
)

func exitSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func processIdentity(pid int) (uint64, bool) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, false
	}
	if proc.Signal(syscall.Signal(0)) != nil {
		return 0, false
	}
	return uint64(pid), true
}
