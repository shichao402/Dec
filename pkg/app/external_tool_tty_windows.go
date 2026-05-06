//go:build windows

package app

import (
	"fmt"
	"os"
)

// defaultOpenTTY 在 Windows 上打开当前进程的 console 句柄。
//
// Windows 的 console 不是单一双向设备，输入和输出走两个 magic 文件名：
//   - CONIN$ ：当前控制台输入（用于读 master password）
//   - CONOUT$：当前控制台输出（bw 的 prompt 文字写到这里）
//
// 这两个句柄独立于 fd 0 / fd 2，bubbletea 即便持有 stdin / stderr 也不会污染
// 这里独立打开的句柄。等同 Unix 上 /dev/tty 的作用。
//
// 任一打开失败都把已开的关掉，整体返回 error，调用方走 fallback（os.Stdin/Stderr）。
func defaultOpenTTY() (*os.File, *os.File, error) {
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open CONIN$: %w", err)
	}
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		_ = in.Close()
		return nil, nil, fmt.Errorf("open CONOUT$: %w", err)
	}
	return in, out, nil
}
