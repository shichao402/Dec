// Package sysproc 统一创建「不弹控制台窗口」的子进程。
//
// dec-server 以脱离控制台的方式常驻，自身没有 console。Windows 上父进程无
// console 时，每个 console 子进程（git 等）都会新建一个自己的控制台窗口。
// 所有非交互式子进程都必须经由本包创建。
//
// 需要用户看见的进程（编辑器等）不要用本包。
package sysproc

import (
	"context"
	"os/exec"
)

// Command 等价于 exec.Command，但保证子进程不弹出控制台窗口。
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	Hide(cmd)
	return cmd
}

// CommandContext 等价于 exec.CommandContext，但保证子进程不弹出控制台窗口。
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	Hide(cmd)
	return cmd
}
