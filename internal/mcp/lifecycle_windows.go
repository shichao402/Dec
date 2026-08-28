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

func processIdentity(pid int) (uint64, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil || code != stillActive {
		return 0, false
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	return uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime), true
}
