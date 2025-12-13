package cmd

import (
	"fmt"

	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/registry"
	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "管理包注册表",
	Long: `管理包注册表（registry）。

子命令：
  update    更新本地包索引缓存
  add       添加包到 registry（维护者使用）
  remove    从 registry 移除包（维护者使用）
  export    导出 registry 为 JSON`,
}

var registryUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "更新本地包索引缓存",
	Long: `从远程服务器下载最新的包索引，并更新所有包的 manifest 缓存。

这个命令会：
  1. 下载最新的 registry.json
  2. 获取每个包的 manifest 信息
  3. 缓存到本地供后续使用`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 确保目录结构存在
		if err := paths.EnsureAllDirs(); err != nil {
			return fmt.Errorf("初始化目录失败: %w", err)
		}

		mgr := registry.NewManager()
		return mgr.Update()
	},
}

var registryAddCmd = &cobra.Command{
	Use:   "add <repository-url>",
	Short: "添加包到 registry",
	Long: `添加一个新包到本地 registry。

这个命令用于 registry 维护者添加新包。
添加后需要发布 registry 到 GitHub Release。

示例：
  dec registry add https://github.com/user/my-toolset`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repository := args[0]

		// 确保目录结构存在
		if err := paths.EnsureAllDirs(); err != nil {
			return fmt.Errorf("初始化目录失败: %w", err)
		}

		mgr := registry.NewManager()
		if err := mgr.Load(); err != nil {
			return fmt.Errorf("加载 registry 失败: %w", err)
		}

		if err := mgr.AddPackage(repository); err != nil {
			return fmt.Errorf("添加包失败: %w", err)
		}

		fmt.Printf("✅ 已添加仓库 %s 到 registry\n", repository)
		fmt.Println("\n下一步：")
		fmt.Println("  1. 运行 'dec registry export' 导出 registry")
		fmt.Println("  2. 将导出的 JSON 发布到 GitHub Release")

		return nil
	},
}

var registryRemoveCmd = &cobra.Command{
	Use:   "remove <repository-or-package-name>",
	Short: "从 registry 移除包",
	Long: `从本地 registry 移除一个包。

这个命令用于 registry 维护者移除包。
移除后需要重新发布 registry 到 GitHub Release。

可以使用仓库 URL、仓库名或包名来指定要移除的包。`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := args[0]

		mgr := registry.NewManager()
		if err := mgr.Load(); err != nil {
			return fmt.Errorf("加载 registry 失败: %w", err)
		}

		if err := mgr.RemovePackage(identifier); err != nil {
			return fmt.Errorf("移除包失败: %w", err)
		}

		fmt.Printf("✅ 已从 registry 移除: %s\n", identifier)
		return nil
	},
}

var registryExportCmd = &cobra.Command{
	Use:   "export",
	Short: "导出 registry 为 JSON",
	Long: `导出当前的 registry 为 JSON 格式。

输出可以用于发布到 GitHub Release。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := registry.NewManager()
		if err := mgr.Load(); err != nil {
			return fmt.Errorf("加载 registry 失败: %w", err)
		}

		data, err := mgr.ExportRegistry()
		if err != nil {
			return fmt.Errorf("导出 registry 失败: %w", err)
		}

		fmt.Println(string(data))
		return nil
	},
}

var registryListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出 registry 中的所有包",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := registry.NewManager()
		if err := mgr.Load(); err != nil {
			return fmt.Errorf("加载 registry 失败: %w", err)
		}

		packages := mgr.ListPackages()
		if len(packages) == 0 {
			fmt.Println("📦 registry 为空")
			fmt.Println("\n提示: 运行 'dec registry update' 更新包索引")
			return nil
		}

		fmt.Printf("📦 Registry 中有 %d 个包:\n\n", len(packages))
		for i, item := range packages {
			repoName := item.GetRepoName()
			fmt.Printf("%d. %s\n", i+1, item.Repository)

			// 显示缓存的 manifest 信息
			if manifest := mgr.GetManifestByRepo(repoName); manifest != nil {
				fmt.Printf("   包名: %s\n", manifest.Name)
				if manifest.DisplayName != "" {
					fmt.Printf("   名称: %s\n", manifest.DisplayName)
				}
				if manifest.Version != "" {
					fmt.Printf("   版本: %s\n", manifest.Version)
				}
				if manifest.Description != "" {
					fmt.Printf("   描述: %s\n", manifest.Description)
				}
			}

			if i < len(packages)-1 {
				fmt.Println()
			}
		}

		return nil
	},
}

func init() {
	// 添加子命令
	registryCmd.AddCommand(registryUpdateCmd)
	registryCmd.AddCommand(registryAddCmd)
	registryCmd.AddCommand(registryRemoveCmd)
	registryCmd.AddCommand(registryExportCmd)
	registryCmd.AddCommand(registryListCmd)

	// 添加到根命令
	RootCmd.AddCommand(registryCmd)
}
