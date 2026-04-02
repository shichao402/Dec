package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/shichao402/Dec/pkg/vars"
	"github.com/spf13/cobra"
)

var vaultCmd = &cobra.Command{
	Use:   "vault",
	Short: "管理 Vault 空间和资产",
	Long: `在 Dec 仓库中创建和管理多个 Vault 空间。

每个 Vault 是一个逻辑空间，用于组织特定项目的 Skills、Rules 和 MCP。
一个项目可以关联多个 Vault，通过 dec vault pull/push/remove 管理资产。

示例：
  # 创建 Vault 空间
  dec vault init github-tools

  # 导入资产到 Vault
  dec vault import skill ./my-skill --vault github-tools

  # 在新项目中搜索和使用资产
  dec vault list
  dec vault search "API"
  dec vault pull skill my-skill`,
}

// ========================================
// vault init
// ========================================

var vaultInitCmd = &cobra.Command{
	Use:   "init <vault-name>",
	Short: "创建 Vault 空间",
	Long: `在连接的仓库中创建一个新的 Vault 空间。

Vault 是 Dec 仓库中的逻辑空间，用于组织和管理 Skills、Rules、MCP。
同一个项目可以关联多个 Vault。

示例：
  dec vault init github-tools       # 创建名为 github-tools 的 Vault
  dec vault init common-rules`,
	Args: cobra.ExactArgs(1),
	RunE: runVaultInit,
}

