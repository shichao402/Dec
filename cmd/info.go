package cmd

import (
	"fmt"
	"strings"

	"github.com/firoyang/CursorToolset/pkg/installer"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/firoyang/CursorToolset/pkg/registry"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <package-name>",
	Short: "查看包的详细信息",
	Long: `显示指定包的详细信息，包括：
  - 基本信息（名称、版本、描述）
  - 仓库信息
  - 安装状态
  - 下载信息`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		packageName := args[0]

		// 确保目录结构存在
		if err := paths.EnsureAllDirs(); err != nil {
			return fmt.Errorf("初始化目录失败: %w", err)
		}

		// 加载 registry
		mgr := registry.NewManager()
		if err := mgr.Load(); err != nil {
			return fmt.Errorf("加载包索引失败: %w", err)
		}

		// 查找包
		manifest := mgr.FindPackage(packageName)
		if manifest == nil {
			return fmt.Errorf("未找到包: %s\n\n提示: 运行 'cursortoolset registry update' 更新包索引", packageName)
		}

		// 显示信息
		fmt.Println("📋 包信息")
		fmt.Println(strings.Repeat("=", 50))
		fmt.Println()

		// 基本信息
		fmt.Printf("名称: %s\n", manifest.Name)
		if manifest.DisplayName != "" {
			fmt.Printf("显示名称: %s\n", manifest.DisplayName)
		}
		fmt.Printf("版本: %s\n", manifest.Version)
		if manifest.Description != "" {
			fmt.Printf("描述: %s\n", manifest.Description)
		}
		if manifest.Author != "" {
			fmt.Printf("作者: %s\n", manifest.Author)
		}
		if manifest.License != "" {
			fmt.Printf("许可证: %s\n", manifest.License)
		}
		if len(manifest.Keywords) > 0 {
			fmt.Printf("关键词: %s\n", strings.Join(manifest.Keywords, ", "))
		}
		fmt.Println()

		// 仓库信息
		if manifest.Repository.URL != "" {
			fmt.Println("📦 仓库")
			fmt.Printf("   URL: %s\n", manifest.Repository.URL)
			fmt.Println()
		}

		// 下载信息
		fmt.Println("📥 下载")
		fmt.Printf("   Tarball: %s\n", manifest.Dist.Tarball)
		if manifest.Dist.SHA256 != "" {
			fmt.Printf("   SHA256: %s\n", manifest.Dist.SHA256)
		}
		if manifest.Dist.Size > 0 {
			fmt.Printf("   大小: %.2f MB\n", float64(manifest.Dist.Size)/1024/1024)
		}
		fmt.Println()

		// 安装状态
		inst := installer.NewInstaller()
		packagePath, _ := paths.GetPackagePath(packageName)

		if inst.IsInstalled(packageName) {
			fmt.Printf("状态: ✅ 已安装\n")
			fmt.Printf("路径: %s\n", packagePath)
		} else {
			fmt.Printf("状态: ⏳ 未安装\n")
			fmt.Println()
			fmt.Println("💡 使用以下命令安装:")
			fmt.Printf("   cursortoolset install %s\n", packageName)
		}

		// 依赖
		if len(manifest.Dependencies) > 0 {
			fmt.Println()
			fmt.Println("📦 依赖")
			for _, dep := range manifest.Dependencies {
				if inst.IsInstalled(dep) {
					fmt.Printf("   ✅ %s\n", dep)
				} else {
					fmt.Printf("   ⏳ %s\n", dep)
				}
			}
		}

		// 管理器兼容性
		if manifest.CursorToolset.MinVersion != "" {
			fmt.Println()
			fmt.Println("⚙️  兼容性")
			fmt.Printf("   最低管理器版本: %s\n", manifest.CursorToolset.MinVersion)
		}

		return nil
	},
}
