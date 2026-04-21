package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/pkg/app"
	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/spf13/cobra"
)

var pushRemove bool

var pushCmd = &cobra.Command{
	Use:   "push [<type> <name>]",
	Short: "推送本地修改到仓库",
	Long: `推送项目中修改的资产到远程仓库。

此命令检测 .dec/cache/ 中已启用资产的修改，
并推送到仓库。

删除远程资产需明确指定 --remove 加 <type> <name>：
  dec push --remove skill my-skill

示例：
  dec push                                  # 推送所有修改
  dec push --remove skill my-skill          # 删除远程资产（需确认）`,
	Args: cobra.ArbitraryArgs,
	RunE: runPush,
}

func runPush(cmd *cobra.Command, args []string) error {
	if pushRemove {
		if len(args) != 2 {
			return fmt.Errorf("--remove 需要指定 <type> <name>，例如: dec push --remove skill my-skill")
		}
		return runPushRemove(args[0], args[1])
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	mgr := config.NewProjectConfigManager(cwd)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return fmt.Errorf("加载项目配置失败: %w", err)
	}

	if projectConfig.Enabled.IsEmpty() {
		fmt.Println("当前项目没有已启用的资产")
		return nil
	}

	enabledAssets := projectConfig.Enabled.All()
	fmt.Printf("📤 检查 %d 个已启用资产...\n\n", len(enabledAssets))

	pushed := 0
	if err := withWriteRepo(func(tx *repo.Transaction) error {
		repoDir := tx.WorkDir()
		for _, asset := range enabledAssets {
			// 从 .dec/cache/ 读取
			cachePath := getCachePath(cwd, asset.Vault, asset.Type, asset.Name)
			if _, err := os.Stat(cachePath); os.IsNotExist(err) {
				continue
			}

			destPath := resolveAssetFile(repoDir, asset.Vault, asset.Type, asset.Name)
			switch asset.Type {
			case "skill":
				if err := copyDir(cachePath, destPath); err != nil {
					fmt.Printf("⚠️  推送 %s/%s 失败: %v\n", asset.Type, asset.Name, err)
					continue
				}
			case "rule", "mcp":
				if err := copyFile(cachePath, destPath); err != nil {
					fmt.Printf("⚠️  推送 %s/%s 失败: %v\n", asset.Type, asset.Name, err)
					continue
				}
			}

			fmt.Printf("  [%s] %s -> %s\n", asset.Type, asset.Name, asset.Vault)
			pushed++
		}

		if pushed == 0 {
			fmt.Println("没有需要推送的变更")
			return nil
		}

		commitMsg := fmt.Sprintf("push: 更新 %d 个资产", pushed)
		if err := tx.CommitAndPush(commitMsg); err != nil {
			return fmt.Errorf("提交失败: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if pushed > 0 {
		fmt.Printf("\n✅ 已推送 %d 个资产到远程仓库\n", pushed)
	}
	return nil
}

func runPushRemove(itemType, assetName string) error {
	if !isValidAssetType(itemType) {
		return fmt.Errorf("不支持的资产类型: %s (支持: skill, rule, mcp)", itemType)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	// 交互式确认
	fmt.Printf("⚠️  确认从远程仓库删除 %s '%s'? (y/N): ", itemType, assetName)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input != "y" && input != "yes" {
		fmt.Println("已取消")
		return nil
	}

	reporter := app.ReporterFunc(func(event app.OperationEvent) {
		switch event.Level {
		case app.EventWarn, app.EventError:
			fmt.Printf("  %s\n", event.Message)
		default:
			fmt.Printf("  %s\n", event.Message)
		}
	})

	result, err := app.RemoveAsset(app.RemoveAssetInput{
		ProjectRoot: cwd,
		Type:        itemType,
		Name:        assetName,
		Confirmed:   true,
	}, reporter)
	if err != nil {
		return err
	}

	fmt.Printf("✅ %s '%s' 已从远程删除 (vault: %s)\n", itemType, assetName, result.Vault)
	return nil
}

func init() {
	pushCmd.Flags().BoolVar(&pushRemove, "remove", false, "删除远程资产（需指定 <type> <name>）")
	RootCmd.AddCommand(pushCmd)
}
