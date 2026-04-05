package editor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Open 使用指定编辑器打开文件，阻塞直到编辑器关闭。
func Open(filePath, editorCmd string) error {
	resolved := strings.TrimSpace(editorCmd)
	if resolved == "" {
		resolved = DefaultCommand()
	}

	args := splitCommand(resolved)
	if len(args) == 0 {
		return fmt.Errorf("未检测到可用编辑器，请在配置文件中设置 editor，或安装 vim/vi")
	}

	cmd := exec.Command(args[0], append(args[1:], filePath)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
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