func runVaultInit(cmd *cobra.Command, args []string) error {
	vaultName := args[0]

	// 确保仓库已连接
	connected, err := repo.IsConnected()
	if err != nil {
		return fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return fmt.Errorf("仓库未连接\n\n运行 dec repo <url> 先连接你的仓库")
	}

	if err := withWriteRepo(func(tx *repo.Transaction) error {
		vaultDir := filepath.Join(tx.WorkDir(), vaultName)
		if _, err := os.Stat(vaultDir); err == nil {
			fmt.Printf("Vault '%s' 已存在于仓库中，跳过创建\n", vaultName)
			return nil
		}

		fmt.Printf("📦 创建 Vault 空间: %s\n", vaultName)
		for _, sub := range []string{"skills", "rules", "mcp"} {
			if err := os.MkdirAll(filepath.Join(vaultDir, sub), 0755); err != nil {
				return fmt.Errorf("创建 %s/%s 目录失败: %w", vaultName, sub, err)
			}
			gitkeep := filepath.Join(vaultDir, sub, ".gitkeep")
			if err := os.WriteFile(gitkeep, []byte(""), 0644); err != nil {
				return fmt.Errorf("创建 .gitkeep 失败: %w", err)
			}
		}

		if err := tx.CommitAndPush(fmt.Sprintf("vault: 创建 %s", vaultName)); err != nil {
			return fmt.Errorf("提交失败: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	// 在项目中创建 .dec/ 配置
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	mgr := config.NewProjectConfigManager(cwd)
	if !mgr.Exists() {
		// 首次初始化：创建完整配置
		ides, err := config.GetEffectiveIDEs(nil)
		if err != nil {
			ides = []string{"cursor"}
		}
		if err := mgr.InitProject(vaultName, ides); err != nil {
			return fmt.Errorf("初始化项目配置失败: %w", err)
		}
	} else {
		// 项目已初始化：只添加 vault 关联
		if err := mgr.AddVault(vaultName); err != nil {
			return fmt.Errorf("添加 Vault 关联失败: %w", err)
		}
	}

	fmt.Printf("✅ Vault '%s' 已初始化\n", vaultName)
	fmt.Println("\n后续步骤:")
	fmt.Printf("  dec vault import skill <path>   # 导入 Skill 到 %s\n", vaultName)
	fmt.Println("  dec vault list                  # 列出所有 Vault")
	fmt.Println("  dec vault pull skill <name>     # 从 Vault 下载资产")

	return nil
}

// ========================================
// vault import
// ========================================

var (
	importVault string
	pullAll     bool
	pullVault   string
)

var vaultImportCmd = &cobra.Command{
	Use:   "import <type> <path>",
	Short: "导入资产到 Vault",
	Long: `导入本地资产到 Vault。

支持的资产类型：
  skill   Skill 目录（包含 SKILL.md）
  rule    规则文件（.mdc）
  mcp     MCP 配置文件 (JSON，包含 command/args/env)

资产导入到当前项目关联的 Vault 中。
如果项目关联多个 Vault，通过 --vault 指定目标。

示例：
  dec vault import skill ./my-skill
  dec vault import rule ./rules/logging.mdc
  dec vault import mcp ./mcp-config.json --vault github-tools`,
	Args: cobra.ExactArgs(2),
	RunE: runVaultImport,
}

func runVaultImport(cmd *cobra.Command, args []string) error {
	itemType := args[0]
	sourcePath := args[1]

	// 验证资产类型
	if !isValidAssetType(itemType) {
		return fmt.Errorf("不支持的资产类型: %s (支持: skill, rule, mcp)", itemType)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	// 获取项目配置（包括关联的 Vault）
	mgr := config.NewProjectConfigManager(cwd)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return fmt.Errorf("加载项目配置失败: %w", err)
	}

	// 确定目标 Vault
	targetVault, err := resolveTargetVault(projectConfig, importVault)
	if err != nil {
		return err
	}

	// 解析源路径
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("解析路径失败: %w", err)
	}

	var assetName string
	if err := withWriteRepo(func(tx *repo.Transaction) error {
		vaultDir := filepath.Join(tx.WorkDir(), targetVault)
		if _, err := os.Stat(vaultDir); os.IsNotExist(err) {
			return fmt.Errorf("Vault '%s' 不存在于仓库中\n\n运行 dec vault init %s 先创建 Vault", targetVault, targetVault)
		}

		name, err := saveAssetToVault(itemType, absSource, vaultDir)
		if err != nil {
			return err
		}
		assetName = name

		fmt.Printf("📦 导入 %s '%s' 到 Vault '%s'\n", itemType, assetName, targetVault)
		commitMsg := fmt.Sprintf("import: %s/%s/%s", targetVault, itemType, assetName)
		if err := tx.CommitAndPush(commitMsg); err != nil {
			return fmt.Errorf("提交失败: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	// 记录到项目资产追踪
	assetsConfig, err := mgr.LoadAssetsConfig()
	if err != nil {
		return fmt.Errorf("加载资产追踪失败: %w", err)
	}
	assetsConfig.AddAsset(itemType, assetName, targetVault, time.Now().Format(time.RFC3339))
	if err := mgr.SaveAssetsConfig(assetsConfig); err != nil {
		return fmt.Errorf("保存资产追踪失败: %w", err)
	}

	// 保存模板到 .dec/templates/
	if err := withReadRepoDir(func(repoDir string) error {
		assetPath := getAssetPath(repoDir, targetVault, itemType, assetName)
		return saveAssetTemplate(itemType, assetName, assetPath, cwd, targetVault)
	}); err != nil {
		fmt.Printf("⚠️  保存模板失败: %v\n", err)
	}

	fmt.Printf("✅ %s '%s' 已导入到 Vault '%s'\n", itemType, assetName, targetVault)

	return nil
}

// ========================================
// vault list
// ========================================

var vaultListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有 Vault 空间",
	Long: `列出当前仓库中的所有 Vault 空间及其资产详情。

示例：
  dec vault list`,
	RunE: runVaultList,
}

func runVaultList(cmd *cobra.Command, args []string) error {
	return withReadRepoDir(func(repoDir string) error {
		entries, err := os.ReadDir(repoDir)
		if err != nil {
			return fmt.Errorf("读取仓库目录失败: %w", err)
		}

		var vaults []string
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				vaults = append(vaults, entry.Name())
			}
		}

		if len(vaults) == 0 {
			fmt.Println("仓库中还没有 Vault 空间")
			fmt.Println("\n💡 运行 dec vault init <vault-name> 创建 Vault")
			return nil
		}

		fmt.Printf("📦 Vault 空间 (%d 个):\n", len(vaults))
		for _, v := range vaults {
			vaultDir := filepath.Join(repoDir, v)
			assets := listVaultAssets(vaultDir, v)

			skillCount, ruleCount, mcpCount := 0, 0, 0
			for _, a := range assets {
				switch a.Type {
				case "skill":
					skillCount++
				case "rule":
					ruleCount++
				case "mcp":
					mcpCount++
				}
			}
			var parts []string
			if skillCount > 0 {
				parts = append(parts, fmt.Sprintf("%d skills", skillCount))
			}
			if ruleCount > 0 {
				parts = append(parts, fmt.Sprintf("%d rules", ruleCount))
			}
			if mcpCount > 0 {
				parts = append(parts, fmt.Sprintf("%d mcps", mcpCount))
			}
			summary := "(空)"
			if len(parts) > 0 {
				summary = strings.Join(parts, ", ")
			}
			fmt.Printf("\n  %s  (%s)\n", v, summary)

			if len(assets) > 0 {
				for _, a := range assets {
					fmt.Printf("    [%-5s] %s\n", a.Type, a.Name)
				}
			}
		}

		return nil
	})
}

// ========================================
// vault search
// ========================================

var vaultSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "搜索 Vault 中的资产",
	Long: `在所有已连接的 Vault 中搜索资产。

匹配资产名称和元数据。

示例：
  dec vault search "API"
  dec vault search "test"`,
	Args: cobra.ExactArgs(1),
	RunE: runVaultSearch,
}

func runVaultSearch(cmd *cobra.Command, args []string) error {
	query := strings.ToLower(args[0])

	return withReadRepoDir(func(repoDir string) error {
		entries, err := os.ReadDir(repoDir)
		if err != nil {
			return fmt.Errorf("读取仓库目录失败: %w", err)
		}

		var results []vaultAssetInfo
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			vaultDir := filepath.Join(repoDir, entry.Name())
			assets := listVaultAssets(vaultDir, entry.Name())
			for _, a := range assets {
				if strings.Contains(strings.ToLower(a.Name), query) {
					results = append(results, a)
				}
			}
		}

		if len(results) == 0 {
			fmt.Printf("未找到匹配 \"%s\" 的资产\n", args[0])
			return nil
		}

		fmt.Printf("🔍 搜索 \"%s\"，找到 %d 个结果:\n\n", args[0], len(results))
		for _, r := range results {
			fmt.Printf("  [%s] %-24s  (vault: %s)\n", r.Type, r.Name, r.Vault)
		}

		fmt.Println("\n💡 使用 dec vault pull <type> <name> 下载资产")
		return nil
	})
}

// ========================================
// vault pull
// ========================================

var vaultPullCmd = &cobra.Command{
	Use:   "pull [<type> <name>]",
	Short: "从 Vault 下载资产到项目",
	Long: `从 Vault 下载资产到当前项目。

pull 会：
1. 从远程仓库拉取资产
2. 复制到项目的 IDE 目录
3. 记录到 .dec/assets.yaml

使用 --all 批量拉取所有资产：
  dec vault pull --all                    # 拉取所有 Vault 的所有资产
  dec vault pull --all --vault my-vault   # 拉取指定 Vault 的所有资产

拉取单个资产：
  dec vault pull skill my-skill
  dec vault pull rule logging-standard`,
	Args: cobra.ArbitraryArgs,
	RunE: runVaultPull,
}

func runVaultPull(cmd *cobra.Command, args []string) error {
	if pullAll {
		return runVaultPullAll()
	}

	// 单个资产模式：需要恰好 2 个参数
	if len(args) != 2 {
		return fmt.Errorf("需要指定 <type> <name>，或使用 --all 批量拉取")
	}

	itemType := args[0]
	assetName := args[1]

	return pullSingleAsset(itemType, assetName)
}

func pullSingleAsset(itemType, assetName string) error {
	if !isValidAssetType(itemType) {
		return fmt.Errorf("不支持的资产类型: %s (支持: skill, rule, mcp)", itemType)
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

	var foundVault string
	var ideNames []string
	if err := withReadRepoDir(func(repoDir string) error {
		vaultName, assetPath, err := findAssetInVaults(repoDir, projectConfig.Vaults, itemType, assetName)
		if err != nil {
			return err
		}
		foundVault = vaultName

		fmt.Printf("📥 从 Vault '%s' 下载 %s '%s'\n", foundVault, itemType, assetName)

		// 保存原始模板到 .dec/templates/
		if err := saveAssetTemplate(itemType, assetName, assetPath, cwd, foundVault); err != nil {
			return fmt.Errorf("保存模板失败: %w", err)
		}

		ideNames, err = config.GetEffectiveIDEs(projectConfig)
		if err != nil {
			ideNames = []string{"cursor"}
		}
		if err := installAssetToIDEs(itemType, assetName, assetPath, cwd, ideNames); err != nil {
			return err
		}

		fmt.Printf("✅ %s '%s' 已下载到项目 (IDE: %s)\n", itemType, assetName, strings.Join(ideNames, ", "))
		return nil
	}); err != nil {
		return err
	}

	// 占位符替换（对 IDE 目录中的文件）
	substituteAssetVars(itemType, assetName, cwd, ideNames, mgr)

	assetsConfig, err := mgr.LoadAssetsConfig()
	if err != nil {
		return fmt.Errorf("加载资产追踪失败: %w", err)
	}
	assetsConfig.AddAsset(itemType, assetName, foundVault, time.Now().Format(time.RFC3339))
	if err := mgr.SaveAssetsConfig(assetsConfig); err != nil {
		return fmt.Errorf("保存资产追踪失败: %w", err)
	}

	return nil
}

func runVaultPullAll() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	mgr := config.NewProjectConfigManager(cwd)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return fmt.Errorf("加载项目配置失败: %w", err)
	}

	ideNames, err := config.GetEffectiveIDEs(projectConfig)
	if err != nil {
		ideNames = []string{"cursor"}
	}

	assetsConfig, err := mgr.LoadAssetsConfig()
	if err != nil {
		return fmt.Errorf("加载资产追踪失败: %w", err)
	}

	pulled := 0
	failed := 0
	processed := false
	if err := withReadRepoDir(func(repoDir string) error {
		var targetVaults []string
		if pullVault != "" {
			vaultDir := filepath.Join(repoDir, pullVault)
			if _, err := os.Stat(vaultDir); os.IsNotExist(err) {
				return fmt.Errorf("Vault '%s' 不存在", pullVault)
			}
			targetVaults = []string{pullVault}
		} else {
			entries, err := os.ReadDir(repoDir)
			if err != nil {
				return fmt.Errorf("读取仓库目录失败: %w", err)
			}
			for _, entry := range entries {
				if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
					targetVaults = append(targetVaults, entry.Name())
				}
			}
		}

		if len(targetVaults) == 0 {
			fmt.Println("仓库中还没有 Vault 空间")
			return nil
		}

		var allAssets []vaultAssetInfo
		for _, v := range targetVaults {
			vaultDir := filepath.Join(repoDir, v)
			assets := listVaultAssets(vaultDir, v)
			allAssets = append(allAssets, assets...)
		}

		if len(allAssets) == 0 {
			fmt.Println("没有可拉取的资产")
			return nil
		}

		processed = true
		fmt.Printf("📥 批量拉取 %d 个资产 (来自 %d 个 Vault)...\n\n", len(allAssets), len(targetVaults))
		for _, asset := range allAssets {
			assetPath := getAssetPath(repoDir, asset.Vault, asset.Type, asset.Name)
			if assetPath == "" {
				continue
			}

			if err := installAssetToIDEs(asset.Type, asset.Name, assetPath, cwd, ideNames); err == nil {
				// 保存原始模板
				if err := saveAssetTemplate(asset.Type, asset.Name, assetPath, cwd, asset.Vault); err != nil {
					fmt.Printf("  ⚠️  保存模板失败 %s/%s: %v\n", asset.Type, asset.Name, err)
				}
				substituteAssetVars(asset.Type, asset.Name, cwd, ideNames, mgr)
				fmt.Printf("  ✅ [%-5s] %s  (from %s)\n", asset.Type, asset.Name, asset.Vault)
				assetsConfig.AddAsset(asset.Type, asset.Name, asset.Vault, time.Now().Format(time.RFC3339))
				pulled++
			} else {
				fmt.Printf("  ⚠️  [%-5s] %s (%v)\n", asset.Type, asset.Name, err)
				failed++
			}
		}

		return nil
	}); err != nil {
		return err
	}

	if !processed {
		return nil
	}

	if pulled > 0 {
		if err := mgr.SaveAssetsConfig(assetsConfig); err != nil {
			return fmt.Errorf("保存资产追踪失败: %w", err)
		}
	}

	fmt.Printf("\n✅ 完成：%d 个资产已拉取", pulled)
	if failed > 0 {
		fmt.Printf("，%d 个失败", failed)
	}
	fmt.Printf(" (IDE: %s)\n", strings.Join(ideNames, ", "))

	return nil
}

