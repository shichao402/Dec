package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/spf13/cobra"
)

var (
	initProjectIDEs []string
)

var initNewCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化项目 Dec 配置",
	Long: `初始化项目的 Dec 配置，创建 .dec/config/ 目录结构。

生成的配置文件：
  .dec/config/
  ├── ides.yaml         目标 IDE 配置
  ├── technology.yaml   技术栈配置
  └── mcp.yaml          MCP 配置

配置文件根据已缓存的包自动生成可用选项。
如果没有可用的包，请先运行 'dec update' 更新包缓存。

示例：
  dec init                        # 初始化
  dec init --ide cursor           # 指定目标 IDE`,
	RunE: runInitProject,
}

func init() {
	RootCmd.AddCommand(initNewCmd)
	initNewCmd.Flags().StringSliceVar(&initProjectIDEs, "ide", []string{"cursor"}, "目标 IDE (cursor, codebuddy, windsurf, trae)")
}

func runInitProject(cmd *cobra.Command, args []string) error {
	// 获取当前目录
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	// 检查是否已初始化
	mgr := config.NewProjectConfigManagerV2(cwd)
	if mgr.Exists() {
		fmt.Println("⚠️  项目已初始化")
		fmt.Println()
		fmt.Println("💡 运行 dec sync 同步规则和 MCP 配置")
		return nil
	}

	// 检查是否有可用的包
	scanner, err := config.NewScanner()
	if err == nil && !scanner.HasPackages() {
		fmt.Println("⚠️  没有可用的包缓存")
		fmt.Println()
		fmt.Println("请先运行 'dec update' 更新包缓存，然后再初始化项目。")
		return nil
	}

	projectName := filepath.Base(cwd)

	fmt.Printf("📦 初始化 Dec 配置: %s\n", projectName)
	fmt.Printf("   目录: %s\n\n", cwd)

	// 初始化项目
	if err := mgr.InitProject(initProjectIDEs); err != nil {
		return fmt.Errorf("初始化失败: %w", err)
	}

	fmt.Println("  ✅ 创建 .dec/config/ides.yaml")
	fmt.Println("  ✅ 创建 .dec/config/technology.yaml")
	fmt.Println("  ✅ 创建 .dec/config/mcp.yaml")

	fmt.Println("\n✅ 初始化完成！")
	fmt.Println("\n📝 下一步：")
	fmt.Println("   1. 编辑 .dec/config/ides.yaml 配置目标 IDE")
	fmt.Println("   2. 编辑 .dec/config/technology.yaml 配置技术栈")
	fmt.Println("   3. 编辑 .dec/config/mcp.yaml 启用需要的 MCP")
	fmt.Println("   4. 运行 dec sync 同步规则和 MCP 配置")

	return nil
}
