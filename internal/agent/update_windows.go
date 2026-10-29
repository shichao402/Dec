//go:build windows

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// updateWindowsTask 是更新进程借用的一次性计划任务。
//
// 代理本身由计划任务 DecAgent 拉起，任务计划程序结束任务时会连同它的作业
// 对象里的子进程一起结束；更新进程如果是代理的子进程，在「停服务」那一步
// 就会被一起杀掉。把它放进一个独立的任务里运行，它就和 DecAgent 互不相干。
// 任务没有触发器，只在被 /Run 时执行一次；保留注册无副作用，下次更新覆盖。
const updateWindowsTask = "DecAgentUpdate"

func launchDetached(bin string, args []string) error {
	cmd, argLine := windowsExec(bin, args, "")
	if err := registerWindowsTask(updateWindowsTask, oneShotTaskXML(cmd, argLine, windowsUserID()), filepath.Dir(bin)); err != nil {
		return err
	}
	return runQuiet("schtasks", "/Run", "/TN", updateWindowsTask)
}

// oneShotTaskXML 生成没有触发器、限时 15 分钟的用户级任务。
func oneShotTaskXML(command, args, userID string) string {
	user := ""
	if userID != "" {
		user = "<UserId>" + xmlEscape(userID) + "</UserId>"
	}
	argsXML := ""
	if args != "" {
		argsXML = "\n      <Arguments>" + xmlEscape(args) + "</Arguments>"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Dec 代理自动更新（一次性）</Description>
  </RegistrationInfo>
  <Triggers />
  <Principals>
    <Principal id="Author">%s
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT15M</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>%s
    </Exec>
  </Actions>
</Task>
`, user, xmlEscape(command), argsXML)
}

type platformServiceControl struct{}

func hasTray(items []UpdateItem) bool {
	for _, it := range items {
		if it.Kind == "tray" {
			return true
		}
	}
	return false
}

func (platformServiceControl) Stop(items []UpdateItem) error {
	// 先礼后兵：/End 是正常结束，残留再按映像名强制结束（与安装脚本一致）。
	for _, it := range items {
		task := windowsTask
		if it.Kind == "tray" {
			task = trayWindowsTask
		}
		_ = runQuiet("schtasks", "/End", "/TN", task)
		_ = runQuiet("taskkill", "/F", "/IM", filepath.Base(it.Target))
	}
	return nil
}

func (platformServiceControl) Start(items []UpdateItem) error {
	err := runQuiet("schtasks", "/Run", "/TN", windowsTask)
	if hasTray(items) {
		_ = runQuiet("schtasks", "/Run", "/TN", trayWindowsTask)
	}
	return err
}

// serviceRunsBinary 报告计划任务 DecAgent 是否指向给定的可执行文件。
//
// schtasks /Query /XML 的输出编码随系统而变（UTF-16 或控制台代码页），
// 中文用户名在代码页里会变形；所以退一步也接受安装脚本的标准安装目录。
func serviceRunsBinary(paths ...string) bool {
	out, err := commandOutputBytes("schtasks", "/Query", "/TN", windowsTask, "/XML")
	if err != nil {
		return false
	}
	def := strings.ToLower(decodeMaybeUTF16(out))
	std := ""
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		std = strings.ToLower(filepath.Join(la, "Programs", "dec"))
	}
	for _, bin := range paths {
		if bin == "" {
			continue
		}
		low := strings.ToLower(bin)
		if strings.Contains(def, low) || strings.Contains(def, strings.ToLower(xmlEscape(bin))) {
			return true
		}
		if std != "" && strings.EqualFold(filepath.Dir(low), std) {
			return true
		}
	}
	return false
}

func commandOutputBytes(name string, args ...string) ([]byte, error) {
	s, err := commandOutput(context.Background(), name, args...)
	return []byte(s), err
}

func decodeMaybeUTF16(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	} else if len(b) < 2 || b[1] != 0 {
		return string(b)
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}