// ========================================
// vault push
// ========================================

var vaultPushCmd = &cobra.Command{
	Use:   "push",
	Short: "推送本地修改到 Vault",
	Long: `推送项目中修改的资产到远程 Vault。

此命令检测 .dec/assets.yaml 中追踪的资产修改，
并推送到对应的 Vault。

示例：
  dec vault push`,
	RunE: runVaultPush,
}

func runVaultPush(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	mgr := config.NewProjectConfigManager(cwd)
	assetsConfig, err := mgr.LoadAssetsConfig()
	if err != nil {
		return fmt.Errorf("加载资产追踪失败: %w", err)
	}

	allAssets := collectAllAssets(assetsConfig)
	if len(allAssets) == 0 {
		fmt.Println("当前项目没有追踪的 Vault 资产")
		return nil
	}

	fmt.Printf("📤 检查 %d 个已追踪资产...\n\n", len(allAssets))

	pushed := 0
	if err := withWriteRepo(func(tx *repo.Transaction) error {
		repoDir := tx.WorkDir()
		for _, asset := range allAssets {
			// 从 .dec/templates/ 读取原始模板
			templatePath := getTemplatePath(cwd, asset.Vault, asset.Type, asset.Name)
			if _, err := os.Stat(templatePath); os.IsNotExist(err) {
				continue
			}

			vaultDir := filepath.Join(repoDir, asset.Vault)
			if _, err := os.Stat(vaultDir); os.IsNotExist(err) {
				continue
			}

			destPath := getAssetPath(repoDir, asset.Vault, asset.Type, asset.Name)
			switch asset.Type {
			case "skill":
				if err := copyDir(templatePath, destPath); err != nil {
					fmt.Printf("⚠️  推送 %s/%s 失败: %v\n", asset.Type, asset.Name, err)
					continue
				}
			case "rule", "mcp":
				if err := copyFile(templatePath, destPath); err != nil {
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

	if pushed == 0 {
		return nil
	}

	fmt.Printf("\n✅ 已推送 %d 个资产到远程仓库\n", pushed)
	return nil
}

// ========================================
// vault remove
// ========================================

var (
	removeRemote bool
)

var vaultRemoveCmd = &cobra.Command{
	Use:   "remove <type> <name>",
	Short: "从项目移除资产",
	Long: `从项目中移除已安装的资产。

默认只从本地项目移除。
使用 --remote 也删除远程 Vault 中的资产。

示例：
  dec vault remove skill my-skill          # 只从项目移除
  dec vault remove rule logging --remote   # 从项目和 Vault 移除`,
	Args: cobra.ExactArgs(2),
	RunE: runVaultRemove,
}

func runVaultRemove(cmd *cobra.Command, args []string) error {
	itemType := args[0]
	assetName := args[1]

	if !isValidAssetType(itemType) {
		return fmt.Errorf("不支持的资产类型: %s (支持: skill, rule, mcp)", itemType)
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

	// 从所有 IDE 目录中删除
	ideNames, err := config.GetEffectiveIDEs(projectConfig)
	if err != nil {
		ideNames = []string{"cursor"}
	}

	fmt.Printf("🗑️  移除 %s '%s'\n", itemType, assetName)

	local := false
	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)
		if removed, err := removeAssetFromIDE(itemType, assetName, cwd, ideImpl); err != nil {
			fmt.Printf("⚠️  从 %s 移除失败: %v\n", ideName, err)
		} else if removed {
			local = true
		}
	}

	// 从 assets.yaml 移除
	assetsConfig, err := mgr.LoadAssetsConfig()
	if err != nil {
		return fmt.Errorf("加载资产追踪失败: %w", err)
	}
	trackedAsset := assetsConfig.FindAsset(itemType, assetName)

	// 清理 .dec/templates/ 中的模板
	if trackedAsset != nil && trackedAsset.Vault != "" {
		removeAssetTemplate(itemType, assetName, cwd, trackedAsset.Vault)
	}

	removed := assetsConfig.RemoveAsset(itemType, assetName)
	if removed {
		if err := mgr.SaveAssetsConfig(assetsConfig); err != nil {
			return fmt.Errorf("保存资产追踪失败: %w", err)
		}
		local = true
	}

	remote := false
	// 如果 --remote，也从 vault 仓库中删除
	if removeRemote {
		if err := withWriteRepo(func(tx *repo.Transaction) error {
			repoDir := tx.WorkDir()
			var targetVault string
			var assetPath string

			if trackedAsset != nil && trackedAsset.Vault != "" {
				targetVault = trackedAsset.Vault
				assetPath = getAssetPath(repoDir, targetVault, itemType, assetName)
				if _, err := os.Stat(assetPath); err != nil {
					if os.IsNotExist(err) {
						fmt.Printf("⚠️  远程资产未找到（vault: %s）\n", targetVault)
					} else {
						return fmt.Errorf("检查远程资产失败: %w", err)
					}
					targetVault = ""
					assetPath = ""
				}
			} else {
				foundVault, foundPath, err := findAssetInVaults(repoDir, projectConfig.Vaults, itemType, assetName)
				if err != nil {
					fmt.Printf("⚠️  远程资产未找到\n")
				} else {
					targetVault = foundVault
					assetPath = foundPath
				}
			}

			if targetVault == "" || assetPath == "" {
				return nil
			}
			if err := os.RemoveAll(assetPath); err != nil {
				return fmt.Errorf("删除远程资产失败: %w", err)
			}

			commitMsg := fmt.Sprintf("remove: %s/%s/%s", targetVault, itemType, assetName)
			if err := tx.CommitAndPush(commitMsg); err != nil {
				return fmt.Errorf("提交失败: %w", err)
			}

			fmt.Printf("  已从远程 Vault '%s' 删除\n", targetVault)
			remote = true
			return nil
		}); err != nil {
			return err
		}
	}

	// 只在真的删除了东西时才显示成功
	if !local && !remote {
		return fmt.Errorf("%s '%s' 不存在或已被删除", itemType, assetName)
	}

	fmt.Printf("✅ %s '%s' 已移除\n", itemType, assetName)

	return nil
}

func withReadRepoDir(fn func(string) error) error {
	tx, err := repo.NewReadTransaction()
	if err != nil {
		return err
	}
	defer tx.Close()
	return fn(tx.WorkDir())
}

func withWriteRepo(fn func(*repo.Transaction) error) error {
	tx, err := repo.NewWriteTransaction()
	if err != nil {
		return err
	}
	defer tx.Close()
	return fn(tx)
}

// ========================================
// 辅助函数
// ========================================

// isValidAssetType 检查资产类型是否有效
func isValidAssetType(t string) bool {
	return t == "skill" || t == "rule" || t == "mcp"
}

// resolveTargetVault 根据项目配置确定目标 vault
func resolveTargetVault(projectConfig *types.ProjectConfig, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if len(projectConfig.Vaults) == 1 {
		return projectConfig.Vaults[0], nil
	}
	if len(projectConfig.Vaults) > 1 {
		return "", fmt.Errorf("项目关联多个 Vault，请通过 --vault 指定目标:\n  %s", strings.Join(projectConfig.Vaults, ", "))
	}
	return "", fmt.Errorf("项目未关联任何 Vault，请先运行 dec vault init <vault-name>")
}

// saveAssetToVault 将资产复制到 vault 目录，返回资产名称
func saveAssetToVault(itemType, absSource, vaultDir string) (string, error) {
	info, err := os.Stat(absSource)
	if err != nil {
		return "", fmt.Errorf("源路径不存在: %w", err)
	}

	switch itemType {
	case "skill":
		return saveSkillToVault(absSource, info, vaultDir)
	case "rule":
		return saveRuleToVault(absSource, vaultDir)
	case "mcp":
		return saveMCPToVault(absSource, vaultDir)
	default:
		return "", fmt.Errorf("不支持的资产类型: %s", itemType)
	}
}

func saveSkillToVault(absSource string, info os.FileInfo, vaultDir string) (string, error) {
	if !info.IsDir() {
		return "", fmt.Errorf("skill 必须是目录（包含 SKILL.md）: %s", absSource)
	}

	// 检查 SKILL.md 存在
	if _, err := os.Stat(filepath.Join(absSource, "SKILL.md")); err != nil {
		return "", fmt.Errorf("skill 目录缺少 SKILL.md: %s", absSource)
	}

	name := filepath.Base(absSource)
	name = strings.TrimPrefix(name, "dec-") // 去掉 dec- 前缀
	destDir := filepath.Join(vaultDir, "skills", name)

	if err := copyDir(absSource, destDir); err != nil {
		return "", fmt.Errorf("复制 skill 失败: %w", err)
	}
	return name, nil
}

func saveRuleToVault(absSource, vaultDir string) (string, error) {
	if !strings.HasSuffix(absSource, ".mdc") {
		return "", fmt.Errorf("rule 文件必须是 .mdc 格式: %s", absSource)
	}

	name := strings.TrimSuffix(filepath.Base(absSource), ".mdc")
	name = strings.TrimPrefix(name, "dec-")
	destPath := filepath.Join(vaultDir, "rules", name+".mdc")

	if err := copyFile(absSource, destPath); err != nil {
		return "", fmt.Errorf("复制 rule 失败: %w", err)
	}
	return name, nil
}

func saveMCPToVault(absSource, vaultDir string) (string, error) {
	if !strings.HasSuffix(absSource, ".json") {
		return "", fmt.Errorf("MCP 配置必须是 .json 格式: %s", absSource)
	}

	// 验证 JSON 格式
	data, err := os.ReadFile(absSource)
	if err != nil {
		return "", fmt.Errorf("读取 MCP 配置失败: %w", err)
	}
	var server types.MCPServer
	if err := json.Unmarshal(data, &server); err != nil {
		return "", fmt.Errorf("MCP 配置必须是合法 JSON（包含 command/args/env）: %w", err)
	}
	if server.Command == "" {
		return "", fmt.Errorf("MCP 配置缺少 command 字段")
	}

	name := strings.TrimSuffix(filepath.Base(absSource), ".json")
	name = strings.TrimPrefix(name, "dec-")
	destPath := filepath.Join(vaultDir, "mcp", name+".json")

	if err := copyFile(absSource, destPath); err != nil {
		return "", fmt.Errorf("复制 MCP 配置失败: %w", err)
	}
	return name, nil
}

// findAssetInVaults 在所有 vault 中查找资产，返回 vault 名和本地路径
func findAssetInVaults(repoDir string, vaults []string, itemType, assetName string) (string, string, error) {
	for _, v := range vaults {
		assetPath := getAssetPath(repoDir, v, itemType, assetName)
		if _, err := os.Stat(assetPath); err == nil {
			return v, assetPath, nil
		}
	}

	// 如果关联的 vault 中没找到，遍历 repo 中所有 vault
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return "", "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		assetPath := getAssetPath(repoDir, entry.Name(), itemType, assetName)
		if _, err := os.Stat(assetPath); err == nil {
			return entry.Name(), assetPath, nil
		}
	}

	return "", "", fmt.Errorf("未找到 %s '%s'", itemType, assetName)
}

// findAllAssetInVaults 在所有 vault 中查找所有匹配的资产（用于删除重复）
type vaultResult struct {
	vault string
	path  string
}

func findAllAssetInVaults(repoDir string, vaults []string, itemType, assetName string) []vaultResult {
	var results []vaultResult
	visited := make(map[string]bool)

	// 首先查找关联的 vault
	for _, v := range vaults {
		assetPath := getAssetPath(repoDir, v, itemType, assetName)
		if _, err := os.Stat(assetPath); err == nil {
			results = append(results, vaultResult{vault: v, path: assetPath})
			visited[v] = true
		}
	}

	// 遍历所有 vault
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return results
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || visited[entry.Name()] {
			continue
		}
		assetPath := getAssetPath(repoDir, entry.Name(), itemType, assetName)
		if _, err := os.Stat(assetPath); err == nil {
			results = append(results, vaultResult{vault: entry.Name(), path: assetPath})
			visited[entry.Name()] = true
		}
	}

	return results
}

// getAssetPath 获取资产在 vault 中的路径
func getAssetPath(repoDir, vaultName, itemType, assetName string) string {
	switch itemType {
	case "skill":
		return filepath.Join(repoDir, vaultName, "skills", assetName)
	case "rule":
		return filepath.Join(repoDir, vaultName, "rules", assetName+".mdc")
	case "mcp":
		return filepath.Join(repoDir, vaultName, "mcp", assetName+".json")
	}
	return ""
}

// listVaultAssets 列出一个 vault 目录中的资产
func listVaultAssets(vaultDir, vaultName string) []vaultAssetInfo {
	var assets []vaultAssetInfo
	for _, subDir := range []string{"skills", "rules", "mcp"} {
		dir := filepath.Join(vaultDir, subDir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Name() == ".gitkeep" {
				continue
			}
			name := e.Name()
			assetType := subDir
			// 去掉后缀
			if subDir == "rules" {
				assetType = "rule"
				name = strings.TrimSuffix(name, ".mdc")
			} else if subDir == "mcp" {
				name = strings.TrimSuffix(name, ".json")
			} else {
				assetType = "skill"
			}
			assets = append(assets, vaultAssetInfo{
				Name:  name,
				Type:  assetType,
				Vault: vaultName,
			})
		}
	}
	return assets
}

type vaultAssetInfo struct {
	Name  string
	Type  string
	Vault string
}

// collectAllAssets 从 AssetsConfig 收集所有资产条目
func collectAllAssets(ac *types.AssetsConfig) []vaultAssetInfo {
	var all []vaultAssetInfo
	for _, a := range ac.Skills {
		all = append(all, vaultAssetInfo{Name: a.Name, Type: "skill", Vault: a.Vault})
	}
	for _, a := range ac.Rules {
		all = append(all, vaultAssetInfo{Name: a.Name, Type: "rule", Vault: a.Vault})
	}
	for _, a := range ac.MCPs {
		all = append(all, vaultAssetInfo{Name: a.Name, Type: "mcp", Vault: a.Vault})
	}
	return all
}

// getLocalAssetPath 获取资产在项目中的本地路径
func getLocalAssetPath(itemType, assetName, projectRoot string, ideImpl ide.IDE) string {
	managed := managedName(assetName)
	switch itemType {
	case "skill":
		return filepath.Join(ideImpl.SkillsDir(projectRoot), managed)
	case "rule":
		return filepath.Join(ideImpl.RulesDir(projectRoot), managed+".mdc")
	case "mcp":
		return ideImpl.MCPConfigPath(projectRoot)
	}
	return ""
}

// managedName 添加 dec- 前缀
func managedName(name string) string {
	if strings.HasPrefix(name, "dec-") {
		return name
	}
	return "dec-" + name
}

// installAssetToIDEs 将资产安装到多个 IDE；如果中途失败，会回滚已安装的 IDE
func installAssetToIDEs(itemType, assetName, srcPath, projectRoot string, ideNames []string) error {
	installed := make([]ide.IDE, 0, len(ideNames))

	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)
		if err := installAssetToIDE(itemType, assetName, srcPath, projectRoot, ideImpl); err != nil {
			rollbackErrors := rollbackInstalledAsset(itemType, assetName, projectRoot, installed)
			if len(rollbackErrors) > 0 {
				return fmt.Errorf("安装到 %s 失败: %v；回滚失败: %s", ideName, err, strings.Join(rollbackErrors, "; "))
			}
			return fmt.Errorf("安装到 %s 失败: %w", ideName, err)
		}
		installed = append(installed, ideImpl)
	}

	return nil
}

