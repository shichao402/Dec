package cmd

import (
	"fmt"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   "repo <url>",
	Short: "连接个人仓库",
	Long: `连接用户准备好的 GitHub 仓库作为 Dec 的资产存储。

仓库可以是空仓库，Dec 会在其中按 vault 名称创建目录结构。

示例:
  dec repo https://github.com/user/my-dec-repo
  dec repo git@github.com:user/my-dec-repo.git`,
	Args: cobra.ExactArgs(1),
	RunE: runRepo,
}

func runRepo(cmd *cobra.Command, args []string) error {
	repoURL := args[0]

	fmt.Printf("📦 连接仓库: %s\n", repoURL)

	if err := repo.Connect(repoURL); err != nil {
		return err
	}

	// 保存仓库 URL 到全局配置
	if err := config.SetRepoURL(repoURL); err != nil {
		fmt.Printf("⚠️  保存配置失败: %v\n", err)
	}

	repoDir, _ := repo.GetRepoDir()
	fmt.Printf("✅ 仓库已连接: %s\n", repoDir)
	fmt.Println("\n后续步骤:")
	fmt.Println("  dec config global          # 配置本机 IDE")
	fmt.Println("  dec vault init <name>      # 创建 Vault 空间")

	return nil
}

func init() {
	RootCmd.AddCommand(repoCmd)
}
