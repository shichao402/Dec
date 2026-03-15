package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/vault"
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
  └── vault.yaml        Vault 资产声明

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
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	mgr := config.NewProjectConfigManagerV2(cwd)
	if mgr.Exists() {
		fmt.Println("⚠️  项目已初始化")
		fmt.Println()
		fmt.Println("💡 运行 dec sync 同步 Vault 资产")
		return nil
	}

	projectName := filepath.Base(cwd)

	fmt.Printf("📦 初始化 Dec 配置: %s\n", projectName)
	fmt.Printf("   目录: %s\n\n", cwd)

	if err := mgr.InitProject(initProjectIDEs); err != nil {
		return fmt.Errorf("初始化失败: %w", err)
	}

	fmt.Println("  ✅ 创建 .dec/config/ides.yaml")
	fmt.Println("  ✅ 创建 .dec/config/vault.yaml")

	if !vault.IsDecSkillInstalled() {
		if err := vault.InstallDecSkill(); err != nil {
			fmt.Printf("  ⚠️  安装 Dec Skill 失败: %v\n", err)
		} else {
			skillPath, _ := vault.GetDecSkillPath()
			fmt.Printf("  ✅ 安装 Dec Skill: %s\n", skillPath)
		}
	}

	fmt.Println("\n✅ 初始化完成！")
	fmt.Println("\n📝 下一步：")
	fmt.Println("   1. 运行 dec vault init 初始化个人知识仓库")
	fmt.Println("   2. 编辑 .dec/config/vault.yaml 声明需要的资产")
	fmt.Println("   3. 运行 dec sync 同步到 IDE")

	return nil
}