func rollbackInstalledAsset(itemType, assetName, projectRoot string, installed []ide.IDE) []string {
	var rollbackErrors []string

	for i := len(installed) - 1; i >= 0; i-- {
		ideImpl := installed[i]
		removed, err := removeAssetFromIDE(itemType, assetName, projectRoot, ideImpl)
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("%s: %v", ideImpl.Name(), err))
			continue
		}
		if !removed {
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("%s: 未找到已安装资产", ideImpl.Name()))
		}
	}

	return rollbackErrors
}

// installAssetToIDE 将资产安装到指定 IDE 的项目目录
func installAssetToIDE(itemType, assetName, srcPath, projectRoot string, ideImpl ide.IDE) error {
	managed := managedName(assetName)

	switch itemType {
	case "skill":
		destDir := filepath.Join(ideImpl.SkillsDir(projectRoot), managed)
		return copyDir(srcPath, destDir)

	case "rule":
		destDir := ideImpl.RulesDir(projectRoot)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return err
		}
		return copyFile(srcPath, filepath.Join(destDir, managed+".mdc"))

	case "mcp":
		// MCP 需要合并到 IDE 的 MCP 配置文件
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("读取 MCP 配置失败: %w", err)
		}
		var server types.MCPServer
		if err := json.Unmarshal(data, &server); err != nil {
			return fmt.Errorf("解析 MCP 配置失败: %w", err)
		}

		existingConfig, err := ideImpl.LoadMCPConfig(projectRoot)
		if err != nil {
			return fmt.Errorf("加载 IDE MCP 配置失败: %w", err)
		}
		if existingConfig.MCPServers == nil {
			existingConfig.MCPServers = make(map[string]types.MCPServer)
		}
		existingConfig.MCPServers[managed] = server
		return ideImpl.WriteMCPConfig(projectRoot, existingConfig)
	}
	return nil
}

