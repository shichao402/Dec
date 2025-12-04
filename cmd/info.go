package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/firoyang/CursorToolset/pkg/loader"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/firoyang/CursorToolset/pkg/types"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <toolset-name>",
	Short: "查看工具集详细信息",
	Long: `显示指定工具集的详细信息，包括：
  - 基本信息（名称、版本、描述）
  - 仓库信息
  - 安装状态
  - 安装目标（如果已安装）
  - 功能列表（如果有）`,
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
		toolsetInfo := loader.FindToolset(toolsets, toolsetName)
		if toolsetInfo == nil {
			return fmt.Errorf("未找到工具集: %s", toolsetName)
		}

		// 显示基本信息
		fmt.Println("📋 工具集信息")
		fmt.Println(strings.Repeat("=", 50))
		fmt.Println()

		fmt.Printf("名称: %s\n", toolsetInfo.Name)
		if toolsetInfo.DisplayName != "" {
			fmt.Printf("显示名称: %s\n", toolsetInfo.DisplayName)
		}
		if toolsetInfo.Version != "" {
			fmt.Printf("版本: %s\n", toolsetInfo.Version)
		}
		if toolsetInfo.Description != "" {
			fmt.Printf("描述: %s\n", toolsetInfo.Description)
		}
		fmt.Printf("仓库: %s\n", toolsetInfo.GitHubURL)
		fmt.Println()

		// 检查安装状态
		toolsetsDir, err := paths.GetToolsetsDir(workDir)
		if err != nil {
			fmt.Printf("⚠️  无法确定安装目录\n")
			return nil
		}

		toolsetPath := filepath.Join(toolsetsDir, toolsetInfo.Name)
		if _, err := os.Stat(toolsetPath); os.IsNotExist(err) {
			fmt.Printf("状态: ⏳ 未安装\n")
			fmt.Println()
			fmt.Println("💡 使用以下命令安装:")
			fmt.Printf("   cursortoolset install %s\n", toolsetInfo.Name)
			return nil
		}

		fmt.Printf("状态: ✅ 已安装\n")
		fmt.Printf("路径: %s\n", toolsetPath)
		fmt.Println()

		// 读取 toolset.json 获取详细信息
		toolsetConfigPath := filepath.Join(toolsetPath, "toolset.json")
		toolset, err := loadToolsetConfig(toolsetConfigPath)
		if err != nil {
			fmt.Printf("⚠️  无法读取 toolset.json: %v\n", err)
			return nil
		}

		// 显示详细信息
		if toolset.Author != "" {
			fmt.Printf("作者: %s\n", toolset.Author)
		}
		if toolset.License != "" {
			fmt.Printf("许可证: %s\n", toolset.License)
		}
		if len(toolset.Keywords) > 0 {
			fmt.Printf("关键词: %s\n", strings.Join(toolset.Keywords, ", "))
		}
		fmt.Println()

		// 显示安装目标
		if len(toolset.Install.Targets) > 0 {
			fmt.Println("📦 安装目标:")
			for targetPath, target := range toolset.Install.Targets {
				fmt.Printf("  • %s\n", targetPath)
				fmt.Printf("    源路径: %s\n", target.Source)
				if len(target.Files) > 0 {
					fmt.Printf("    文件: %v\n", target.Files)
				}
				if target.Description != "" {
					fmt.Printf("    说明: %s\n", target.Description)
				}
			}
			fmt.Println()
		}

		// 显示功能列表
		if len(toolset.Features) > 0 {
			fmt.Println("✨ 功能列表:")
			for _, feature := range toolset.Features {
				essentialMark := ""
				if feature.Essential {
					essentialMark = " [核心]"
				}
				fmt.Printf("  • %s%s\n", feature.Name, essentialMark)
				if feature.Description != "" {
					fmt.Printf("    %s\n", feature.Description)
				}
			}
			fmt.Println()
		}

		// 显示文档链接
		if len(toolset.Documentation) > 0 {
			fmt.Println("📚 文档:")
			for docType, docURL := range toolset.Documentation {
				fmt.Printf("  • %s: %s\n", docType, docURL)
			}
			fmt.Println()
		}

		return nil
	},
}

// loadToolsetConfig 加载 toolset.json
func loadToolsetConfig(toolsetPath string) (*types.Toolset, error) {
	data, err := os.ReadFile(toolsetPath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	var toolset types.Toolset
	if err := json.Unmarshal(data, &toolset); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}

	return &toolset, nil
}
