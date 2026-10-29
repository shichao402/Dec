//go:build !windows

// 交互式收集 Bitwarden 凭据。
//
// Windows 版见 creds_windows.go：那里的构建是 GUI 子系统，交互前需要先
// 附加控制台，其余逻辑与本文件一致。
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

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
	in := bufio.NewReader(os.Stdin)
	out := &creds{Email: email, Password: password, TOTP: totp}

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
