package cmd

import (
	"os"

	"github.com/shichao402/Dec/pkg/freshness"
	"github.com/spf13/cobra"
)

// freshnessCheckCmd 是 `dec __freshness-check`，一个 hidden 子命令。
//
// 主 dec 进程在 PersistentPostRunE 里 fork 自己来运行这个命令：
// 子进程独立完成 git fetch 和 cache 写入，父进程立即退出，用户感觉不到等待。
//
// 它不应被用户手动调用，也不向终端输出任何东西。
var freshnessCheckCmd = &cobra.Command{
	Use:           "__freshness-check",
	Short:         "（内部）后台检查项目资产与远端差异，结果写入 cache",
	Hidden:        true,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, _ := cmd.Flags().GetString("project-root")
		if root == "" {
			// Fallback：父进程理论上总会传 --project-root，这里兜底。
			root, _ = os.Getwd()
		}
		return freshness.RunBackgroundCheck(root)
	},
}

func init() {
	freshnessCheckCmd.Flags().String("project-root", "", "要检查的项目根目录（父进程传入）")
	RootCmd.AddCommand(freshnessCheckCmd)
}
