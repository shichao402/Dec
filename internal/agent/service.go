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
		xmlEscape(filepath.Join(opts.LogDir, "agent.log")),
		xmlEscape(filepath.Join(opts.LogDir, "agent.err.log")))
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
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`, bin, opts.StateDir)
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

// installWindowsTask 用计划任务注册。
func installWindowsTask(bin string, opts ServiceOptions) error {
	_ = runQuiet("schtasks", "/Delete", "/TN", windowsTask, "/F")
	// ONLOGON 保证登录即启动；/RL LIMITED 避免要管理员权限。
	cmd := fmt.Sprintf(`set DEC_AGENT_STATE=%s && "%s" run`, opts.StateDir, bin)
	return runQuiet("schtasks", "/Create", "/TN", windowsTask,
		"/TR", cmd, "/SC", "ONLOGON", "/RL", "LIMITED", "/F")
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
