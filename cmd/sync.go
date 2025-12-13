package cmd

import (
	"fmt"
	"os"

	"github.com/shichao402/Dec/pkg/service"
	"github.com/spf13/cobra"
)

var syncNewCmd = &cobra.Command{
	Use:   "sync",
	Short: "同步规则和 MCP 配置",
	Long: `根据项目配置同步规则文件和 MCP 配置。

此命令会：
1. 读取 .dec/config/ 中的配置
2. 生成 .cursor/rules/*.mdc 规则文件
3. 生成 .cursor/mcp.json MCP 配置

示例：
  dec sync`,
	RunE: runSyncRules,
}

func init() {
	RootCmd.AddCommand(syncNewCmd)
}

func runSyncRules(cmd *cobra.Command, args []string) error {
	// 获取当前目录
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	fmt.Println("🔄 同步规则和 MCP 配置...")
	fmt.Println()

	// 创建同步服务并执行
	svc := service.NewSyncService(cwd)
	result, err := svc.Sync()
	if err != nil {
		return err
	}

	// 打印结果
	printSyncResult(result)

	return nil
}

// printSyncResult 打印同步结果
func printSyncResult(result *service.SyncResult) {
	fmt.Printf("📦 项目: %s\n", result.ProjectName)
	fmt.Printf("🎯 目标 IDE: %v\n", result.IDEs)

	for ideName, ideResult := range result.IDEResults {
		fmt.Printf("\n━━━ %s ━━━\n", ideName)

		// 规则
		fmt.Printf("📜 生成规则文件...\n")
		fmt.Printf("  ✅ 核心规则 (%d 个)\n", ideResult.CoreRulesCount)

		for _, name := range ideResult.BuiltinPacks {
			fmt.Printf("  ✅ %s (内置)\n", name)
		}

		for _, name := range ideResult.ExternalPacks {
			fmt.Printf("  ✅ %s\n", name)
		}

		// MCP
		fmt.Printf("🔌 生成 MCP 配置 (%d 个包)...\n", len(ideResult.MCPPacks)+1) // +1 for dec itself
		fmt.Printf("  ✅ dec\n")
		for _, name := range ideResult.MCPPacks {
			fmt.Printf("  ✅ %s\n", name)
		}

		fmt.Printf("   规则目录: %s\n", ideResult.RulesDir)
		fmt.Printf("   MCP 配置: %s\n", ideResult.MCPConfigPath)
	}

	fmt.Println("\n✅ 同步完成！")
}
