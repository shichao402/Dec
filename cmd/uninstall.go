package cmd

import (
	"fmt"

	"github.com/firoyang/CursorToolset/pkg/installer"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/firoyang/CursorToolset/pkg/registry"
	"github.com/spf13/cobra"
)

var (
	uninstallForce bool
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <package-name>",
	Short: "卸载包",
	Long: `卸载指定的包。

这将删除包的安装目录。使用 --force 跳过确认提示。`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		packageName := args[0]

		// 确保目录结构存在
		if err := paths.EnsureAllDirs(); err != nil {
			return fmt.Errorf("初始化目录失败: %w", err)
		}

		inst := installer.NewInstaller()

		// 检查是否已安装
		if !inst.IsInstalled(packageName) {
			fmt.Printf("⚠️  包 %s 未安装\n", packageName)
			return nil
		}

		// 获取包信息（用于显示）
		mgr := registry.NewManager()
		_ = mgr.Load()
		manifest := mgr.FindPackage(packageName)

		displayName := packageName
		if manifest != nil && manifest.DisplayName != "" {
			displayName = manifest.DisplayName
		}

		// 确认操作
		if !uninstallForce {
			packagePath, _ := paths.GetPackagePath(packageName)
			fmt.Printf("🗑️  准备卸载: %s\n", displayName)
			fmt.Printf("   将删除: %s\n", packagePath)
			fmt.Println()
			fmt.Print("⚠️  确认卸载？ [y/N]: ")

			var response string
			_, _ = fmt.Scanln(&response)
			if response != "y" && response != "Y" && response != "yes" {
				fmt.Println("❌ 操作已取消")
				return nil
			}
		}

		// 执行卸载
		return inst.Uninstall(packageName)
	},
}

func init() {
	uninstallCmd.Flags().BoolVarP(&uninstallForce, "force", "f", false, "跳过确认提示")
}
