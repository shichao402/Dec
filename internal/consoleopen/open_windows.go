//go:build windows

package consoleopen

import (
	"fmt"

	"github.com/shichao402/Dec/internal/sysproc"
)

func openSystemURI(uri string) error {
	cmd := sysproc.Command("cmd", "/c", "start", "", uri)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 Dec Console 失败: %w", err)
	}
	return cmd.Process.Release()
}
