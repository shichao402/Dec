package editor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// BuildCommand 构造用于打开指定文件的编辑器命令，不立即执行。
// 调用方负责执行或传入 tea.ExecProcess 以便在 TUI 内挂起执行。
// stdin/stdout/stderr 已绑定到当前进程，与 Open 保持一致。
//
// 当未提供 editorCmd 时会回退到 DefaultCommand()；若依然解析不出可用命令，
// 返回 nil 与错误，便于上层给出清晰的用户提示。
func BuildCommand(filePath, editorCmd string) (*exec.Cmd, error) {
	resolved := strings.TrimSpace(editorCmd)
	if resolved == "" {
		resolved = DefaultCommand()
	}

	args := splitCommand(resolved)
	if len(args) == 0 {
		return nil, fmt.Errorf("未检测到可用编辑器，请在配置文件中设置 editor，或安装 vim/vi")
	}

	cmd := exec.Command(args[0], append(args[1:], filePath)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, nil
}

// Open 使用指定编辑器打开文件，阻塞直到编辑器关闭。
func Open(filePath, editorCmd string) error {
	cmd, err := BuildCommand(filePath, editorCmd)
	if err != nil {
		return err
	}

	if err := cmd.Run(); err != nil {
		resolved := strings.TrimSpace(editorCmd)
		if resolved == "" {
			resolved = DefaultCommand()
		}
		return fmt.Errorf("编辑器 %s 退出异常: %w", resolved, err)
	}
	return nil
}

// DefaultCommand 返回平台默认编辑器。
func DefaultCommand() string {
	switch runtime.GOOS {
	case "windows":
		return "notepad"
	default:
		if _, err := exec.LookPath("vim"); err == nil {
			return "vim"
		}
		if _, err := exec.LookPath("vi"); err == nil {
			return "vi"
		}
		if _, err := exec.LookPath("nano"); err == nil {
			return "nano"
		}
		return ""
	}
}

func splitCommand(command string) []string {
	return strings.Fields(command)
}
