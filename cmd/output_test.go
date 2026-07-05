package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPrintCommandErrorSeparatesHelpSection(t *testing.T) {
	var buf bytes.Buffer
	PrintCommandError(&buf, []string{"pull"}, errors.New("未知命令\n\n在 TTY 中运行 dec 启动 TUI"))

	out := buf.String()
	if !strings.Contains(out, "错误: 未知命令\n") {
		t.Fatalf("输出应包含错误段, 实际:\n%s", out)
	}
	if !strings.Contains(out, "\n\n帮助: 在 TTY 中运行 dec 启动 TUI\n") {
		t.Fatalf("输出应包含帮助段, 实际:\n%s", out)
	}
	if strings.Contains(out, "查看完整用法") {
		t.Fatalf("已有帮助段时不应追加通用帮助, 实际:\n%s", out)
	}
}

func TestPrintCommandErrorAddsCommandHelpHintWhenMissing(t *testing.T) {
	var buf bytes.Buffer
	PrintCommandError(&buf, []string{"pull"}, errors.New("unknown command \"pull\" for \"dec\""))

	out := buf.String()
	if !strings.Contains(out, "错误: unknown command \"pull\" for \"dec\"\n") {
		t.Fatalf("输出应包含错误段, 实际:\n%s", out)
	}
	if !strings.Contains(out, "\n\n帮助: 运行 dec --help 查看完整用法\n") {
		t.Fatalf("输出应包含命令级帮助提示, 实际:\n%s", out)
	}
}
