package cmd

import (
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:   "cursortoolset",
	Short: "Cursor 工具集管理器",
	Long: `CursorToolset - Cursor 工具集管理器

一个用于管理和安装 Cursor 工具集的命令行工具。
每个工具集都包含一个 toolset.json 描述文件，定义了工具的安装和配置信息。

使用示例:
  # 安装所有工具集
  cursortoolset install

  # 安装特定工具集
  cursortoolset install <toolset-name>

  # 列出所有可用工具集
  cursortoolset list`,
}

func init() {
	RootCmd.AddCommand(installCmd)
	RootCmd.AddCommand(listCmd)
}

