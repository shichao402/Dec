package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/vault"
	"github.com/spf13/cobra"
)

var (
	vaultRepo   string
	vaultCreate string
	vaultTags   []string
	vaultType   string
)

var vaultCmd = &cobra.Command{
	Use:   "vault",
	Short: "管理个人知识仓库",
	Long: `管理 Dec 个人知识仓库（Vault），跨项目、跨机器积累和复用 AI 资产。

Vault 将 Skills、Rules、MCP 配置保存到个人 GitHub 仓库，
在任何新项目中都可以搜索和下载使用。

示例：
  # 初始化（克隆已有仓库）
  dec vault init --repo https://github.com/user/my-dec-vault

  # 初始化（创建新仓库）
  dec vault init --create my-dec-vault

  # 保存 skill 到 vault
  dec vault save skill .cursor/skills/my-skill

  # 搜索 vault
  dec vault find "API test"

  # 下载 skill 到当前项目
  dec vault pull skill my-skill

  # 列出所有资产
  dec vault list`,
}

// ========================================
// vault init
// ========================================

var vaultInitCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化个人知识仓库",
	Long: `初始化 Dec 个人知识仓库。

两种方式：
  --repo <url>     克隆已有的 GitHub 仓库
  --create <name>  创建新的 GitHub 私有仓库（需要 gh CLI）

示例：
  dec vault init --repo https://github.com/user/my-dec-vault
  dec vault init --create my-dec-vault`,
	RunE: runVaultInit,
}

func runVaultInit(cmd *cobra.Command, args []string) error {
	if vaultRepo == "" && vaultCreate == "" {
		return fmt.Errorf("请指定 --repo <url> 或 --create <name>")
	}

	var v *vault.Vault
	var err error

	if vaultRepo != "" {
		fmt.Printf("📦 克隆仓库: %s\n", vaultRepo)
		v, err = vault.Init(vaultRepo)
		if err != nil {
			return err
		}
	} else {
		fmt.Printf("📦 创建仓库: %s\n", vaultCreate)
		var warnings []string
		v, warnings, err = vault.InitCreate(vaultCreate)
		if err != nil {
			return err
		}
		for _, warning := range warnings {
			fmt.Printf("⚠️  %s\n", warning)
		}
	}

	fmt.Printf("✅ Vault 已初始化: %s\n", v.Dir)

	// 记录 vault 源到全局配置
	source := vaultRepo
	if source == "" {
		source = vaultCreate
	}
	if err := config.SetVaultSource(source); err != nil {
		fmt.Printf("⚠️  保存 Vault 配置失败: %v\n", err)
	}

	// 自动安装 Dec Skill
	if err := vault.InstallDecSkill(); err != nil {
		fmt.Printf("⚠️  安装 Dec Skill 失败: %v\n", err)
	} else {
		skillPath, _ := vault.GetDecSkillPath()
		fmt.Printf("✅ Dec Skill 已安装: %s\n", skillPath)
	}

	fmt.Println("\n📝 下一步：")
	fmt.Println("   1. 使用 dec vault save 保存资产到 Vault")
	fmt.Println("   2. 使用 dec vault find 搜索已有资产")
	fmt.Println("   3. 使用 dec vault pull 下载资产到项目")

	return nil
}

// ========================================
// vault save
// ========================================

var vaultSaveCmd = &cobra.Command{
	Use:   "save <type> <path>",
	Short: "保存资产到 Vault",
	Long: `保存本地资产到个人知识仓库。

支持的资产类型：
  skill   Skill 目录（包含 SKILL.md）
  rule    规则文件（.mdc）
  mcp     单个 MCP server 片段 JSON（包含 command/args/env）

示例：
  dec vault save skill .cursor/skills/my-skill
  dec vault save skill .cursor/skills/my-skill --tag testing --tag api
  dec vault save rule .cursor/rules/my-rule.mdc
  dec vault save mcp my-tool.json`,
	Args: cobra.ExactArgs(2),
	RunE: runVaultSave,
}

