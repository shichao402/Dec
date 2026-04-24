package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/app"
	"github.com/shichao402/Dec/pkg/bundle"
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
并推送到仓库。Bundle 声明文件（.dec/cache/<vault>/bundles/*.yaml）
会在启用资产推送完成后被扫描并一并推送。

删除远程资产需明确指定 --remove 加 <type> <name>：
  dec push --remove skill my-skill
  dec push --remove bundle my-bundle

示例：
  dec push                                  # 推送所有修改（含 bundle 声明）
  dec push --remove skill my-skill          # 删除远程资产（需确认）
  dec push --remove bundle my-bundle        # 删除远程 bundle 声明`,
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

		// 扫描并推送 bundle 声明文件
		// bundle 没有「启用/未启用」的单条目列表，但项目修改的 bundle 声明仍然
		// 通过 .dec/cache/<vault>/bundles/*.yaml 形态存在，push 时统一扫描。
		bundlePushed, err := pushBundles(cwd, repoDir)
		if err != nil {
			return err
		}
		pushed += bundlePushed

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

// pushBundles 扫描 .dec/cache/<vault>/bundles/*.yaml 并推送到 repo。
//
// 对每个 bundle YAML 文件，先用 bundle.Validate 做语法与成员格式校验，
// 校验失败的文件会被跳过并输出警告，不会中断其它 bundle 的推送。
// 校验通过后按 resolveAssetFile 规则复制到 repo 中对应位置。
func pushBundles(projectRoot, repoDir string) (int, error) {
	cacheRoot := filepath.Join(projectRoot, ".dec", "cache")
	vaultEntries, err := os.ReadDir(cacheRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取 .dec/cache 目录失败: %w", err)
	}

	pushed := 0
	for _, vaultEntry := range vaultEntries {
		if !vaultEntry.IsDir() || strings.HasPrefix(vaultEntry.Name(), ".") {
			continue
		}
		vault := vaultEntry.Name()
		bundlesDir := filepath.Join(cacheRoot, vault, "bundles")
		entries, err := os.ReadDir(bundlesDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return pushed, fmt.Errorf("读取 %s 失败: %w", bundlesDir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}

			cachePath := filepath.Join(bundlesDir, name)
			data, err := os.ReadFile(cachePath)
			if err != nil {
				fmt.Printf("⚠️  读取 bundle %s/%s 失败: %v\n", vault, name, err)
				continue
			}
			if _, err := bundle.Validate(data, cachePath); err != nil {
				fmt.Printf("⚠️  校验 bundle %s/%s 失败: %v\n", vault, name, err)
				continue
			}

			// 写到 repo：统一规范扩展名为 .yaml
			assetName := strings.TrimSuffix(strings.TrimSuffix(name, ".yml"), ".yaml")
			destPath := resolveAssetFile(repoDir, vault, "bundle", assetName)
			if err := copyFile(cachePath, destPath); err != nil {
				fmt.Printf("⚠️  推送 bundle/%s 失败: %v\n", assetName, err)
				continue
			}

			fmt.Printf("  [bundle] %s -> %s\n", assetName, vault)
			pushed++
		}
	}
	return pushed, nil
}

func runPushRemove(itemType, assetName string) error {
	if !isValidAssetType(itemType) {
		return fmt.Errorf("不支持的资产类型: %s (支持: skill, rule, mcp, bundle)", itemType)
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

	// bundle 没有 IDE 安装副作用，走单独的删除路径：
	// 只从 repo 中删除 bundle 声明文件，同时清理本地 .dec/cache 中对应副本。
	if itemType == "bundle" {
		return runPushRemoveBundle(cwd, assetName)
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

// runPushRemoveBundle 从远程仓库删除 bundle 声明文件，并清理本地缓存副本。
//
// 与 skill/rule/mcp 不同，bundle 不做 IDE 安装、不维护 enabled 列表条目，
// 所以不能复用 app.RemoveAsset（它会去处理 IDE 清理和 AssetList 移除）。
func runPushRemoveBundle(projectRoot, assetName string) error {
	var removedVault string
	if err := withWriteRepo(func(tx *repo.Transaction) error {
		repoDir := tx.WorkDir()
		vault, fullPath, err := findAssetInRepo(repoDir, "bundle", assetName)
		if err != nil {
			return err
		}
		if err := os.Remove(fullPath); err != nil {
			return fmt.Errorf("删除 bundle 文件失败: %w", err)
		}
		removedVault = vault

		commitMsg := fmt.Sprintf("push: 删除 bundle %s", assetName)
		if err := tx.CommitAndPush(commitMsg); err != nil {
			return fmt.Errorf("提交失败: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	// 清理本地缓存副本，保持与远程一致
	removeCachedAsset("bundle", assetName, projectRoot, removedVault)

	fmt.Printf("✅ bundle '%s' 已从远程删除 (vault: %s)\n", assetName, removedVault)
	return nil
}

func init() {
	pushCmd.Flags().BoolVar(&pushRemove, "remove", false, "删除远程资产（需指定 <type> <name>）")
	RootCmd.AddCommand(pushCmd)
}