// removeAssetFromIDE 从指定 IDE 的项目目录中删除资产，返回是否真的删除了东西
func removeAssetFromIDE(itemType, assetName, projectRoot string, ideImpl ide.IDE) (bool, error) {
	managed := managedName(assetName)

	switch itemType {
	case "skill":
		destDir := filepath.Join(ideImpl.SkillsDir(projectRoot), managed)
		_, err := os.Stat(destDir)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil // 目录不存在
			}
			return false, err
		}
		// 目录存在，删除它
		return true, os.RemoveAll(destDir)

	case "rule":
		destPath := filepath.Join(ideImpl.RulesDir(projectRoot), managed+".mdc")
		err := os.Remove(destPath)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil // 文件不存在
			}
			return false, err
		}
		return true, nil // 文件被删除

	case "mcp":
		existingConfig, err := ideImpl.LoadMCPConfig(projectRoot)
		if err != nil {
			return false, nil // 配置不存在则跳过
		}
		_, exists := existingConfig.MCPServers[managed]
		if !exists {
			return false, nil // 条目不存在
		}
		// 条目存在，删除它
		delete(existingConfig.MCPServers, managed)
		return true, ideImpl.WriteMCPConfig(projectRoot, existingConfig)
	}
	return false, nil
}

// ========================================
// 文件操作
// ========================================

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// ========================================
// 占位符替换
// ========================================

