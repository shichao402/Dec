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

// openTTYFunc 是 /dev/tty 打开钩子，方便测试 mock。
// 默认实现：在 Unix 上打开 /dev/tty；Windows 或不可用时返回 (nil, err)。
var openTTYFunc = func() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}

// LocatePKVInteractive 与 LocatePKV 同，但额外把 stdin/stderr 切到 /dev/tty
// （绕开 bubbletea 持有的 fd 0/2，避免 raw 模式残留导致 bw 读到错乱密码字节）。
//
// 返回的 cleanup 在调用方拿到 cmd.Run() 结果之后调用，用于关闭 /dev/tty 文件句柄。
// 即便 cmd 失败也要 cleanup。如果 /dev/tty 不可用（Windows 或无 controlling tty），
// 会回退到 os.Stdin / os.Stderr，cleanup 是 no-op。
//
// stdout 仍为 os.Stdout（由调用方按需覆盖，比如 unlock 流程要换成 bytes.Buffer）。
func LocatePKVInteractive(args ...string) (*exec.Cmd, func(), error) {
	cmd, err := LocatePKV(args...)
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := attachTTYIfAvailable(cmd)
	return cmd, cleanup, nil
}

// LocatePKVInteractiveWithEnv 是 LocatePKVInteractive 的 env 注入版本。
// 语义同 LocatePKVWithEnv + LocatePKVInteractive 的并集。
func LocatePKVInteractiveWithEnv(extraEnv []string, args ...string) (*exec.Cmd, func(), error) {
	cmd, cleanup, err := LocatePKVInteractive(args...)
	if err != nil {
		return nil, cleanup, err
	}
	env := append(os.Environ(), extraEnv...)
	cmd.Env = env
	return cmd, cleanup, nil
}

// attachTTYIfAvailable 尝试打开 /dev/tty，把 cmd.Stdin / cmd.Stderr 切过去。
// 不影响 cmd.Stdout（调用方可能挂了 buffer）。返回的 cleanup 关闭打开的 tty 文件。
// 打不开 /dev/tty（Windows、daemon、无 controlling tty）时维持原 Stdin/Stderr，cleanup no-op。
func attachTTYIfAvailable(cmd *exec.Cmd) func() {
	tty, err := openTTYFunc()
	if err != nil || tty == nil {
		return func() {}
	}
	cmd.Stdin = tty
	cmd.Stderr = tty
	return func() { _ = tty.Close() }
}

// BuildPKVUnlockCmd 构造一个用于 `pkv unlock` 的 cmd：
//
//	stdout = 新建的 *bytes.Buffer（捕获 session 字符串）
//	stderr = /dev/tty（让 bw 的密码提示对用户可见，绕开 bubbletea 占用的 fd 2）
//	stdin  = /dev/tty（让用户能输 master password，绕开 bubbletea 持有的 fd 0）
//
// 为什么不直接 os.Stdin / os.Stderr：bubbletea 启动时把 stdin 切到 raw 模式，
// 即便 ReleaseTerminal 调了 term.Restore，readLoop / cancelreader 仍可能与 bw
// 抢占 fd 0 字节流，导致 bw 读到的密码与用户实际输入对不上 → decryption failed。
// 直接打开 /dev/tty 拿一个干净的 fd 给 pkv/bw，可以彻底绕开这个抢占。
//
// /dev/tty 不可用时（Windows / 无 controlling tty / 重定向）回退到 os.Stdin/os.Stderr，
// 行为与历史版本一致；调用 cleanup 是 no-op。
//
// 调用方（TUI）负责：
//
//  1. 把 cmd 交给 tea.ExecProcess 挂起运行；
//  2. 在 callback 里调用 ParsePKVUnlockOutput(buf) 取 session；
//  3. 不论成功失败都调用 cleanup 关闭打开的 /dev/tty fd。
func BuildPKVUnlockCmd() (*exec.Cmd, *bytes.Buffer, func(), error) {
	path, err := exec.LookPath("pkv")
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("未找到 pkv 可执行文件，请确认 pkv 已安装并在 $PATH 中")
	}
	buf := &bytes.Buffer{}
	cmd := exec.Command(path, "unlock")
	// 默认先挂上 stdin/stderr 兜底，再尝试切到 /dev/tty。
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = buf
	cleanup := attachTTYIfAvailable(cmd)
	return cmd, buf, cleanup, nil
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
