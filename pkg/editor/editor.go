package editor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// Open 使用系统编辑器打开文件，阻塞直到编辑器关闭。
func Open(filePath string) error {
	editorCmd := detectEditor()
	if editorCmd == "" {
		return fmt.Errorf("未检测到编辑器，请设置 $EDITOR 或 $VISUAL 环境变量")
	}

	cmd := exec.Command(editorCmd, filePath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("编辑器 %s 退出异常: %w", editorCmd, err)
	}
	return nil
}

// detectEditor 按优先级检测编辑器
func detectEditor() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	return defaultEditor()
}

// defaultEditor 返回平台默认编辑器
func defaultEditor() string {
	switch runtime.GOOS {
	case "windows":
		return "notepad"
	default:
		// macOS / Linux: 优先 nano（更友好），退回 vi
		if _, err := exec.LookPath("nano"); err == nil {
			return "nano"
		}
		if _, err := exec.LookPath("vi"); err == nil {
			return "vi"
		}
		return ""
	}
}
