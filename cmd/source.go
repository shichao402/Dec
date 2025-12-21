package cmd

import (
	"fmt"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/spf13/cobra"
)

var sourceResetFlag bool

var sourceCmd = &cobra.Command{
	Use:   "source [url]",
	Short: "查看或切换包源",
	Long: `查看或切换包源地址。

不带参数时显示当前包源配置。
带 URL 参数时切换到指定包源。
使用 --reset 重置为默认包源。

示例:
  # 查看当前包源
  dec source

  # 切换包源
  dec source https://github.com/shichao402/MyDecPackage

  # 重置为默认包源
  dec source --reset`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSource,
}

func init() {
	sourceCmd.Flags().BoolVar(&sourceResetFlag, "reset", false, "重置为默认包源")
	RootCmd.AddCommand(sourceCmd)
}

func runSource(cmd *cobra.Command, args []string) error {
	// 重置为默认包源
	if sourceResetFlag {
		if err := config.SetPackagesSource(config.DefaultPackagesSource); err != nil {
			return fmt.Errorf("重置包源失败: %w", err)
		}
		fmt.Printf("已重置为默认包源: %s\n", config.DefaultPackagesSource)
		return nil
	}

	// 切换包源
	if len(args) == 1 {
		newSource := args[0]
		if err := config.SetPackagesSource(newSource); err != nil {
			return fmt.Errorf("切换包源失败: %w", err)
		}
		fmt.Printf("已切换包源: %s\n", newSource)
		fmt.Println("请运行 'dec update' 更新包缓存")
		return nil
	}

	// 显示当前配置
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	fmt.Println("当前包源配置:")
	fmt.Printf("  包源地址: %s\n", cfg.PackagesSource)
	fmt.Printf("  包版本:   %s\n", cfg.PackagesVersion)

	return nil
}
