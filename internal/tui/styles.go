package tui

import "github.com/charmbracelet/lipgloss"

var (
	shellTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	shellMutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	shellCardStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("67")).Padding(0, 1)
	shellActiveNav   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	shellNavStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	shellStatusBar   = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	shellLogStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	shellWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	shellGoodStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Bold(true)
	shellSelectedRow = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Bold(true)
	shellEnabledRow  = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	shellAccentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
)

// cardChromeVertical 是圆角边框上下各 1 行占用。
const cardChromeVertical = 2

// cardChromeHorizontal 是圆角边框左右各 1 列 + Padding(0,1) 左右各 1 列。
const cardChromeHorizontal = 4