func runVaultSave(cmd *cobra.Command, args []string) error {
	itemType := args[0]
	sourcePath := args[1]

	v, err := vault.Open()
	if err != nil {
		return err
	}

	fmt.Printf("📦 保存 %s: %s\n", itemType, sourcePath)

	savedName, warnings, err := v.Save(itemType, sourcePath, vaultTags)
	if err != nil {
		return err
	}

	fmt.Printf("✅ 已保存到 Vault\n")
	for _, warning := range warnings {
		fmt.Printf("⚠️  %s\n", warning)
	}

	// 更新本地追踪
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("⚠️  获取当前目录失败，跳过本地追踪更新: %v\n", err)
		return nil
	}

	td, err := vault.LoadTracking(cwd)
	if err != nil {
		fmt.Printf("⚠️  加载本地追踪失败，跳过更新: %v\n", err)
		return nil
	}

	item := v.Index.Get(itemType, savedName)
	if item == nil {
		fmt.Printf("⚠️  Vault 索引中未找到已保存资产，跳过本地追踪更新: %s/%s\n", itemType, savedName)
		return nil
	}

	hash, err := hashLocalPath(sourcePath)
	if err != nil {
		fmt.Printf("⚠️  计算本地资产哈希失败，跳过本地追踪更新: %v\n", err)
		return nil
	}

	td.Track(item.Name, item.Type, sourcePath, hash)
	if err := td.Save(cwd); err != nil {
		fmt.Printf("⚠️  保存本地追踪失败: %v\n", err)
	}

	return nil
}

// ========================================
// vault find
// ========================================

var vaultFindCmd = &cobra.Command{
	Use:   "find <query>",
	Short: "搜索 Vault 中的资产",
	Long: `搜索个人知识仓库中的资产。

匹配名称、描述和标签。

示例：
  dec vault find "API test"
  dec vault find "logging"`,
	Args: cobra.ExactArgs(1),
	RunE: runVaultFind,
}

func runVaultFind(cmd *cobra.Command, args []string) error {
	query := args[0]

	v, err := vault.Open()
	if err != nil {
		return err
	}

	// 先从远程同步
	_ = v.Refresh()

	results := v.Find(query)
	if len(results) == 0 {
		fmt.Printf("未找到匹配 \"%s\" 的资产\n", query)
		return nil
	}

	fmt.Printf("🔍 搜索 \"%s\"，找到 %d 个结果:\n\n", query, len(results))
	printVaultItems(results)

	return nil
}

// ========================================
// vault pull
// ========================================

var vaultPullCmd = &cobra.Command{
	Use:   "pull <type> <name>",
	Short: "从 Vault 下载资产到当前项目",
	Long: `从个人知识仓库下载资产到当前项目。

pull 成功后会：
1. 把资产部署到当前项目配置的所有 IDE
2. 自动写入 .dec/config/vault.yaml
3. 更新本地追踪状态

示例：
  dec vault pull skill create-api-test
  dec vault pull rule my-logging-standard`,
	Args: cobra.ExactArgs(2),
	RunE: runVaultPull,
}

func runVaultPull(cmd *cobra.Command, args []string) error {
	itemType := args[0]
	name := args[1]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	v, err := vault.Open()
	if err != nil {
		return err
	}

	// 先从远程同步
	_ = v.Refresh()

	// 读取项目 IDE 配置，确定所有目标 IDE
	ideNames := resolveProjectIDEs(cwd)

	fmt.Printf("📥 下载 %s: %s\n", itemType, name)

	localPaths, err := v.Pull(itemType, name, cwd, ideNames)
	if err != nil {
		return err
	}

	mgr := config.NewProjectConfigManagerV2(cwd)
	if err := mgr.EnsureVaultItem(itemType, name); err != nil {
		return fmt.Errorf("更新项目 Vault 声明失败: %w", err)
	}

	// 更新本地追踪
	item := v.Index.Get(itemType, name)
	if item != nil {
		td, _ := vault.LoadTracking(cwd)
		if itemType == "mcp" && len(localPaths) > 1 {
			localPaths = localPaths[:1]
		}
		hash := ""
		if len(localPaths) > 0 {
			hash, _ = hashLocalPath(localPaths[0])
		}
		td.TrackPaths(name, itemType, localPaths, hash)
		_ = td.Save(cwd)
	}

	fmt.Printf("✅ 已下载到当前项目\n")
	return nil
}

// ========================================
// vault push
// ========================================

