package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/spf13/cobra"
)

var (
	vaultTags []string
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

  # 保存资产到 Vault
  dec vault save skill ./my-skill --vault github-tools

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

	fmt.Printf("📦 初始化 Vault: %s\n", vaultName)

	// 在项目配置中记录 Vault 关联
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	mgr := config.NewProjectConfigManager(cwd)
	if err := mgr.AddVault(vaultName); err != nil {
		return fmt.Errorf("添加 Vault 关联失败: %w", err)
	}

	fmt.Printf("✅ Vault '%s' 已初始化\n", vaultName)
	fmt.Println("\n后续步骤:")
	fmt.Printf("  dec vault save skill <path>     # 保存 Skill 到 %s\n", vaultName)
	fmt.Println("  dec vault list                  # 列出所有 Vault 中的资产")
	fmt.Println("  dec vault pull skill <name>     # 从 Vault 下载资产")

	return nil
}

// ========================================
// vault save
// ========================================

var (
	saveVault string
)

var vaultSaveCmd = &cobra.Command{
	Use:   "save <type> <path>",
	Short: "保存资产到 Vault",
	Long: `保存本地资产到 Vault。

支持的资产类型：
  skill   Skill 目录（包含 SKILL.md）
  rule    规则文件（.mdc）
  mcp     MCP 配置文件 (JSON，包含 command/args/env)

资产保存到当前项目关联的 Vault 中。
如果项目关联多个 Vault，通过 --vault 指定目标。

示例：
  dec vault save skill ./my-skill
  dec vault save skill ./my-skill --tag testing --tag api
  dec vault save rule ./rules/logging.mdc
  dec vault save mcp ./mcp-config.json --vault github-tools`,
	Args: cobra.ExactArgs(2),
	RunE: runVaultSave,
}

func runVaultSave(cmd *cobra.Command, args []string) error {
	itemType := args[0]
	sourcePath := args[1]

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
	var targetVault string
	if saveVault != "" {
		targetVault = saveVault
	} else if len(projectConfig.Vaults) == 1 {
		targetVault = projectConfig.Vaults[0]
	} else if len(projectConfig.Vaults) > 1 {
		return fmt.Errorf("项目关联多个 Vault，请通过 --vault 指定目标:\n  %s", strings.Join(projectConfig.Vaults, ", "))
	} else {
		return fmt.Errorf("项目未关联任何 Vault，请先运行 dec vault init <vault-name>")
	}

	fmt.Printf("📦 保存 %s 到 Vault '%s': %s\n", itemType, targetVault, sourcePath)

	// 获取仓库 Git 实例来操作 Vault
	git, err := repo.GetGit()
	if err != nil {
		return err
	}

	repoDir, err := repo.GetRepoDir()
	if err != nil {
		return err
	}

	// 保存资产到 vault/{vaultName}/{type}/ 目录
	vaultAssetPath, warnings, err := saveAssetToVault(itemType, sourcePath, repoDir, targetVault, vaultTags)
	if err != nil {
		return err
	}

	for _, w := range warnings {
		fmt.Printf("⚠️  %s\n", w)
	}

	// 提交并推送
	if err := git.Add("."); err != nil {
		return fmt.Errorf("git add 失败: %w", err)
	}

	commitMsg := fmt.Sprintf("save: %s/%s %s", targetVault, itemType, vaultAssetPath)
	if err := git.Commit(commitMsg); err != nil {
		return fmt.Errorf("git commit 失败: %w", err)
	}

	if err := git.Push(); err != nil {
		fmt.Printf("⚠️  推送到远程失败，已保存到本地: %v\n", err)
	}

	// 记录到项目资产追踪
	assetsConfig, _ := mgr.LoadAssetsConfig()
	assetsConfig.AddAsset(itemType, vaultAssetPath, targetVault, time.Now().Format(time.RFC3339))
	_ = mgr.SaveAssetsConfig(assetsConfig)

	fmt.Println("✅ 资产已保存")

	return nil
}

// ========================================
// vault list
// ========================================

var vaultListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有 Vault 空间",
	Long: `列出当前仓库中的所有 Vault 空间。

示例：
  dec vault list`,
	RunE: runVaultList,
}

