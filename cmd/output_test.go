package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPrintCommandErrorSeparatesHelpSection(t *testing.T) {
	var buf bytes.Buffer
	PrintCommandError(&buf, []string{"config", "repo"}, errors.New("仓库未连接\n\n运行 dec config repo <url> 连接仓库"))

	out := buf.String()
	if !strings.Contains(out, "错误: 仓库未连接\n") {
		t.Fatalf("输出应包含错误段, 实际:\n%s", out)
	}
	if !strings.Contains(out, "\n\n帮助: 运行 dec config repo <url> 连接仓库\n") {
		t.Fatalf("输出应包含帮助段, 实际:\n%s", out)
	}
	if strings.Contains(out, "查看完整用法") {
		t.Fatalf("已有帮助段时不应追加通用帮助, 实际:\n%s", out)
	}
}

func TestPrintCommandErrorAddsCommandHelpHintWhenMissing(t *testing.T) {
	var buf bytes.Buffer
	PrintCommandError(&buf, []string{"config", "repo"}, errors.New("accepts 1 arg(s), received 0"))

	out := buf.String()
	if !strings.Contains(out, "错误: accepts 1 arg(s), received 0\n") {
		t.Fatalf("输出应包含错误段, 实际:\n%s", out)
	}
	if !strings.Contains(out, "\n\n帮助: 运行 dec config repo --help 查看完整用法\n") {
		t.Fatalf("输出应包含命令级帮助提示, 实际:\n%s", out)
	}
}
