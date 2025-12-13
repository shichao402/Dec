package cmd

import (
	"fmt"
	"strings"

	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/registry"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "搜索包",
	Long: `根据关键词搜索包。

搜索范围包括：
  - 包名称
  - 描述

示例：
  dec search github     # 搜索包含 github 的包
  dec search rule       # 搜索规则相关的包`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keyword := strings.ToLower(args[0])

		// 确保目录结构存在
		if err := paths.EnsureAllDirs(); err != nil {
			return fmt.Errorf("初始化目录失败: %w", err)
		}

		// 使用多注册表管理器
		mgr := registry.NewMultiRegistryManager()
		if err := mgr.Load(); err != nil {
			return fmt.Errorf("加载注册表失败: %w", err)
		}

		// 获取所有包并搜索
		packs := mgr.ListAllPacks()
		var results []*types.ResolvedPack

		for _, pack := range packs {
			// 搜索名称和描述
			if strings.Contains(strings.ToLower(pack.Name), keyword) ||
				strings.Contains(strings.ToLower(pack.Description), keyword) {
				results = append(results, pack)
			}
		}

		if len(results) == 0 {
			fmt.Printf("🔍 未找到匹配 \"%s\" 的包\n", args[0])
			return nil
		}

		fmt.Printf("🔍 找到 %d 个匹配 \"%s\" 的包:\n\n", len(results), args[0])

		for i, pack := range results {
			// 类型图标
			typeIcon := "📜"
			if pack.Type == types.PackTypeMCP {
				typeIcon = "🔧"
			}

			// 名称和版本
			fmt.Printf("%d. %s %s", i+1, typeIcon, pack.Name)
			if pack.Version != "" {
				fmt.Printf("@%s", pack.Version)
			}

			// 来源标记
			switch pack.Source {
			case types.RegistryTypeLocal:
				fmt.Print(" [local]")
			case types.RegistryTypeTest:
				fmt.Print(" [test]")
			}
			fmt.Println()

			// 描述（高亮匹配部分）
			if pack.Description != "" {
				desc := highlightKeyword(pack.Description, args[0])
				fmt.Printf("   %s\n", desc)
			}

			// 类型
			fmt.Printf("   类型: %s\n", pack.Type)

			// 状态
			if pack.IsInstalled {
				fmt.Printf("   状态: ✅ 已安装\n")
			} else if pack.Source == types.RegistryTypeLocal {
				fmt.Printf("   状态: 🔗 已链接\n")
			} else {
				fmt.Printf("   状态: ⏳ 未安装\n")
			}

			if i < len(results)-1 {
				fmt.Println()
			}
		}

		return nil
	},
}

// highlightKeyword 高亮关键词（简单实现，使用 ** 包裹）
func highlightKeyword(text, keyword string) string {
	lowerText := strings.ToLower(text)
	lowerKeyword := strings.ToLower(keyword)

	idx := strings.Index(lowerText, lowerKeyword)
	if idx == -1 {
		return text
	}

	// 找到匹配位置，用 ** 包裹
	return text[:idx] + "**" + text[idx:idx+len(keyword)] + "**" + text[idx+len(keyword):]
}
