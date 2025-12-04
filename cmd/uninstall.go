package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/firoyang/CursorToolset/pkg/installer"
	"github.com/firoyang/CursorToolset/pkg/loader"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/spf13/cobra"
)

var (
	uninstallForce bool
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <toolset-name>",
	Short: "卸载指定的工具集",
	Long: `卸载指定的工具集，包括：
  1. 删除工具集源码目录
  2. 删除安装的规则文件
  3. 删除安装的脚本文件

使用 --force 跳过确认提示。`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolsetName := args[0]

		// 确定工作目录
		workDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("获取工作目录失败: %w", err)
		}

		// 加载工具集列表
		toolsetsPath := loader.GetToolsetsPath(workDir)
		toolsets, err := loader.LoadToolsets(toolsetsPath)
		if err != nil {
			return fmt.Errorf("加载工具集列表失败: %w", err)
		}

		// 查找工具集
		toolset := loader.FindToolset(toolsets, toolsetName)
		if toolset == nil {
			return fmt.Errorf("未找到工具集: %s", toolsetName)
		}

		// 检查是否已安装
		toolsetsDir, err := paths.GetToolsetsDir(workDir)
		if err != nil {
			return fmt.Errorf("获取工具集安装目录失败: %w", err)
		}

		toolsetPath := filepath.Join(toolsetsDir, toolset.Name)
		if _, err := os.Stat(toolsetPath); os.IsNotExist(err) {
			fmt.Printf("⚠️  工具集 %s 未安装\n", toolset.DisplayName)
			return nil
		}

		// 确认操作
		if !uninstallForce {
			fmt.Printf("🗑️  准备卸载工具集: %s\n", toolset.DisplayName)
			fmt.Printf("   将删除:\n")
			fmt.Printf("   - 工具集源码: %s\n", toolsetPath)
			fmt.Printf("   - 安装的规则文件\n")
			fmt.Printf("   - 安装的脚本文件\n")
			fmt.Println()
			fmt.Print("⚠️  确认卸载？ [y/N]: ")
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" && response != "yes" {
				fmt.Println("❌ 操作已取消")
				return nil
			}
		}

		// 创建卸载器
		uninstaller := installer.NewInstaller(toolsetsDir, workDir)

		// 执行卸载
		fmt.Printf("\n🗑️  开始卸载工具集: %s\n", toolset.DisplayName)
		if err := uninstaller.UninstallToolset(toolset); err != nil {
			return fmt.Errorf("卸载失败: %w", err)
		}

		fmt.Printf("✅ 工具集 %s 卸载完成\n", toolset.DisplayName)
		return nil
	},
}

func init() {
	uninstallCmd.Flags().BoolVarP(&uninstallForce, "force", "f", false, "跳过确认提示，直接卸载")
}
