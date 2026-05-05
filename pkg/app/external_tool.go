package app

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

// LocatePKVWithEnv 行为同 LocatePKV，但额外把 extraEnv 追加到 cmd.Env 上。
// extraEnv 形如 []string{"BW_SESSION=xxx"}；当前进程 os.Environ() 作为基线。
// 用于把 TUI 缓存的 BW_SESSION 透传给 pkv get/list 等子命令，避免它们再问 master password。
func LocatePKVWithEnv(extraEnv []string, args ...string) (*exec.Cmd, error) {
	cmd, err := LocatePKV(args...)
	if err != nil {
		return nil, err
	}
	// 基线继承当前进程环境，保证 PATH / HOME / 语言环境这些基础变量仍然生效。
	// extraEnv 追加在后面，后面的同名键会覆盖前面的（exec.Cmd 行为）。
	env := append(os.Environ(), extraEnv...)
	cmd.Env = env
	return cmd, nil
}

// BuildPKVUnlockCmd 构造一个用于 `pkv unlock` 的 cmd：
//
//	stdout = 新建的 *bytes.Buffer（捕获 session 字符串）
//	stderr = os.Stderr（让 bw 的密码提示对用户可见）
//	stdin  = os.Stdin（让用户能输 master password）
//
// 调用方负责把 cmd 交给 tea.ExecProcess，在 callback 里调 ParsePKVUnlockOutput(buf)。
// 未找到 pkv 时返回 (nil, nil, error)。
//
// tea.ExecProcess 只在 cmd 的 Std* 为 nil 时才覆盖为 tty，所以这里预先挂好的 Stdout
// (bytes.Buffer) 会被保留，Stderr/Stdin (os.Stderr/os.Stdin) 会被 bubbletea 识别为
// 已赋值也不再覆盖。bw 从 tty (stderr/stdin) 问密码，pkv 从 stdout 输出 session。
func BuildPKVUnlockCmd() (*exec.Cmd, *bytes.Buffer, error) {
	path, err := exec.LookPath("pkv")
	if err != nil {
		return nil, nil, fmt.Errorf("未找到 pkv 可执行文件，请确认 pkv 已安装并在 $PATH 中")
	}
	buf := &bytes.Buffer{}
	cmd := exec.Command(path, "unlock")
	cmd.Stdin = os.Stdin
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr
	return cmd, buf, nil
}

// ParsePKVUnlockOutput 把 BuildPKVUnlockCmd 的 buffer 内容 trim 换行/空白后返回 BW_SESSION。
//
// pkv unlock 成功时 stdout 是一行 bare session 字符串（不包含空白、换行），
// 所有诊断/交互文本都走 stderr。这里做三层校验，任一不满足都当成解锁失败：
//
//  1. buffer 非 nil；
//  2. trim 后非空；
//  3. trim 后不含空白字符（空格/换行）——避免误把 stderr 串进 stdout 的异常输出
//     或者带行尾诊断的输出当成 session 缓存下去。
//
// 即便 exit code == 0 但 stdout 形态不对，也拒绝写入 session。
func ParsePKVUnlockOutput(buf *bytes.Buffer) (string, error) {
	if buf == nil {
		return "", fmt.Errorf("pkv unlock 未捕获任何输出")
	}
	session := strings.TrimSpace(buf.String())
	if session == "" {
		return "", fmt.Errorf("pkv unlock 未输出 session，可能未成功解锁")
	}
	if strings.ContainsAny(session, " \t\r\n") {
		return "", fmt.Errorf("pkv unlock 输出格式异常（包含空白字符），不是有效的 BW_SESSION")
	}
	return session, nil
}
