//go:build windows

package freshness

import "os/exec"

// setDetached 在 Windows 上暂不做进程分离。StartBackgroundCheck 已在入口 early return，
// 这里保留空实现只是为了让 Windows 构建能过。后续若要支持，改成
// cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}。
func setDetached(cmd *exec.Cmd) {}
