//go:build windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/term"
)

// 交互式收集 Bitwarden 凭据。
//
// 为什么不接受命令行参数：`--password xxx` 会进 shell 历史、进 ps 输出，
// 主密码是这套系统最后一道防线，不能为了少写几行代码把它留在那些地方。
// 环境变量 DEC_BW_PASSWORD 保留给无桌面环境的脚本场景，但仍然推荐交互输入。

// creds 是一次交互收集到的凭据。
type creds struct {
	Email    string
	Password string
	TOTP     string
}

// collectCreds 交互式收集凭据。
//
// 已经通过参数或环境变量给出的项不会再问，因此这三者可以混用：
// 例如脚本里给 email，密码仍然交互输入。
func collectCreds(email, password, totp string) (*creds, error) {
	out := &creds{Email: email, Password: password, TOTP: totp}

	// 只有在真的有东西要问时才去要控制台：凭据齐全时（例如安装脚本已通过
	// 参数与环境变量给全）不该弹窗。
	if out.Email == "" || out.Password == "" || out.TOTP == "" {
		if err := ensureConsole(); err != nil {
			return nil, err
		}
	}

	in := bufio.NewReader(os.Stdin)

	if out.Email == "" {
		fmt.Fprint(os.Stderr, "Bitwarden 邮箱: ")
		line, err := in.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("读取邮箱失败: %w", err)
		}
		out.Email = strings.TrimSpace(line)
		if out.Email == "" {
			return nil, fmt.Errorf("邮箱不能为空")
		}
	}
	if out.Password == "" {
		fmt.Fprint(os.Stderr, "Bitwarden 主密码（输入时不回显）: ")
		// 只在真正的终端上用无回显读取；管道输入时退化为普通读取，
		// 否则脚本无法通过 stdin 提供密码。
		var pw string
		if term.IsTerminal(int(os.Stdin.Fd())) {
			raw, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return nil, fmt.Errorf("读取主密码失败: %w", err)
			}
			pw = string(raw)
		} else {
			line, err := in.ReadString('\n')
			if err != nil && err != io.EOF {
				return nil, fmt.Errorf("读取主密码失败: %w", err)
			}
			pw = strings.TrimSpace(line)
		}
		if pw == "" {
			return nil, fmt.Errorf("主密码不能为空")
		}
		out.Password = pw
	}
	if out.TOTP == "" {
		fmt.Fprint(os.Stderr, "两步验证码（没有配置可留空）: ")
		line, err := in.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("读取验证码失败: %w", err)
		}
		// 只去掉空白与连字符，用户常按界面上的 "123 456" 格式输入。
		out.TOTP = strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(line))
	}
	return out, nil
}

// ensureConsole 保证交互输入有一个可用的控制台。
//
// dec-agent 的 Windows 构建是 GUI 子系统（-H windowsgui），否则计划任务拉起
// 常驻进程时会闪一个黑框。代价是 GUI 子系统启动时没有控制台，标准句柄为 0，
// 直接 ReadString 只会拿到 EOF，用户看到的却是「邮箱不能为空」这种误导性报错。
//
// 因此交互前先要一个控制台：
//  1. AttachConsole(ATTACH_PARENT_PROCESS) 挂到父进程的控制台上——从 PowerShell
//     或 cmd 里启动时就是用户眼前那个窗口，输入体验与 console 版完全一致；
//  2. 没有父控制台（从资源管理器双击、计划任务启动）才 AllocConsole 自己开一个。
//
// 要来控制台后必须重建标准句柄：AttachConsole 不会自动把 os.Stdin 指向它，
// 不重开的话 Go 侧读到的仍是原来的无效句柄。
func ensureConsole() error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")

	// 已经有可用输入（console 子系统构建、或已被重定向）就不要动它。
	if hasStdin() {
		return nil
	}

	const attachParentProcess = ^uintptr(0) // (DWORD)-1
	const stdInputHandle = ^uintptr(10) + 1 // -10
	const stdOutputHandle = ^uintptr(11) + 1
	const stdErrorHandle = ^uintptr(12) + 1

	attached := false
	r, _, _ := kernel32.NewProc("AttachConsole").Call(attachParentProcess)
	if r != 0 {
		attached = true
	} else {
		// 没有父控制台（双击启动、计划任务）：自己开一个。
		if r, _, _ := kernel32.NewProc("AllocConsole").Call(); r == 0 {
			// 开不出来就退回原行为，让上层报出真实错误，不要掩盖失败。
			return nil
		}
	}
	_ = attached

	getStdHandle := kernel32.NewProc("GetStdHandle")
	reopen := func(which uintptr, name string, flags int) {
		h, _, _ := getStdHandle.Call(which)
		if h == 0 || h == ^uintptr(0) { // NULL 或 INVALID_HANDLE_VALUE
			return
		}
		f := os.NewFile(h, name)
		switch which {
		case stdInputHandle:
			os.Stdin = f
		default:
			// stdout/stderr 只用于回显，失败不影响读取，忽略错误即可。
			_ = flags
		}
	}
	reopen(stdInputHandle, "CONIN$", os.O_RDONLY)

	// stdout/stderr 也要指过去，否则交互提示打印不进刚开的窗口，
	// 用户面对一个空窗口不知道该输什么。
	outH, _, _ := getStdHandle.Call(stdOutputHandle)
	if outH != 0 && outH != ^uintptr(0) {
		os.Stdout = os.NewFile(outH, "CONOUT$")
	}
	errH, _, _ := getStdHandle.Call(stdErrorHandle)
	if errH != 0 && errH != ^uintptr(0) {
		os.Stderr = os.NewFile(errH, "CONOUT$")
	}

	return nil
}

// hasStdin 报告标准输入是否已经可用。
//
// GUI 子系统下 os.Stdin 不是 nil 但句柄无效，读它会返回 EOF 而不是报错；
// 这里只探句柄是否有效，不去真的读：试探性读会吞掉一个字节，
// 用户输入的第一个字符就没了。
func hasStdin() bool {
	if os.Stdin == nil {
		return false
	}
	// 句柄是 0 或 INVALID_HANDLE_VALUE 就是没有控制台。
	fd := os.Stdin.Fd()
	if fd == 0 || fd == ^uintptr(0) {
		return false
	}
	// 再确认它确实是个能读的句柄：管道用 PeekNamedPipe（不消耗数据），
	// 字符设备（真实控制台）直接认为可用。
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	var avail uint32
	r, _, _ := kernel32.NewProc("PeekNamedPipe").Call(
		fd, 0, 0, 0, uintptr(unsafe.Pointer(&avail)), 0)
	if r != 0 {
		return true
	}
	var mode uint32
	if err := syscall.GetConsoleMode(syscall.Handle(fd), &mode); err == nil {
		return true
	}
	// 不是管道也不是控制台（例如 NUL、已关闭的句柄），视为不可用。
	return false
}
