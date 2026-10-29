package agent

import (
	"context"

	"github.com/shichao402/Dec/internal/sysproc"
)

// commandOutput 运行一个子进程并取合并输出；Windows 上不弹控制台窗口。
func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := sysproc.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
