package cmd

import (
	"fmt"

	"github.com/shichao402/Dec/pkg/update"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "更新 Dec 到最新版本",
	Long: `检查并下载最新版本的 Dec 二进制，替换当前安装。

示例：
  dec update`,
	RunE: runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	currentVersion := GetVersion()

	fmt.Println("🔍 检查最新版本...")
	result, err := update.Check(currentVersion)
	if err != nil {
		return fmt.Errorf("检查更新失败: %w", err)
	}

	if !result.NeedUpdate {
		fmt.Printf("✅ 已是最新版本 %s\n", currentVersion)
		return nil
	}

	fmt.Printf("📦 发现新版本: %s -> %s\n", result.CurrentVersion, result.LatestVersion)
	fmt.Println("⬇️  下载更新中...")

	if err := update.DoUpdate(currentVersion); err != nil {
		return fmt.Errorf("更新失败: %w", err)
	}

	fmt.Printf("✅ 更新成功！已更新到 %s\n", result.LatestVersion)
	return nil
}

func init() {
	RootCmd.AddCommand(updateCmd)
}
