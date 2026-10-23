package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// 托盘组件 dec-tray 的登录自启动（agent-tray-access 需求 1.1、1.7、7.2、7.4–7.6）。
//
// 托盘是独立进程，和代理服务分开注册：
//
//   - macOS：LaunchAgent com.dec.tray。KeepAlive 只在非正常退出时拉起，
//     用户从菜单「退出」是正常退出，不会被立即拉回（需求 1.5）。
//   - Windows：计划任务 DecTray，登录触发，失败后重试。
//   - Linux：本期没有托盘，直接跳过（需求 6.3）。
//
// 托盘装不上不影响代理：调用方只记告警（需求 7.4）。
const (
	// trayLaunchdLabel 是 macOS 托盘的登录项标识；Rust 托盘与此一致。
	trayLaunchdLabel = "com.dec.tray"
	// trayWindowsTask 是 Windows 托盘的计划任务名。
	trayWindowsTask = "DecTray"
	// AddrFileName 记录代理本次实际监听的回环地址，托盘据此找到代理
	// （需求 3.3：端口因参数改变时不能写死 8787）。
	AddrFileName = "agent.addr"
)

// TrayOptions 是托盘注册参数。
type TrayOptions struct {
	// BinaryPath 是 dec-tray 可执行文件路径。
	BinaryPath string
	// StateDir 是代理状态目录，托盘从这里读地址文件与日志路径。
	StateDir string
	// LogDir 是日志目录。
	LogDir string
}

// TraySupported 报告当前平台是否提供托盘组件。
func TraySupported() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

// TrayBinaryName 返回当前平台的托盘可执行文件名。
func TrayBinaryName() string {
	if runtime.GOOS == "windows" {
		return "dec-tray.exe"
	}
	return "dec-tray"
}

// FindTrayBinary 在代理可执行文件旁边找 dec-tray。
//
// 发布包里两者放在同一目录；代理路径是符号链接时也看链接目标所在目录。
func FindTrayBinary(agentBin string) (string, error) {
	dirs := []string{filepath.Dir(agentBin)}
	if real, err := filepath.EvalSymlinks(agentBin); err == nil {
		if d := filepath.Dir(real); d != dirs[0] {
			dirs = append(dirs, d)
		}
	}
	names := []string{TrayBinaryName(), trayReleaseName()}
	if runtime.GOOS == "windows" && runtime.GOARCH != "amd64" {
		// 发布只出 amd64 托盘，Windows on ARM 以仿真运行。
		names = append(names, "dec-tray-windows-amd64.exe")
	}
	for _, d := range dirs {
		for _, n := range names {
			p := filepath.Join(d, n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return filepath.Abs(p)
			}
		}
	}
	return "", fmt.Errorf("在 %s 旁边没有找到 %s 或 %s", agentBin, TrayBinaryName(), trayReleaseName())
}

// trayReleaseName 是下载通道里的托盘文件名，如 dec-tray-darwin-arm64、
// dec-tray-windows-amd64.exe。用户下载后不改名也能被 install 找到。
func trayReleaseName() string {
	n := "dec-tray-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return n
}

// WriteAddrFile 把实际监听地址写进状态目录，供托盘发现代理。
func WriteAddrFile(stateDir, addr string) error {
	return writeFileAtomic(filepath.Join(stateDir, AddrFileName), []byte(addr+"\n"), 0o600)
}

// InstallTray 注册托盘登录自启动并立即启动。幂等。
func InstallTray(opts TrayOptions) error {
	if !TraySupported() {
		return nil
	}
	if opts.BinaryPath == "" {
		return fmt.Errorf("缺少 dec-tray 路径")
	}
	abs, err := filepath.Abs(opts.BinaryPath)
	if err != nil {
		return err
	}
	if opts.LogDir == "" {
		opts.LogDir = filepath.Join(opts.StateDir, "logs")
	}
	if err := os.MkdirAll(opts.LogDir, 0o700); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		p := trayPlistPath()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
		}
		if err := writeFileAtomic(p, []byte(trayPlist(abs, opts)), 0o644); err != nil {
			return err
		}
		_ = runQuiet("launchctl", "unload", "-w", p)
		if err := runQuiet("launchctl", "load", "-w", p); err != nil {
			return fmt.Errorf("加载托盘登录项失败: %w", err)
		}
		return nil
	case "windows":
		cmd, args := windowsExec(abs, nil, opts.StateDir)
		x := windowsTaskXML("Dec 托盘图标", cmd, args, windowsUserID())
		if err := registerWindowsTask(trayWindowsTask, x, opts.StateDir); err != nil {
			return err
		}
		return runQuiet("schtasks", "/Run", "/TN", trayWindowsTask)
	}
	return nil
}

