package cmd

import (
	"fmt"
	"os"

	"github.com/firoyang/CursorToolset/pkg/installer"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/spf13/cobra"
)

var (
	cleanCache bool
	cleanAll   bool
	cleanForce bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "清理缓存或已安装的包",
	Long: `清理缓存或已安装的包。

选项：
  --cache    清理下载缓存
  --all      清理所有（缓存 + 已安装的包）
  
默认只清理下载缓存。使用 --force 跳过确认提示。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 默认清理缓存
		if !cleanCache && !cleanAll {
			cleanCache = true
		}

		// 确认操作
		if !cleanForce {
			if cleanAll {
				fmt.Println("⚠️  此操作将删除所有缓存和已安装的包！")
			} else {
				fmt.Println("⚠️  此操作将删除下载缓存。")
			}
			fmt.Print("是否继续？ [y/N]: ")

			var response string
			_, _ = fmt.Scanln(&response)
			if response != "y" && response != "Y" && response != "yes" {
				fmt.Println("❌ 操作已取消")
				return nil
			}
		}

		fmt.Println()

		// 清理缓存
		if cleanCache || cleanAll {
			if err := cleanCacheDir(); err != nil {
				fmt.Printf("⚠️  清理缓存失败: %v\n", err)
			}
		}

		// 清理已安装的包
		if cleanAll {
			if err := cleanReposDir(); err != nil {
				fmt.Printf("⚠️  清理已安装包失败: %v\n", err)
			}
		}

		fmt.Println("\n✅ 清理完成！")
		return nil
	},
}

func init() {
	cleanCmd.Flags().BoolVar(&cleanCache, "cache", false, "清理下载缓存")
	cleanCmd.Flags().BoolVar(&cleanAll, "all", false, "清理所有（缓存 + 已安装的包）")
	cleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false, "跳过确认提示")
}

// cleanCacheDir 清理缓存目录
func cleanCacheDir() error {
	cacheDir, err := paths.GetCacheDir()
	if err != nil {
		return err
	}

	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		fmt.Println("  ℹ️  缓存目录不存在")
		return nil
	}

	fmt.Printf("  🗑️  清理缓存: %s\n", cacheDir)

	inst := installer.NewInstaller()
	return inst.ClearCache()
}

// cleanReposDir 清理已安装的包
func cleanReposDir() error {
	reposDir, err := paths.GetReposDir()
	if err != nil {
		return err
	}

	if _, err := os.Stat(reposDir); os.IsNotExist(err) {
		fmt.Println("  ℹ️  没有已安装的包")
		return nil
	}

	fmt.Printf("  🗑️  清理已安装包: %s\n", reposDir)
	return os.RemoveAll(reposDir)
}
