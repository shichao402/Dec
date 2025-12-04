package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/firoyang/CursorToolset/pkg/loader"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "搜索工具集",
	Long: `根据关键词搜索工具集。

搜索范围包括：
  - 工具集名称
  - 显示名称
  - 描述
  - 关键词（如果有）`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keyword := strings.ToLower(args[0])

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

		// 搜索匹配的工具集
		var matches []*loader.ToolsetSearchResult
		for _, toolset := range toolsets {
			if result := loader.SearchToolset(toolset, keyword); result != nil {
				matches = append(matches, result)
			}
		}

		// 显示结果
		if len(matches) == 0 {
			fmt.Printf("🔍 未找到匹配 \"%s\" 的工具集\n", args[0])
			return nil
		}

		fmt.Printf("🔍 找到 %d 个匹配 \"%s\" 的工具集:\n\n", len(matches), args[0])

		// 获取安装目录以检查状态
		toolsetsDir, err := paths.GetToolsetsDir(workDir)
		if err != nil {
			toolsetsDir = ""
		}

		for i, result := range matches {
			toolset := result.Toolset
			fmt.Printf("%d. %s", i+1, toolset.Name)
			if toolset.DisplayName != "" {
				fmt.Printf(" (%s)", toolset.DisplayName)
			}
			fmt.Println()

			if toolset.Description != "" {
				fmt.Printf("   描述: %s\n", toolset.Description)
			}

			// 显示匹配的字段
			if len(result.MatchedFields) > 0 {
				fmt.Printf("   匹配: %s\n", strings.Join(result.MatchedFields, ", "))
			}

			fmt.Printf("   仓库: %s\n", toolset.GitHubURL)

			// 检查是否已安装
			if toolsetsDir != "" {
				toolsetPath := filepath.Join(toolsetsDir, toolset.Name)
				if _, err := os.Stat(toolsetPath); err == nil {
					fmt.Printf("   状态: ✅ 已安装\n")
				} else {
					fmt.Printf("   状态: ⏳ 未安装\n")
				}
			}

			if i < len(matches)-1 {
				fmt.Println()
			}
		}

		return nil
	},
}
