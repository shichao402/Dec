package cmd

import (
	"fmt"
	"strings"

	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/registry"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <package-name>",
	Short: "查看包的详细信息",
	Long: `显示指定包的详细信息，包括：
  - 基本信息（名称、版本、描述、类型）
  - 来源（local/test/official）
  - 安装状态`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		packageName := args[0]

		// 确保目录结构存在
		if err := paths.EnsureAllDirs(); err != nil {
			return fmt.Errorf("初始化目录失败: %w", err)
		}

		// 使用多注册表管理器
		mgr := registry.NewMultiRegistryManager()
		if err := mgr.Load(); err != nil {
			return fmt.Errorf("加载注册表失败: %w", err)
		}

		// 查找包
		pack := mgr.ResolvePack(packageName)
		if pack == nil {
			return fmt.Errorf("未找到包: %s\n\n提示: 使用 dec list 查看可用的包", packageName)
		}

		// 显示信息
		fmt.Println("📋 包信息")
		fmt.Println(strings.Repeat("=", 50))
		fmt.Println()

		// 类型图标
		typeIcon := "📜 规则包"
		if pack.Type == types.PackTypeMCP {
			typeIcon = "🔧 MCP 工具包"
		}

		// 基本信息
		fmt.Printf("名称: %s\n", pack.Name)
		fmt.Printf("类型: %s\n", typeIcon)
		if pack.Version != "" {
			fmt.Printf("版本: %s\n", pack.Version)
		}
		if pack.Description != "" {
			fmt.Printf("描述: %s\n", pack.Description)
		}
		if pack.Category != "" {
			fmt.Printf("分类: %s\n", pack.Category)
		}
		fmt.Println()

		// 来源信息
		fmt.Println("📦 来源")
		switch pack.Source {
		case types.RegistryTypeLocal:
			fmt.Println("   类型: 本地开发包")
			if pack.LocalPath != "" {
				fmt.Printf("   路径: %s\n", pack.LocalPath)
			}
			if pack.LinkedAt != "" {
				fmt.Printf("   链接时间: %s\n", pack.LinkedAt)
			}
		case types.RegistryTypeTest:
			fmt.Println("   类型: 测试渠道")
			if pack.Repository != "" {
				fmt.Printf("   仓库: %s\n", pack.Repository)
			}
		case types.RegistryTypeOfficial:
			fmt.Println("   类型: 正式渠道")
			if pack.Repository != "" {
				fmt.Printf("   仓库: %s\n", pack.Repository)
			}
		}
		fmt.Println()

		// 安装状态
		fmt.Println("📊 状态")
		if pack.Source == types.RegistryTypeLocal {
			fmt.Println("   🔗 已链接（本地开发）")
		} else if pack.IsInstalled {
			fmt.Println("   ✅ 已安装")
			if pack.InstallPath != "" {
				fmt.Printf("   路径: %s\n", pack.InstallPath)
			}
		} else {
			fmt.Println("   ⏳ 未安装")
			fmt.Println()
			fmt.Println("💡 在 .dec/config/packs.json 中启用此包，然后运行 dec sync")
		}

		return nil
	},
}
