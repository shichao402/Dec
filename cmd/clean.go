package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/firoyang/CursorToolset/pkg/loader"
	"github.com/spf13/cobra"
)

var (
	cleanKeepToolsets bool
	cleanForce        bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "清理已安装的工具集",
	Long: `清理所有已安装的工具集文件。

此命令会删除：
  1. .cursor/rules/ 中安装的规则文件
  2. scripts/toolsets/ 中安装的脚本
  3. .cursor/toolsets/ 目录（可选，使用 --keep-toolsets 保留）

使用 --force 跳过确认提示。`,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		if len(toolsets) == 0 {
			fmt.Println("📋 没有找到工具集")
			return nil
		}

		// 收集需要清理的目录
		dirsToClean := []string{
			filepath.Join(workDir, ".cursor", "rules"),
			filepath.Join(workDir, "scripts", "toolsets"),
		}

		if !cleanKeepToolsets {
			dirsToClean = append(dirsToClean, filepath.Join(workDir, ".cursor", "toolsets"))
		}

		// 显示将要清理的内容
		fmt.Printf("🧹 准备清理以下目录:\n\n")
		for _, dir := range dirsToClean {
			if _, err := os.Stat(dir); err == nil {
				fmt.Printf("  📁 %s\n", dir)
			}
		}
		fmt.Println()

		// 确认操作
		if !cleanForce {
			fmt.Print("⚠️  此操作将删除已安装的工具集文件。是否继续？ [y/N]: ")
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" && response != "yes" {
				fmt.Println("❌ 操作已取消")
				return nil
			}
		}

		// 执行清理
		fmt.Println()
		cleaned := 0
		for _, dir := range dirsToClean {
			if err := cleanDirectory(dir); err != nil {
				fmt.Printf("  ⚠️  清理 %s 失败: %v\n", dir, err)
			} else {
				cleaned++
			}
		}

		fmt.Println()
		if cleaned > 0 {
			fmt.Printf("✅ 清理完成！共清理 %d 个目录\n", cleaned)
			if cleanKeepToolsets {
				fmt.Println("💡 提示：.cursor/toolsets/ 目录已保留")
			}
		} else {
			fmt.Println("ℹ️  没有需要清理的内容")
		}

		return nil
	},
}

func init() {
	cleanCmd.Flags().BoolVarP(&cleanKeepToolsets, "keep-toolsets", "k", false, "保留 .cursor/toolsets/ 目录（只清理安装的文件）")
	cleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false, "跳过确认提示，直接清理")
}

// cleanDirectory 清理指定目录
func cleanDirectory(dir string) error {
	// 检查目录是否存在
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		fmt.Printf("  ⏭️  跳过不存在的目录: %s\n", dir)
		return nil
	}

	// 删除目录
	fmt.Printf("  🗑️  删除: %s\n", dir)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}

	return nil
}

