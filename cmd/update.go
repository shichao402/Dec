package cmd

import (
	"context"
	"fmt"

	"github.com/shichao402/Dec/internal/serviceapi"
	"github.com/shichao402/Dec/internal/update"
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
		return fmt.Errorf("检查更新失败: %w\n\n%s", err, update.NetworkHelp())
	}

	if !result.NeedUpdate {
		fmt.Printf("✅ 已是最新版本 %s\n", currentVersion)
		return nil
	}

	fmt.Printf("📦 发现新版本: %s -> %s\n", result.CurrentVersion, result.LatestVersion)
	fmt.Println("⬇️  下载更新中...")

	if err := update.DoUpdate(currentVersion, result.LatestVersion); err != nil {
		return fmt.Errorf("更新失败: %w\n\n%s", err, update.NetworkHelp())
	}

	fmt.Printf("✅ 更新成功！已更新到 %s\n", result.LatestVersion)
	fmt.Println("🔄 正在停止旧的 dec-server（下次启动将使用新二进制）...")
	if err := serviceapi.ShutdownIfRunning(context.Background(), result.LatestVersion, "update"); err != nil {
		fmt.Printf("⚠ 未能自动停止 dec-server: %v\n请到 TUI Settings「重启 dec-server」手动重启\n", err)
	} else {
		fmt.Println("✅ 旧服务已停止；下次打开 TUI / MCP 将自动拉起新版本")
	}
	return nil
}

func init() {
	RootCmd.AddCommand(updateCmd)
}
