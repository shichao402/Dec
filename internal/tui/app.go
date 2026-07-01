package tui

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// RunOptions 控制 TUI 启动行为。
type RunOptions struct {
	// ConfigInitMode 为 true 时直接进入 Assets 页做 package 选择，保存后退出。
	ConfigInitMode bool
}

// Run 启动默认 TUI Shell。
func Run(projectRoot, currentVersion string, input io.Reader, output io.Writer) error {
	return runWithOptions(projectRoot, currentVersion, RunOptions{}, input, output)
}

// RunConfigInit 启动项目配置初始化 TUI：聚焦 package 勾选，保存后退出。
func RunConfigInit(projectRoot, currentVersion string, input io.Reader, output io.Writer) error {
	return runWithOptions(projectRoot, currentVersion, RunOptions{ConfigInitMode: true}, input, output)
}

func runWithOptions(projectRoot, currentVersion string, opts RunOptions, input io.Reader, output io.Writer) error {
	program := tea.NewProgram(
		newModelWithOptions(projectRoot, currentVersion, opts),
		tea.WithAltScreen(),
		tea.WithInput(input),
		tea.WithOutput(output),
	)

	_, err := program.Run()
	return err
}
