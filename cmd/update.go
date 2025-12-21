package cmd

import (
	"fmt"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "更新包缓存",
	Long: `从远程包源下载最新的包到本地缓存。

根据全局配置中的包源地址和版本，下载对应的规则和 MCP 包。

示例:
  # 更新包缓存
  dec update

  # 先切换版本再更新
  dec use v1.0.0
  dec update`,
	RunE: runUpdate,
}

func init() {
	RootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	// 显示当前配置
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	fmt.Printf("包源: %s\n", cfg.PackagesSource)
	fmt.Printf("版本: %s\n", cfg.PackagesVersion)
	fmt.Println()

	// 创建获取器并下载
	fetcher, err := config.NewFetcher()
	if err != nil {
		return fmt.Errorf("创建获取器失败: %w", err)
	}

	if err := fetcher.FetchPackages(); err != nil {
		return fmt.Errorf("更新包失败: %w", err)
	}

	fmt.Println()
	fmt.Println("更新完成！运行 'dec list' 查看可用包。")
	return nil
}
