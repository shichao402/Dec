package tui

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/internal/app"
)

// RunOptions 控制 TUI 启动行为。
type RunOptions struct {
	// Plane 选择 project 或 user 工作空间；空值兼容为 project。
	Plane app.WorkspacePlane
	// ConfigInitMode 为 true 时直接进入 Bundles 页做 bundle 选择，保存后退出。
	// 仅供内部测试；用户面入口已移除。
	ConfigInitMode bool
}

// Run 启动默认 TUI Shell。
func Run(projectRoot, currentVersion string, input io.Reader, output io.Writer) error {
	return runWithOptions(projectRoot, currentVersion, RunOptions{}, input, output)
}

// RunWithOptions 启动显式工作空间平面的 TUI。
func RunWithOptions(projectRoot, currentVersion string, opts RunOptions, input io.Reader, output io.Writer) error {
	return runWithOptions(projectRoot, currentVersion, opts, input, output)
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
