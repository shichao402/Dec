package app

import (
	"fmt"
	"os"
	"os/exec"
)

// LocatePKV 定位 pkv 可执行文件，并构造一个把 stdin/stdout/stderr 绑定到当前进程的
// *exec.Cmd。命令本身不 Run，由调用方（通常是 TUI 通过 tea.ExecProcess 挂起执行）负责启动。
//
// 仅搜索 $PATH；未找到时返回带中文说明的错误，调用方可直接把 err.Error() 原样展示。
func LocatePKV(args ...string) (*exec.Cmd, error) {
	path, err := exec.LookPath("pkv")
	if err != nil {
		return nil, fmt.Errorf("未找到 pkv 可执行文件，请确认 pkv 已安装并在 $PATH 中")
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, nil
}
