package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

// PrintCommandError 使用分段和颜色区分错误与帮助信息。
func PrintCommandError(w io.Writer, args []string, err error) {
	if err == nil {
		return
	}

	sections := splitErrorSections(err.Error())
	if len(sections) == 0 {
		return
	}

	writeLabeledBlock(w, "错误:", ansiRed, sections[0])

	helpSections := sections[1:]
	if len(helpSections) == 0 {
		helpSections = []string{fmt.Sprintf("运行 %s --help 查看完整用法", helpCommandForArgs(args))}
	}
	for _, section := range helpSections {
		fmt.Fprintln(w)
		writeLabeledBlock(w, "帮助:", ansiCyan, section)
	}
}

func printWarningBlock(w io.Writer, message string) {
	writeLabeledBlock(w, "警告:", ansiYellow, message)
}

func writeLabeledBlock(w io.Writer, label, color, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	if strings.Contains(body, "\n") {
		fmt.Fprintf(w, "%s\n%s\n", colorize(w, color, label), body)
		return
	}
	fmt.Fprintf(w, "%s %s\n", colorize(w, color, label), body)
}

func splitErrorSections(message string) []string {
	normalized := strings.ReplaceAll(message, "\r\n", "\n")
	rawSections := strings.Split(normalized, "\n\n")
	sections := make([]string, 0, len(rawSections))
	for _, raw := range rawSections {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			sections = append(sections, trimmed)
		}
	}
	return sections
}

func helpCommandForArgs(args []string) string {
	if len(args) == 0 {
		return RootCmd.CommandPath()
	}
	found, _, err := RootCmd.Find(args)
	if err == nil && found != nil {
		return found.CommandPath()
	}
	if found != nil {
		return found.CommandPath()
	}
	return RootCmd.CommandPath()
}

func colorize(w io.Writer, color, text string) string {
	if !supportsColor(w) {
		return text
	}
	return color + text + ansiReset
}

func supportsColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
