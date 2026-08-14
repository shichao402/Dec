//go:build windows

package service

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detachedProcessAttributes 让 dec-server 常驻且不占用调用方终端。
//
// 这里用 CREATE_NO_WINDOW 而不是 DETACHED_PROCESS：后者会让服务进程完全没有
// console，于是它拉起的每个 git 子进程都要自建控制台，屏幕上会冒出一堆窗口。
// CREATE_NO_WINDOW 给服务一个不可见的独立 console，子进程继承它即可静默运行。
func detachedProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
	}
}
