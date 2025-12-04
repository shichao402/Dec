package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/firoyang/CursorToolset/pkg/loader"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有可用工具集",
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
			fmt.Println("available-toolsets.json 中没有找到工具集")
			return nil
		}
		
		fmt.Printf("📋 可用工具集 (%d 个):\n\n", len(toolsets))
		for i, toolset := range toolsets {
			fmt.Printf("%d. %s", i+1, toolset.Name)
			if toolset.DisplayName != "" {
				fmt.Printf(" (%s)", toolset.DisplayName)
			}
			fmt.Println()
			
			if toolset.Description != "" {
				fmt.Printf("   描述: %s\n", toolset.Description)
			}
			
			fmt.Printf("   仓库: %s\n", toolset.GitHubURL)
			
			// 检查是否已安装
			toolsetPath := filepath.Join(workDir, ".cursor", "toolsets", toolset.Name)
			if _, err := os.Stat(toolsetPath); err == nil {
				fmt.Printf("   状态: ✅ 已安装\n")
			} else {
				fmt.Printf("   状态: ⏳ 未安装\n")
			}
			
			if i < len(toolsets)-1 {
				fmt.Println()
			}
		}
		
		return nil
	},
}


