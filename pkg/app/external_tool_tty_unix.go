//go:build !windows

package app

import "os"

// defaultOpenTTY 在 Unix 上打开 /dev/tty，返回同一个 fd 当 stdin/stderr 用。
// /dev/tty 是当前进程的 controlling terminal，bubbletea 即便把 fd 0/2 切到 raw
// 模式，这里独立打开的 fd 仍然是干净的 —— bw 用它读 master password 不会被抢字节。
func defaultOpenTTY() (*os.File, *os.File, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	// in == out：调用方 cleanup 时只 Close 一次。
	return f, f, nil
}
