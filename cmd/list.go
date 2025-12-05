package cmd

import (
	"fmt"

	"github.com/firoyang/CursorToolset/pkg/installer"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/firoyang/CursorToolset/pkg/registry"
	"github.com/firoyang/CursorToolset/pkg/types"
	"github.com/spf13/cobra"
)

var (
	listInstalled bool
)

type displayItem struct {
	manifest    *types.Manifest
	isInstalled bool
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有可用或已安装的包",
	Long: `列出所有可用的工具集包。

使用 --installed 只显示已安装的包。`,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		inst := installer.NewInstaller()
		manifests := mgr.GetAllManifests()

		if len(manifests) == 0 {
			fmt.Println("📦 没有可用的包")
			return nil
		}

		// 统计
		totalCount := len(manifests)
		installedCount := 0

		// 过滤已安装的包
		var displayList []*displayItem
		for _, manifest := range manifests {
			isInstalled := inst.IsInstalled(manifest.Name)
			if isInstalled {
				installedCount++
			}

			if listInstalled && !isInstalled {
				continue
			}

			displayList = append(displayList, &displayItem{
				manifest:    manifest,
				isInstalled: isInstalled,
			})
		}

		if len(displayList) == 0 {
			if listInstalled {
				fmt.Println("📦 没有已安装的包")
			} else {
				fmt.Println("📦 没有可用的包")
			}
			return nil
		}

		// 显示标题
		if listInstalled {
			fmt.Printf("📦 已安装的包 (%d 个):\n\n", len(displayList))
		} else {
			fmt.Printf("📦 可用包 (%d 个, 已安装 %d 个):\n\n", totalCount, installedCount)
		}

		// 显示列表
		for i, item := range displayList {
			m := item.manifest

			// 名称和版本
			fmt.Printf("%d. %s", i+1, m.Name)
			if m.Version != "" {
				fmt.Printf("@%s", m.Version)
			}

			// 显示名称
			if m.DisplayName != "" && m.DisplayName != m.Name {
				fmt.Printf(" (%s)", m.DisplayName)
			}
			fmt.Println()

			// 描述
			if m.Description != "" {
				fmt.Printf("   %s\n", m.Description)
			}

			// 状态
			if item.isInstalled {
				fmt.Printf("   状态: ✅ 已安装\n")
			} else {
				fmt.Printf("   状态: ⏳ 未安装\n")
			}

			if i < len(displayList)-1 {
				fmt.Println()
			}
		}

		return nil
	},
}

func init() {
	listCmd.Flags().BoolVar(&listInstalled, "installed", false, "只显示已安装的包")
}
