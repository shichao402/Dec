package cmd

import (
	"fmt"
	"strings"

	"github.com/firoyang/CursorToolset/pkg/installer"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/firoyang/CursorToolset/pkg/registry"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "搜索包",
	Long: `根据关键词搜索包。

搜索范围包括：
  - 包名称
  - 显示名称
  - 描述
  - 关键词`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keyword := args[0]

		// 确保目录结构存在
		if err := paths.EnsureAllDirs(); err != nil {
			return fmt.Errorf("初始化目录失败: %w", err)
		}

		// 加载 registry
		mgr := registry.NewManager()
		if err := mgr.Load(); err != nil {
			return fmt.Errorf("加载包索引失败: %w", err)
		}

		// 检查是否有本地缓存
		if !mgr.HasLocalCache() {
			fmt.Println("📦 本地包索引为空")
			fmt.Println("\n提示: 运行 'cursortoolset registry update' 更新包索引")
			return nil
		}

		// 搜索
		results := mgr.SearchPackages(keyword)

		if len(results) == 0 {
			fmt.Printf("🔍 未找到匹配 \"%s\" 的包\n", keyword)
			return nil
		}

		fmt.Printf("🔍 找到 %d 个匹配 \"%s\" 的包:\n\n", len(results), keyword)

		inst := installer.NewInstaller()

		for i, manifest := range results {
			// 名称和版本
			fmt.Printf("%d. %s", i+1, manifest.Name)
			if manifest.Version != "" {
				fmt.Printf("@%s", manifest.Version)
			}

			// 显示名称
			if manifest.DisplayName != "" && manifest.DisplayName != manifest.Name {
				fmt.Printf(" (%s)", manifest.DisplayName)
			}
			fmt.Println()

			// 描述（高亮匹配部分）
			if manifest.Description != "" {
				desc := highlightKeyword(manifest.Description, keyword)
				fmt.Printf("   %s\n", desc)
			}

			// 关键词
			if len(manifest.Keywords) > 0 {
				fmt.Printf("   关键词: %s\n", strings.Join(manifest.Keywords, ", "))
			}

			// 状态
			if inst.IsInstalled(manifest.Name) {
				fmt.Printf("   状态: ✅ 已安装\n")
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

// highlightKeyword 高亮关键词（简单实现，使用大写）
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
