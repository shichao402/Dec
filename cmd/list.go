package cmd

import (
	"fmt"

	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/registry"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/spf13/cobra"
)

var (
	listInstalled bool
	listType      string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有可用的包",
	Long: `列出所有可用的包（规则包和 MCP 工具包）。

支持按类型过滤：
  dec list              # 列出所有包
  dec list --type rule  # 只列出规则包
  dec list --type mcp   # 只列出 MCP 工具包

包来源优先级：local > test > official`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 确保目录结构存在
		if err := paths.EnsureAllDirs(); err != nil {
			return fmt.Errorf("初始化目录失败: %w", err)
		}

		// 使用多注册表管理器
		mgr := registry.NewMultiRegistryManager()
		_ = mgr.Load() // 忽略错误，文件不存在是正常的

		// 检查是否有任何包，如果没有则尝试更新
		packs := mgr.ListAllPacks()
		if len(packs) == 0 {
			// 尝试自动更新注册表
			fmt.Println("📥 正在下载包索引...")
			if updateErr := mgr.UpdateOfficial(); updateErr != nil {
				fmt.Printf("⚠️  下载包索引失败: %v\n", updateErr)
			} else {
				// 重新获取包列表
				packs = mgr.ListAllPacks()
			}
		}

		// 获取所有包（已在上面获取）

		if len(packs) == 0 {
			fmt.Println("📦 没有可用的包")
			fmt.Println("\n提示: 使用 dec link 链接本地开发包")
			return nil
		}

		// 按类型过滤
		var filtered []*types.ResolvedPack
		for _, pack := range packs {
			if listType != "" && pack.Type != listType {
				continue
			}
			filtered = append(filtered, pack)
		}

		if len(filtered) == 0 {
			fmt.Printf("📦 没有类型为 %s 的包\n", listType)
			return nil
		}

		// 统计
		ruleCount := 0
		mcpCount := 0
		for _, pack := range filtered {
			if pack.Type == types.PackTypeRule {
				ruleCount++
			} else if pack.Type == types.PackTypeMCP {
				mcpCount++
			}
		}

		// 显示标题
		if listType != "" {
			fmt.Printf("📦 %s 包 (%d 个):\n\n", listType, len(filtered))
		} else {
			fmt.Printf("📦 可用包 (%d 个, 规则包 %d, MCP 包 %d):\n\n", len(filtered), ruleCount, mcpCount)
		}

		// 显示列表
		for i, pack := range filtered {
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

			// 描述
			if pack.Description != "" {
				fmt.Printf("   %s\n", pack.Description)
			}

			// 本地路径（仅本地开发包）
			if pack.LocalPath != "" {
				fmt.Printf("   路径: %s\n", pack.LocalPath)
			}

			// 安装状态
			if pack.IsInstalled {
				fmt.Printf("   状态: ✅ 已安装\n")
			} else if pack.Source == types.RegistryTypeLocal {
				fmt.Printf("   状态: 🔗 已链接\n")
			} else {
				fmt.Printf("   状态: ⏳ 未安装\n")
			}

			if i < len(filtered)-1 {
				fmt.Println()
			}
		}

		return nil
	},
}

func init() {
	listCmd.Flags().BoolVar(&listInstalled, "installed", false, "只显示已安装的包（已弃用）")
	listCmd.Flags().StringVar(&listType, "type", "", "按类型过滤 (rule, mcp)")
}
