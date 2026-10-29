package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shichao402/Dec/internal/sysproc"
)

// 系统服务注册（需求 2.5）。
//
// 三种平台各自的原生方式：
//
//   - macOS：launchd LaunchAgent（随登录启动，不需要 root）
//   - Linux：systemd user service 加 linger（不需要 root，关机也保持）
//   - Windows：计划任务（避免服务需要管理员权限）
//
// 都用“用户级”而不是系统级：代理写的是用户目录下的文件，
// 以 root 跑反而会让落点属主变成 root，之后用户自己改不了。
const (
	// launchdLabel 是 macOS 的服务标识。
	launchdLabel = "com.dec.agent"
	// systemdUnit 是 Linux 的服务单元名。
	systemdUnit = "dec-agent.service"
	// windowsTask 是 Windows 的计划任务名。
	windowsTask = "DecAgent"
)

// ServiceOptions 是服务注册参数。
type ServiceOptions struct {
	// BinaryPath 是代理可执行文件的绝对路径。
	BinaryPath string
	// StateDir 是状态目录，服务里通过环境变量传入。
	StateDir string
	// LogDir 是日志目录。
	LogDir string
}

// ServicePath 返回当前平台的服务定义文件路径。
func ServicePath() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
	case "linux":
		return filepath.Join(home, ".config", "systemd", "user", systemdUnit), nil
	case "windows":
		// 计划任务的定义不落文件，用命令行注册，这里返回一个空路径。
		return "", nil
	}
	return "", fmt.Errorf("不支持的平台: %s", runtime.GOOS)
}

// InstallService 注册并启动系统服务。
//
// 幂等：重复执行只会覆盖同样的定义并重启服务。
func InstallService(opts ServiceOptions) error {
	if opts.BinaryPath == "" {
		return fmt.Errorf("缺少可执行文件路径")
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
		return installLaunchd(abs, opts)
	case "linux":
		return installSystemd(abs, opts)
	case "windows":
		return installWindowsTask(abs, opts)
	}
	return fmt.Errorf("不支持的平台: %s", runtime.GOOS)
}

// UninstallService 停止并移除系统服务。
func UninstallService() error {
	switch runtime.GOOS {
	case "darwin":
		_ = runQuiet("launchctl", "unload", "-w", plistPath())
		p := plistPath()
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	case "linux":
		_ = runQuiet("systemctl", "--user", "disable", "--now", systemdUnit)
		p, err := ServicePath()
		if err != nil {
			return err
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = runQuiet("systemctl", "--user", "daemon-reload")
		return nil
	case "windows":
		_ = runQuiet("schtasks", "/Delete", "/TN", "DecAgentUpdate", "/F")
		return runQuiet("schtasks", "/Delete", "/TN", windowsTask, "/F")
	}
	return fmt.Errorf("不支持的平台: %s", runtime.GOOS)
}

// installLaunchd 写 plist 并加载。
func installLaunchd(bin string, opts ServiceOptions) error {
	p := plistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>run</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>DEC_AGENT_STATE</key><string>%s</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, launchdLabel, xmlEscape(bin), xmlEscape(opts.StateDir),
		xmlEscape(filepath.Join(opts.LogDir, LogFileName)),
		xmlEscape(filepath.Join(opts.LogDir, LogFileName)))
	if err := writeFileAtomic(p, []byte(plist), 0o644); err != nil {
		return err
	}
	// 先卸载再加载，保证改过的定义生效。
	_ = runQuiet("launchctl", "unload", "-w", p)
	if err := runQuiet("launchctl", "load", "-w", p); err != nil {
		return fmt.Errorf("加载 launchd 服务失败: %w", err)
	}
	return nil
}

// installSystemd 写 unit 并启用。
func installSystemd(bin string, opts ServiceOptions) error {
	p, err := ServicePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("创建 systemd 用户目录失败: %w", err)
	}
	unit := fmt.Sprintf(`[Unit]
Description=Dec agent
After=network-online.target

[Service]
Type=simple
ExecStart=%s run
Environment=DEC_AGENT_STATE=%s
StandardOutput=append:%s
StandardError=append:%s
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`, bin, opts.StateDir, filepath.Join(opts.LogDir, LogFileName), filepath.Join(opts.LogDir, LogFileName))
	if err := writeFileAtomic(p, []byte(unit), 0o644); err != nil {
		return err
	}
	_ = runQuiet("systemctl", "--user", "daemon-reload")
	if err := runQuiet("systemctl", "--user", "enable", "--now", systemdUnit); err != nil {
		return fmt.Errorf("启用 systemd 服务失败: %w", err)
	}
	// linger 让服务在未登录时也运行（需求 2.5）。
	if err := runQuiet("loginctl", "enable-linger"); err != nil {
		// 失败不致命：很多发行版默认就允许。
		return nil
	}
	return nil
}

// installWindowsTask 用计划任务注册并立即启动。
//
// 以前用 `schtasks /TR "set DEC_AGENT_STATE=... && bin run"`：/TR 不经 cmd.exe，
// `set` 不是可执行文件，任务根本起不来；且默认 72 小时执行时限会结束常驻进程。
// 改为 XML 定义，与托盘任务共用生成逻辑。
func installWindowsTask(bin string, opts ServiceOptions) error {
	cmd, args := windowsExec(bin, []string{"run"}, opts.StateDir)
	x := windowsTaskXML("Dec 代理", cmd, args, windowsUserID())
	if err := registerWindowsTask(windowsTask, x, opts.StateDir); err != nil {
		return err
	}
	return runQuiet("schtasks", "/Run", "/TN", windowsTask)
}

// RestartService 重启已注册的系统服务（agent-tray-access 需求 3.3）。
//
// 用各平台的「重启」原语而不是 kill 进程：launchd 的 kickstart -k、
// systemd 的 restart、Windows 计划任务的 end+run。服务没注册时返回错误，
// 由调用方（托盘）提示用户先安装。
func RestartService() error {
	switch runtime.GOOS {
	case "darwin":
		// kickstart -k 先杀掉再按 plist 重新拉起；需要 gui 域 + uid。
		target := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel)
		return runQuiet("launchctl", "kickstart", "-k", target)
	case "linux":
		return runQuiet("systemctl", "--user", "restart", systemdUnit)
	case "windows":
		_ = runQuiet("schtasks", "/End", "/TN", windowsTask)
		return runQuiet("schtasks", "/Run", "/TN", windowsTask)
	}
	return fmt.Errorf("不支持的平台: %s", runtime.GOOS)
}

// StartService 启动已注册但未运行的系统服务。
//
// 与 RestartService 的区别是不强杀：代理已经在跑时是无副作用的空操作，
// 适合托盘发现「未运行」后点「启动」。
func StartService() error {
	switch runtime.GOOS {
	case "darwin":
		target := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel)
		return runQuiet("launchctl", "kickstart", target)
	case "linux":
		return runQuiet("systemctl", "--user", "start", systemdUnit)
	case "windows":
		return runQuiet("schtasks", "/Run", "/TN", windowsTask)
	}
	return fmt.Errorf("不支持的平台: %s", runtime.GOOS)
}

// plistPath 返回 launchd 定义路径。
func plistPath() string {
	home, err := HomeDir()
	if err != nil {
		return launchdLabel + ".plist"
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

// runQuiet 执行命令，静默失败。
func runQuiet(name string, args ...string) error {
	cmd := sysproc.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// xmlEscape 转义 XML 文本。
func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
