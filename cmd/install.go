package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/firoyang/CursorToolset/pkg/installer"
	"github.com/firoyang/CursorToolset/pkg/loader"
	"github.com/spf13/cobra"
)

var (
	installToolsetsDir string
	installWorkDir     string
)

var installCmd = &cobra.Command{
	Use:   "install [toolset-name]",
	Short: "安装工具集",
	Long: `安装一个或多个工具集。

如果不指定工具集名称，将安装 toolsets.json 中列出的所有工具集。
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
		if installToolsetsDir == "" {
			installToolsetsDir = filepath.Join(installWorkDir, "toolsets")
		}
		
		// 加载工具集列表
		toolsetsPath := loader.GetToolsetsPath(installWorkDir)
		toolsets, err := loader.LoadToolsets(toolsetsPath)
		if err != nil {
			return fmt.Errorf("加载工具集列表失败: %w", err)
		}
		
		if len(toolsets) == 0 {
			return fmt.Errorf("toolsets.json 中没有找到工具集")
		}
		
		// 创建安装器
		inst := installer.NewInstaller(installToolsetsDir, installWorkDir)
		
		// 安装工具集
		if len(args) > 0 {
			// 安装指定工具集
			toolsetName := args[0]
			toolset := loader.FindToolset(toolsets, toolsetName)
			if toolset == nil {
				return fmt.Errorf("未找到工具集: %s", toolsetName)
			}
			
			return inst.InstallToolset(toolset)
		} else {
			// 安装所有工具集
			fmt.Printf("📦 开始安装 %d 个工具集...\n\n", len(toolsets))
			for i, toolset := range toolsets {
				fmt.Printf("[%d/%d] ", i+1, len(toolsets))
				if err := inst.InstallToolset(toolset); err != nil {
					return fmt.Errorf("安装工具集 %s 失败: %w", toolset.Name, err)
				}
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
	installCmd.Flags().StringVarP(&installToolsetsDir, "toolsets-dir", "d", "", "工具集安装目录（默认: ./toolsets）")
	installCmd.Flags().StringVarP(&installWorkDir, "work-dir", "w", "", "工作目录（默认: 当前目录）")
}

