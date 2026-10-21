package agent

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/shichao402/Dec/internal/sysproc"
)

// 系统通知（需求 2.5、12.3、19.5）。
//
// 不引第三方依赖：三平台各自的一行命令就够了，而且代理是“下载即跑”的
// 单文件程序，多一个 cgo 依赖就多一类装不上的机器。

// SystemNotifier 用平台自带方式发通知。
type SystemNotifier struct {
	// Logf 用于记录失败，通知失败不应影响主流程。
	Logf func(format string, args ...any)
}

// Notify 发一条系统通知。
//
// 失败只记日志：通知是“锦上添花”的提示，代理的正文职责是保持落点与云端一致。
func (n *SystemNotifier) Notify(title, body string) error {
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = run("osascript", "-e",
			fmt.Sprintf(`display notification %s with title %s`, quoteAppleScript(body), quoteAppleScript(title)))
	case "linux":
		err = run("notify-send", title, body)
	case "windows":
		err = run("powershell", "-NoProfile", "-Command",
			fmt.Sprintf(`[reflection.assembly]::LoadWithPartialName('System.Windows.Forms');`+
				`$n=New-Object System.Windows.Forms.NotifyIcon;`+
				`$n.Icon=[System.Drawing.SystemIcons]::Information;$n.Visible=$true;`+
				`$n.ShowBalloonTip(5000,%s,%s,'Info')`, psQuote(title), psQuote(body)))
	default:
		return nil
	}
	if err != nil && n.Logf != nil {
		n.Logf("发送系统通知失败: %v", err)
	}
	return err
}

// run 执行命令但不收集输出：通知不需要返回值。
func run(name string, args ...string) error {
	cmd := sysproc.Command(name, args...)
	return cmd.Run()
}

// quoteAppleScript 把字符串变成 AppleScript 字面量。
func quoteAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// psQuote 把字符串变成 PowerShell 单引号字面量。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
