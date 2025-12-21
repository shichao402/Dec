package cmd

import (
	"fmt"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/spf13/cobra"
)

var useCmd = &cobra.Command{
	Use:   "use <version>",
	Short: "切换包版本",
	Long: `切换要使用的包版本。

可以指定具体版本号（如 v1.0.0）或使用 latest 自动跟随最新版本。

示例:
  # 使用最新版本
  dec use latest

  # 使用指定版本
  dec use v1.0.0

切换版本后需要运行 'dec update' 更新包缓存。`,
	Args: cobra.ExactArgs(1),
	RunE: runUse,
}

func init() {
	RootCmd.AddCommand(useCmd)
}

func runUse(cmd *cobra.Command, args []string) error {
	version := args[0]

	// 加载当前配置
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	oldVersion := cfg.PackagesVersion

	// 设置新版本
	if err := config.SetPackagesVersion(version); err != nil {
		return fmt.Errorf("设置版本失败: %w", err)
	}

	if oldVersion == version {
		fmt.Printf("当前已是版本: %s\n", version)
	} else {
		fmt.Printf("已从 %s 切换到 %s\n", oldVersion, version)
	}

	fmt.Println("请运行 'dec update' 更新包缓存")
	return nil
}