// substituteAssetVars 对已安装到 IDE 目录的资产执行变量替换
func substituteAssetVars(itemType, assetName, projectRoot string, ideNames []string, mgr *config.ProjectConfigManager) {
	// 加载变量定义
	globalVars, _ := config.LoadGlobalVars()
	projectVars, _ := mgr.LoadVarsConfig()

	// 如果两者都为空，跳过
	if (globalVars == nil || len(globalVars.Vars) == 0) && (projectVars == nil || len(projectVars.Vars) == 0) {
		if globalVars != nil && globalVars.Assets != nil {
			// 可能有资产级变量，继续
		} else if projectVars != nil && projectVars.Assets != nil {
			// 可能有资产级变量，继续
		} else {
			return
		}
	}

	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)

		switch itemType {
		case "skill":
			localPath := filepath.Join(ideImpl.SkillsDir(projectRoot), managedName(assetName))
			placeholders := vars.ExtractPlaceholdersFromDir(localPath)
			if len(placeholders) == 0 {
				continue
			}
			resolved := vars.ResolveVars(globalVars, projectVars, itemType, assetName, placeholders)
			_, missing, err := vars.SubstituteDir(localPath, resolved)
			if err != nil {
				fmt.Printf("  ⚠️  变量替换失败 (%s): %v\n", ideName, err)
				continue
			}
			printMissingVars(missing)

		case "rule":
			localPath := filepath.Join(ideImpl.RulesDir(projectRoot), managedName(assetName)+".mdc")
			placeholders := vars.ExtractPlaceholdersFromFile(localPath)
			if len(placeholders) == 0 {
				continue
			}
			resolved := vars.ResolveVars(globalVars, projectVars, itemType, assetName, placeholders)
			_, missing, err := vars.SubstituteFile(localPath, resolved)
			if err != nil {
				fmt.Printf("  ⚠️  变量替换失败 (%s): %v\n", ideName, err)
				continue
			}
			printMissingVars(missing)

		case "mcp":
			used, missing := substituteMCPVars(assetName, projectRoot, ideImpl, globalVars, projectVars)
			_ = used
			printMissingVars(missing)
		}
	}
}

