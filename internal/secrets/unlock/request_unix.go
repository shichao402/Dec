//go:build !windows

package unlock

import (
	"fmt"
	"os"
)

func parentProcessPath(pid int) string {
	if pid <= 0 {
		return ""
	}
	path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ""
	}
	return path
}
