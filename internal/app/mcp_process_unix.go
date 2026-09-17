//go:build !windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func listMCPProcessesOS() ([]mcpProcess, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("读取 /proc 失败: %w", err)
	}
	out := make([]mcpProcess, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		parts := strings.Split(string(data), "\x00")
		var args []string
		for _, p := range parts {
			if p != "" {
				args = append(args, p)
			}
		}
		if len(args) == 0 {
			continue
		}
		out = append(out, mcpProcess{PID: pid, CmdLine: strings.Join(args, " ")})
	}
	return out, nil
}

func killMCPProcessOS(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("无效 pid")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("SIGTERM pid=%d: %w", pid, err)
	}
	return nil
}