// UninstallTray 停止托盘并删除登录项（需求 7.5）。没装过也返回 nil。
func UninstallTray() error {
	switch runtime.GOOS {
	case "darwin":
		p := trayPlistPath()
		_ = runQuiet("launchctl", "unload", "-w", p)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	case "windows":
		_ = runQuiet("schtasks", "/End", "/TN", trayWindowsTask)
		_ = runQuiet("schtasks", "/Delete", "/TN", trayWindowsTask, "/F")
	}
	return nil
}

func trayPlistPath() string {
	home, err := HomeDir()
	if err != nil {
		return trayLaunchdLabel + ".plist"
	}
	return filepath.Join(home, "Library", "LaunchAgents", trayLaunchdLabel+".plist")
}

// trayPlist 生成托盘的 LaunchAgent 定义。
//
// KeepAlive.SuccessfulExit=false：崩溃（非零退出、信号）才拉起，
// 菜单「退出」与「托盘创建失败」都是零退出，不会被反复拉起。
// LimitLoadToSessionType=Aqua：只在图形会话里启动，ssh 登录不会起托盘。
// 托盘输出很少，并入代理日志文件，不另开一个常年为空的文件（需求 5.1）。
func trayPlist(bin string, opts TrayOptions) string {
	log := xmlEscape(filepath.Join(opts.LogDir, LogFileName))
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>DEC_AGENT_STATE</key><string>%s</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key><false/>
  </dict>
  <key>LimitLoadToSessionType</key><string>Aqua</string>
  <key>ProcessType</key><string>Interactive</string>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, trayLaunchdLabel, xmlEscape(bin), xmlEscape(opts.StateDir), log, log)
}

// windowsExec 决定计划任务的启动命令。
//
// 任务计划程序的 XML 不能设环境变量。状态目录是默认值时直接启动程序；
// 否则经 cmd.exe 设置 DEC_AGENT_STATE 再启动（只在自定义目录时才会出现控制台窗口）。
func windowsExec(bin string, args []string, stateDir string) (string, string) {
	def := ""
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		def = filepath.Join(la, "dec-agent")
	}
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, winQuote(a))
	}
	if stateDir == "" || strings.EqualFold(filepath.Clean(stateDir), filepath.Clean(def)) {
		return bin, strings.Join(quoted, " ")
	}
	inner := fmt.Sprintf(`set "DEC_AGENT_STATE=%s" && %s`, stateDir, winQuote(bin))
	if len(quoted) > 0 {
		inner += " " + strings.Join(quoted, " ")
	}
	return "cmd.exe", `/c "` + inner + `"`
}

func winQuote(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// windowsUserID 返回 DOMAIN\user，计划任务只在该用户登录时触发。
func windowsUserID() string {
	u := os.Getenv("USERNAME")
	if d := os.Getenv("USERDOMAIN"); d != "" && u != "" {
		return d + `\` + u
	}
	return u
}

// windowsTaskXML 生成用户级计划任务定义。
//
// 相比 schtasks /SC ONLOGON 的命令行参数，XML 能关掉默认 72 小时的
// 执行时限（ExecutionTimeLimit=PT0S，否则常驻进程三天后被结束），
// 关掉「用电池时不启动」，并设置失败重试。LeastPrivilege 不需要管理员（需求 7.6）。
func windowsTaskXML(desc, command, args, userID string) string {
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
    <Description>%s</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>%s
    </LogonTrigger>
  </Triggers>
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
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>%s
    </Exec>
  </Actions>
</Task>
`, xmlEscape(desc), user, user, xmlEscape(command), argsXML)
}

// encodeUTF16LE 把任务 XML 编成带 BOM 的 UTF-16LE，schtasks /XML 认这种编码。
func encodeUTF16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 2, 2+len(u)*2)
	out[0], out[1] = 0xFF, 0xFE
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

// registerWindowsTask 用 XML 注册（覆盖）计划任务。
func registerWindowsTask(name, xmlDef, stateDir string) error {
	dir := stateDir
	if dir == "" {
		dir = os.TempDir()
	}
	p := filepath.Join(dir, name+".task.xml")
	if err := os.WriteFile(p, encodeUTF16LE(xmlDef), 0o600); err != nil {
		return err
	}
	defer os.Remove(p)
	if err := runQuiet("schtasks", "/Create", "/TN", name, "/XML", p, "/F"); err != nil {
		return fmt.Errorf("注册计划任务 %s 失败: %w", name, err)
	}
	return nil
}
