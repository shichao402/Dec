package cmd

import (
	"context"
	"fmt"
	"os"

	decmcp "github.com/shichao402/Dec/internal/mcp"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:    "mcp",
	Short:  "启动 Dec MCP Server（stdio，供 Agent 调用）",
	Hidden: false,
	Long: `启动 Model Context Protocol (stdio) 服务，暴露 pull/push/delete 等 Dec 操作。

Cursor / IDE 配置示例：
  {
    "mcpServers": {
      "dec": {
        "command": "dec",
        "args": ["mcp", "--project-root", "${workspaceFolder}"]
      }
    }
  }

Bitwarden 未解锁时会自动弹出 web unlock 页面；MCP 进程内 session 可复用。
Web unlock 超时 3 分钟（可用 DEC_BW_UNLOCK_TIMEOUT 覆盖）。`,
	RunE: runMCP,
}

var mcpProjectRoot string

func runMCP(cmd *cobra.Command, args []string) error {
	// MCP 协议使用 stdout；所有非协议输出必须走 stderr。
	cmd.SetOut(os.Stderr)
	cmd.SetErr(os.Stderr)

	root := mcpProjectRoot
	if root == "" {
		root = os.Getenv("DEC_PROJECT_ROOT")
	}
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("获取当前目录失败: %w", err)
		}
		root = cwd
	}

	return decmcp.Run(context.Background(), decmcp.Config{ProjectRoot: root})
}

func init() {
	mcpCmd.Flags().StringVar(&mcpProjectRoot, "project-root", "", "项目根目录（默认 DEC_PROJECT_ROOT 或当前目录）")
	RootCmd.AddCommand(mcpCmd)
}
