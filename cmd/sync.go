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
2. 从包缓存中读取规则和 MCP
3. 根据配置生成规则文件到 IDE 目录
4. 生成 MCP 配置文件

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

	// 创建新版同步服务并执行
	svc, err := service.NewSyncServiceV2(cwd)
	if err != nil {
		return fmt.Errorf("创建同步服务失败: %w", err)
	}

	result, err := svc.Sync()
	if err != nil {
		return err
	}

	// 打印结果
	printSyncResultV2(result)

	return nil
}

// printSyncResultV2 打印同步结果
func printSyncResultV2(result *service.SyncResultV2) {
	fmt.Printf("📦 项目: %s\n", result.ProjectName)
	fmt.Printf("🎯 目标 IDE: %v\n", result.IDEs)
	fmt.Println()

	fmt.Printf("📜 规则:\n")
	fmt.Printf("   核心规则: %d 个\n", result.CoreRulesCount)
	fmt.Printf("   技术栈规则: %d 个\n", result.TechRulesCount)
	fmt.Println()

	fmt.Printf("🔧 MCP: %d 个\n", result.MCPCount)
	fmt.Println()

	fmt.Println("✅ 同步完成！")
}