var vaultPushCmd = &cobra.Command{
	Use:   "push",
	Short: "推送 Vault 变更到远程仓库",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := vault.Open()
		if err != nil {
			return err
		}

		fmt.Println("📤 推送到远程仓库...")
		if err := v.Push(); err != nil {
			return err
		}

		fmt.Println("✅ 推送完成")
		return nil
	},
}

// ========================================
// vault list
// ========================================

var vaultListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出 Vault 中的资产",
	Long: `列出个人知识仓库中的所有资产。

示例：
  dec vault list              # 列出全部
  dec vault list --type skill # 只列出 skill`,
	RunE: runVaultList,
}

func runVaultList(cmd *cobra.Command, args []string) error {
	v, err := vault.Open()
	if err != nil {
		return err
	}

	items := v.List(vaultType)
	if len(items) == 0 {
		if vaultType != "" {
			fmt.Printf("Vault 中没有 %s 类型的资产\n", vaultType)
		} else {
			fmt.Println("Vault 为空")
		}
		fmt.Println("\n💡 使用 dec vault save 保存资产")
		return nil
	}

	if vaultType != "" {
		fmt.Printf("📦 Vault 资产（%s）: %d 个\n\n", vaultType, len(items))
	} else {
		fmt.Printf("📦 Vault 资产: %d 个\n\n", len(items))
	}

	printVaultItems(items)
	return nil
}

// ========================================
// vault status
// ========================================

var vaultStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "显示 Vault 同步状态",
	Long: `显示当前项目中追踪资产的变更状态。

示例：
  dec vault status`,
	RunE: runVaultStatus,
}

func runVaultStatus(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	td, err := vault.LoadTracking(cwd)
	if err != nil {
		return err
	}

	if len(td.Tracked) == 0 {
		fmt.Println("当前项目没有追踪任何 Vault 资产")
		return nil
	}

	v, _ := vault.Open()
	changes := td.CheckChanges(cwd, v)

	fmt.Printf("📊 追踪 %d 个资产\n\n", len(td.Tracked))
	fmt.Println(vault.FormatChanges(changes))

	return nil
}

// ========================================
// 辅助函数
// ========================================

func printVaultItems(items []vault.VaultItem) {
	for _, item := range items {
		fmt.Printf("  [%s] %s\n", item.Type, item.Name)
		if item.Description != "" {
			fmt.Printf("        %s\n", item.Description)
		}
		if len(item.Tags) > 0 {
			fmt.Printf("        标签: %s\n", strings.Join(item.Tags, ", "))
		}
		fmt.Printf("        更新: %s\n", item.UpdatedAt)
		fmt.Println()
	}
}

// resolveProjectIDEs 从项目配置解析所有目标 IDE 名称
func resolveProjectIDEs(projectRoot string) []string {
	mgr := config.NewProjectConfigManagerV2(projectRoot)
	idesConfig, err := mgr.LoadIDEsConfig()
	if err == nil && len(idesConfig.IDEs) > 0 {
		return idesConfig.IDEs
	}
	return []string{"cursor"}
}

func hashLocalPath(path string) (string, error) {
	return vault.HashPath(path)
}

// ========================================
// 注册命令
// ========================================

func init() {
	// vault init 标志
	vaultInitCmd.Flags().StringVar(&vaultRepo, "repo", "", "克隆已有的 GitHub 仓库 URL")
	vaultInitCmd.Flags().StringVar(&vaultCreate, "create", "", "创建新的 GitHub 私有仓库名称")

	// vault save 标志
	vaultSaveCmd.Flags().StringSliceVar(&vaultTags, "tag", nil, "资产标签（可多次指定）")

	// vault list 标志
	vaultListCmd.Flags().StringVar(&vaultType, "type", "", "筛选资产类型 (skill, rule, mcp)")

	// 注册子命令
	vaultCmd.AddCommand(vaultInitCmd)
	vaultCmd.AddCommand(vaultSaveCmd)
	vaultCmd.AddCommand(vaultFindCmd)
	vaultCmd.AddCommand(vaultPullCmd)
	vaultCmd.AddCommand(vaultPushCmd)
	vaultCmd.AddCommand(vaultListCmd)
	vaultCmd.AddCommand(vaultStatusCmd)

	RootCmd.AddCommand(vaultCmd)
}
