//go:build windows

package unlock

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func parentProcessPath(pid int) string {
	if pid <= 0 {
		return ""
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return ""
	}
	path := windows.UTF16ToString(buf[:size])
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
