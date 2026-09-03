//go:build !windows

package consoleopen

import (
	"fmt"
	"runtime"

	"github.com/shichao402/Dec/internal/sysproc"
)

func openSystemURI(uri string) error {
	name := "xdg-open"
	args := []string{uri}
	if runtime.GOOS == "darwin" {
		name = "/usr/bin/open"
		args = []string{"-g", uri}
	}
	cmd := sysproc.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 Dec Console 失败: %w", err)
	}
	return cmd.Process.Release()
}
