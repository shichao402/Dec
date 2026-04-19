package tui

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// Run 启动默认 TUI Shell。
func Run(projectRoot string, input io.Reader, output io.Writer) error {
	program := tea.NewProgram(
		newModel(projectRoot),
		tea.WithAltScreen(),
		tea.WithInput(input),
		tea.WithOutput(output),
	)

	_, err := program.Run()
	return err
}