// substituteMCPVars 对 MCP 配置中的指定条目执行变量替换
func substituteMCPVars(assetName, projectRoot string, ideImpl ide.IDE, globalVars, projectVars *types.VarsConfig) (map[string]string, []string) {
	managed := managedName(assetName)

	existingConfig, err := ideImpl.LoadMCPConfig(projectRoot)
	if err != nil {
		return nil, nil
	}

	server, ok := existingConfig.MCPServers[managed]
	if !ok {
		return nil, nil
	}

	// 收集所有占位符
	var allContent string
	if server.Env != nil {
		for _, v := range server.Env {
			allContent += v + "\n"
		}
	}
	for _, arg := range server.Args {
		allContent += arg + "\n"
	}
	allContent += server.Command

	placeholders := vars.ExtractPlaceholders(allContent)
	if len(placeholders) == 0 {
		return nil, nil
	}

	resolved := vars.ResolveVars(globalVars, projectVars, "mcp", assetName, placeholders)

	used := make(map[string]string)
	var missing []string

	// 替换 env 值中的占位符
	if server.Env != nil {
		newEnv := make(map[string]string)
		for k, v := range server.Env {
			newVal, u, m := vars.Substitute(v, resolved)
			newEnv[k] = newVal
			for uk, uv := range u {
				used[uk] = uv
			}
			missing = append(missing, m...)
		}
		server.Env = newEnv
	}

	// 替换 args 中的占位符
	for i, arg := range server.Args {
		newArg, u, m := vars.Substitute(arg, resolved)
		server.Args[i] = newArg
		for uk, uv := range u {
			used[uk] = uv
		}
		missing = append(missing, m...)
	}

	// 替换 command 中的占位符
	newCmd, u, m := vars.Substitute(server.Command, resolved)
	server.Command = newCmd
	for uk, uv := range u {
		used[uk] = uv
	}
	missing = append(missing, m...)

	if len(used) > 0 {
		existingConfig.MCPServers[managed] = server
		if err := ideImpl.WriteMCPConfig(projectRoot, existingConfig); err != nil {
			fmt.Printf("  ⚠️  写入 MCP 配置失败: %v\n", err)
		}
	}

	return used, missing
}

