package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type filePickedMsg struct {
	path     string
	err      error
	canceled bool
}

// pickLocalFileCmd 调用操作系统文件选择器；取消时 canceled=true。
func pickLocalFileCmd(prompt string) tea.Cmd {
	return func() tea.Msg {
		path, canceled, err := pickLocalFile(prompt)
		return filePickedMsg{path: path, err: err, canceled: canceled}
	}
}

func pickLocalFile(prompt string) (path string, canceled bool, err error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "选择文件"
	}
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`try
	set theFile to choose file with prompt %q
	POSIX path of theFile
on error number -128
	return ""
end try`, prompt)
		out, runErr := exec.Command("osascript", "-e", script).CombinedOutput()
		if runErr != nil {
			return "", false, fmt.Errorf("系统文件选择失败: %w (%s)", runErr, strings.TrimSpace(string(out)))
		}
		path = strings.TrimSpace(string(out))
		if path == "" {
			return "", true, nil
		}
		return path, false, nil
	default:
		if zenity, lookErr := exec.LookPath("zenity"); lookErr == nil {
			out, runErr := exec.Command(zenity, "--file-selection", "--title="+prompt).CombinedOutput()
			if runErr != nil {
				if strings.TrimSpace(string(out)) == "" {
					return "", true, nil
				}
				return "", false, fmt.Errorf("系统文件选择失败: %w (%s)", runErr, strings.TrimSpace(string(out)))
			}
			return strings.TrimSpace(string(out)), false, nil
		}
		if kdialog, lookErr := exec.LookPath("kdialog"); lookErr == nil {
			out, runErr := exec.Command(kdialog, "--getopenfilename", ".", prompt).CombinedOutput()
			if runErr != nil {
				if strings.TrimSpace(string(out)) == "" {
					return "", true, nil
				}
				return "", false, fmt.Errorf("系统文件选择失败: %w (%s)", runErr, strings.TrimSpace(string(out)))
			}
			return strings.TrimSpace(string(out)), false, nil
		}
		return "", false, fmt.Errorf("当前系统没有可用的文件选择器（需 zenity 或 kdialog）；请改选手输路径")
	}
}
