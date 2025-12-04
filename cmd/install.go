package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/firoyang/CursorToolset/pkg/installer"
	"github.com/firoyang/CursorToolset/pkg/loader"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/spf13/cobra"
)

var (
	installToolsetsDir string
	installWorkDir     string
	installVersion     string // 指定安装版本
)

var installCmd = &cobra.Command{
	Use:   "install [toolset-name]",
	Short: "安装工具集",
	Long: `安装一个或多个工具集。

如果不指定工具集名称，将安装 available-toolsets.json 中列出的所有工具集。
如果指定了工具集名称，只安装该工具集。`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// 确定工作目录
		if installWorkDir == "" {
			var err error
			installWorkDir, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("获取工作目录失败: %w", err)
			}
		}
		
		// 确定工具集安装目录
		// 优先使用环境变量 CURSOR_TOOLSET_ROOT，如果未设置则使用工作目录下的 .cursor/toolsets
		if installToolsetsDir == "" {
			var err error
			installToolsetsDir, err = paths.GetToolsetsDir(installWorkDir)
			if err != nil {
				return fmt.Errorf("获取工具集安装目录失败: %w", err)
			}
		}
		
		// 加载工具集列表
		toolsetsPath := loader.GetToolsetsPath(installWorkDir)
		toolsets, err := loader.LoadToolsets(toolsetsPath)
		if err != nil {
			return fmt.Errorf("加载工具集列表失败: %w", err)
		}
		
		if len(toolsets) == 0 {
			return fmt.Errorf("available-toolsets.json 中没有找到工具集")
		}
		
		// 创建安装器
		inst := installer.NewInstaller(installToolsetsDir, installWorkDir)
		
		// 设置版本（如果指定）
		if installVersion != "" {
			inst.SetVersion(installVersion)
		}
		
		// 安装工具集
		if len(args) > 0 {
			// 安装指定工具集
			toolsetName := args[0]
			toolset := loader.FindToolset(toolsets, toolsetName)
			if toolset == nil {
				return fmt.Errorf("未找到工具集: %s", toolsetName)
			}
			
			// 安装依赖
			if len(toolset.Dependencies) > 0 {
				fmt.Printf("📦 安装依赖...\n")
				for _, depName := range toolset.Dependencies {
					dep := loader.FindToolset(toolsets, depName)
					if dep == nil {
						fmt.Printf("  ⚠️  未找到依赖: %s\n", depName)
						continue
					}
					
					// 检查依赖是否已安装
					depPath := filepath.Join(installToolsetsDir, dep.Name)
					if _, err := os.Stat(depPath); err == nil {
						fmt.Printf("  ✅ 依赖 %s 已安装\n", dep.DisplayName)
						continue
					}
					
					fmt.Printf("  📦 安装依赖: %s\n", dep.DisplayName)
					if err := inst.InstallToolset(dep); err != nil {
						return fmt.Errorf("安装依赖 %s 失败: %w", dep.Name, err)
					}
				}
				fmt.Println()
			}
			
			return inst.InstallToolset(toolset)
		} else {
			// 安装所有工具集
			fmt.Printf("📦 开始安装 %d 个工具集...\n\n", len(toolsets))
			
			// 记录已安装的工具集
			installed := make(map[string]bool)
			
			for i, toolset := range toolsets {
				fmt.Printf("[%d/%d] ", i+1, len(toolsets))
				
				// 检查是否已安装（包括作为依赖安装的）
				if installed[toolset.Name] {
					fmt.Printf("⏭️  %s 已作为依赖安装，跳过\n", toolset.DisplayName)
					continue
				}
				
				// 安装依赖
				if len(toolset.Dependencies) > 0 {
					for _, depName := range toolset.Dependencies {
						if installed[depName] {
							continue
						}
						
						dep := loader.FindToolset(toolsets, depName)
						if dep == nil {
							fmt.Printf("  ⚠️  未找到依赖: %s\n", depName)
							continue
						}
						
						fmt.Printf("  📦 安装依赖: %s\n", dep.DisplayName)
						if err := inst.InstallToolset(dep); err != nil {
							return fmt.Errorf("安装依赖 %s 失败: %w", dep.Name, err)
						}
						installed[dep.Name] = true
					}
				}
				
				// 安装工具集本身
				if err := inst.InstallToolset(toolset); err != nil {
					return fmt.Errorf("安装工具集 %s 失败: %w", toolset.Name, err)
				}
				installed[toolset.Name] = true
				
				if i < len(toolsets)-1 {
					fmt.Println()
				}
			}
			fmt.Printf("\n✅ 所有工具集安装完成\n")
		}
		
		return nil
	},
}

func init() {
	installCmd.Flags().StringVarP(&installToolsetsDir, "toolsets-dir", "d", "", "工具集安装目录（默认: .cursor/toolsets）")
	installCmd.Flags().StringVarP(&installWorkDir, "work-dir", "w", "", "工作目录（默认: 当前目录）")
	installCmd.Flags().StringVarP(&installVersion, "version", "v", "", "指定安装版本（Git 标签或提交哈希）")
}