// saveAssetTemplate 保存原始模板到 .dec/templates/{vault}/{type}/{name}
func saveAssetTemplate(itemType, assetName, srcPath, projectRoot, vaultName string) error {
	destPath := getTemplatePath(projectRoot, vaultName, itemType, assetName)
	switch itemType {
	case "skill":
		return copyDir(srcPath, destPath)
	case "rule", "mcp":
		return copyFile(srcPath, destPath)
	}
	return nil
}

// getTemplatePath 获取资产模板在 .dec/templates/ 中的路径
func getTemplatePath(projectRoot, vaultName, itemType, assetName string) string {
	base := filepath.Join(projectRoot, ".dec", "templates", vaultName)
	switch itemType {
	case "skill":
		return filepath.Join(base, "skills", assetName)
	case "rule":
		return filepath.Join(base, "rules", assetName+".mdc")
	case "mcp":
		return filepath.Join(base, "mcp", assetName+".json")
	}
	return ""
}

// removeAssetTemplate 从 .dec/templates/ 中删除资产模板
func removeAssetTemplate(itemType, assetName, projectRoot, vaultName string) {
	templatePath := getTemplatePath(projectRoot, vaultName, itemType, assetName)
	if templatePath == "" {
		return
	}
	os.RemoveAll(templatePath)
}

// printMissingVars 打印缺失变量的警告（去重）
func printMissingVars(missing []string) {
	seen := map[string]bool{}
	for _, m := range missing {
		if !seen[m] {
			fmt.Printf("  ⚠️  变量 {{%s}} 未定义 (在 .dec/vars.yaml 中添加)\n", m)
			seen[m] = true
		}
	}
}

// ========================================
// 注册命令
// ========================================

func init() {
	// vault import 标志
	vaultImportCmd.Flags().StringVar(&importVault, "vault", "", "目标 Vault（项目关联多个时必填）")

	// vault pull 标志
	vaultPullCmd.Flags().BoolVar(&pullAll, "all", false, "批量拉取所有资产")
	vaultPullCmd.Flags().StringVar(&pullVault, "vault", "", "指定 Vault（配合 --all 使用）")

	// vault remove 标志
	vaultRemoveCmd.Flags().BoolVar(&removeRemote, "remote", false, "同时删除远程 Vault 中的资产")

	// 注册子命令
	vaultCmd.AddCommand(vaultInitCmd)
	vaultCmd.AddCommand(vaultImportCmd)
	vaultCmd.AddCommand(vaultListCmd)
	vaultCmd.AddCommand(vaultSearchCmd)
	vaultCmd.AddCommand(vaultPullCmd)
	vaultCmd.AddCommand(vaultPushCmd)
	vaultCmd.AddCommand(vaultRemoveCmd)

	RootCmd.AddCommand(vaultCmd)
}
