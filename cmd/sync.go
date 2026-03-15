package cmd

import (
	"fmt"
	"os"

	"github.com/shichao402/Dec/pkg/service"
	"github.com/spf13/cobra"
)

var syncNewCmd = &cobra.Command{
	Use:   "sync",
	Short: "同步 Vault 资产到 IDE",
	Long: `根据项目配置，从个人知识仓库同步资产到 IDE 目录。

此命令会：
1. 读取 .dec/config/vault.yaml 中的声明
2. 从 Vault 拉取声明的 Skills、Rules、MCPs
3. 部署到所有配置的 IDE 目录

示例：
  dec sync`,
	RunE: runSyncRules,
}

func init() {
	RootCmd.AddCommand(syncNewCmd)
}

func runSyncRules(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	fmt.Println("🔄 同步 Vault 资产...")
	fmt.Println()

	svc, err := service.NewSyncServiceV2(cwd)
	if err != nil {
		return fmt.Errorf("创建同步服务失败: %w", err)
	}

	result, err := svc.Sync()
	if err != nil {
		return err
	}

	for _, warning := range result.Warnings {
		fmt.Printf("⚠️  %s\n", warning)
	}
	if len(result.Warnings) > 0 {
		fmt.Println()
	}

	fmt.Printf("📦 项目: %s\n", result.ProjectName)
	fmt.Printf("🎯 目标 IDE: %v\n", result.IDEs)
	fmt.Println()

	total := result.SkillsCount + result.RulesCount + result.MCPsCount
	if total > 0 {
		if result.SkillsCount > 0 {
			fmt.Printf("🧠 Skills: %d 个\n", result.SkillsCount)
		}
		if result.RulesCount > 0 {
			fmt.Printf("📜 Rules: %d 个\n", result.RulesCount)
		}
		if result.MCPsCount > 0 {
			fmt.Printf("🔧 MCPs: %d 个\n", result.MCPsCount)
		}
		fmt.Println()
	} else {
		fmt.Println("（未声明任何 Vault 资产，编辑 .dec/config/vault.yaml 添加）")
		fmt.Println()
	}

	fmt.Println("✅ 同步完成！")

	return nil
}
