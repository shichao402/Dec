//go:build windows

package mcp

import (
	"os"

	"golang.org/x/sys/windows"
)

// stillActive 是 Windows GetExitCodeProcess 对「仍在运行」的返回码（STILL_ACTIVE）。
const stillActive = 259

func exitSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

// processAlive 通过 OpenProcess + GetExitCodeProcess 判断目标进程是否仍在运行。
// PID 已不存在时 OpenProcess 失败，视为已退出。
func processAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}
