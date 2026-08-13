//go:build !windows

package secrets

import (
	"fmt"
	"os"
)

func restrictFilePermissions(path string, mode os.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("设置文件权限失败: %w", err)
	}
	return nil
}

func restrictDirPermissions(dir string) error {
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("设置目录权限失败: %w", err)
	}
	return nil
}