func runVaultList(cmd *cobra.Command, args []string) error {
	// 先从远程拉取最新
	if err := repo.Pull(); err != nil {
		fmt.Printf("⚠️  拉取远程最新失败: %v\n", err)
	}

	repoDir, err := repo.GetRepoDir()
	if err != nil {
		return err
	}

	// 列出 repo/{vault-name}/ 目录
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

	fmt.Printf("📦 Vault 空间 (%d 个):\n\n", len(vaults))
	for _, v := range vaults {
		fmt.Printf("  - %s\n", v)
	}

	return nil
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
	query := args[0]

	// TODO: 实现搜索逻辑
	// 遍历 repo/{vault-name}/{skill,rule,mcp}/ 目录
	// 根据文件名和元数据匹配查询

	fmt.Printf("🔍 搜索: %s\n\n", query)
	fmt.Println("(搜索功能开发中)")

	return nil
}

// ========================================
// vault pull
// ========================================

var vaultPullCmd = &cobra.Command{
	Use:   "pull <type> <name>",
	Short: "从 Vault 下载资产到项目",
	Long: `从 Vault 下载资产到当前项目。

pull 会：
1. 从远程仓库拉取资产
2. 复制到项目的 IDE 目录
3. 记录到 .dec/assets.yaml

示例：
  dec vault pull skill my-skill
  dec vault pull rule logging-standard`,
	Args: cobra.ExactArgs(2),
	RunE: runVaultPull,
}

func runVaultPull(cmd *cobra.Command, args []string) error {
	itemType := args[0]
	assetName := args[1]

	fmt.Printf("📥 下载 %s: %s\n", itemType, assetName)

	// TODO: 实现 pull 逻辑
	// 1. 从 repo/{vault-name}/{type}/{name}/ 查找资产
	// 2. 复制到项目 IDE 目录
	// 3. 更新 .dec/assets.yaml

	fmt.Println("(pull 功能开发中)")

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
	fmt.Println("📤 推送资产修改...")

	// TODO: 实现 push 逻辑
	// 1. 读取 .dec/assets.yaml
	// 2. 检查本地资产是否修改
	// 3. 同步回 repo/{vault-name}/{type}/
	// 4. git commit 和 push

	fmt.Println("(push 功能开发中)")

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

	fmt.Printf("🗑️  移除 %s: %s\n", itemType, assetName)

	if removeRemote {
		fmt.Println("   (同时移除远程)")
	}

	// TODO: 实现 remove 逻辑
	// 1. 从 .dec/assets.yaml 移除
	// 2. 从项目 IDE 目录删除文件
	// 3. 如果 --remote，从 repo/{vault-name}/{type}/ 删除

	fmt.Println("(remove 功能开发中)")

	return nil
}

// ========================================
// 辅助函数
// ========================================

func saveAssetToVault(itemType, sourcePath, repoDir, vaultName string, tags []string) (string, []string, error) {
	// TODO: 实现资产保存逻辑
	// 将资产复制到 repoDir/{vaultName}/{itemType}/{name}/

	return sourcePath, nil, nil
}

// ========================================
// 注册命令
// ========================================

func init() {
	// vault save 标志
	vaultSaveCmd.Flags().StringVar(&saveVault, "vault", "", "目标 Vault（项目关联多个时必填）")
	vaultSaveCmd.Flags().StringSliceVar(&vaultTags, "tag", nil, "资产标签（可多次指定）")

	// vault remove 标志
	vaultRemoveCmd.Flags().BoolVar(&removeRemote, "remote", false, "同时删除远程 Vault 中的资产")

	// 注册子命令
	vaultCmd.AddCommand(vaultInitCmd)
	vaultCmd.AddCommand(vaultSaveCmd)
	vaultCmd.AddCommand(vaultListCmd)
	vaultCmd.AddCommand(vaultSearchCmd)
	vaultCmd.AddCommand(vaultPullCmd)
	vaultCmd.AddCommand(vaultPushCmd)
	vaultCmd.AddCommand(vaultRemoveCmd)

	RootCmd.AddCommand(vaultCmd)
}
