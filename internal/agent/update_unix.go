//go:build !windows

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/shichao402/Dec/internal/sysproc"
)

// launchDetached 以脱离服务的方式拉起更新进程。
//
// 停服务时服务管理器会连带结束它托管的子进程，更新进程必须事先跳出去：
//
//   - Linux：systemd 默认按 cgroup 结束整个服务单元，新开一个会话没用，
//     要用 systemd-run 把它放进一个独立的临时单元。没有用户级 systemd 时
//     （前台运行等）退回新会话。
//   - macOS：launchd 停掉任务时结束的是任务的进程组；setsid 开新会话即新
//     进程组，就不在被结束的范围里。
func launchDetached(bin string, args []string) error {
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("systemd-run"); err == nil {
			unit := fmt.Sprintf("dec-agent-update-%d", time.Now().Unix())
			full := append([]string{"--user", "--collect", "--quiet", "--unit", unit, "--", bin}, args...)
			if err := sysproc.Command("systemd-run", full...).Run(); err == nil {
				return nil
			}
		}
	}
	cmd := sysproc.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// platformServiceControl 用各平台原生方式停、起代理与托盘。
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
	switch runtime.GOOS {
	case "darwin":
		if hasTray(items) {
			if p := trayPlistPath(); fileExists(p) {
				_ = runQuiet("launchctl", "unload", p)
			}
		}
		p := plistPath()
		if !fileExists(p) {
			return fmt.Errorf("找不到服务定义 %s", p)
		}
		// 不带 -w：只是停，不改「禁用」标记，下次登录照常自启。
		return runQuiet("launchctl", "unload", p)
	case "linux":
		return runQuiet("systemctl", "--user", "stop", systemdUnit)
	}
	return fmt.Errorf("不支持的平台: %s", runtime.GOOS)
}

func (platformServiceControl) Start(items []UpdateItem) error {
	switch runtime.GOOS {
	case "darwin":
		err := runQuiet("launchctl", "load", plistPath())
		if hasTray(items) {
			if p := trayPlistPath(); fileExists(p) {
				_ = runQuiet("launchctl", "load", p)
			}
		}
		return err
	case "linux":
		return runQuiet("systemctl", "--user", "start", systemdUnit)
	}
	return fmt.Errorf("不支持的平台: %s", runtime.GOOS)
}

// serviceRunsBinary 报告已注册的服务是否指向给定的可执行文件。
func serviceRunsBinary(paths ...string) bool {
	p, err := ServicePath()
	if err != nil || p == "" {
		return false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	def := string(data)
	for _, bin := range paths {
		if bin == "" {
			continue
		}
		if strings.Contains(def, xmlEscape(bin)) || strings.Contains(def, bin) {
			return true
		}
	}
	return false
}
