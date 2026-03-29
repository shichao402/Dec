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

	repoDir, err := repo.GetRepoDir()
	if err != nil {
		return err
	}

	// 在 repo 中创建 vault 目录结构
	vaultDir := filepath.Join(repoDir, vaultName)
	if _, err := os.Stat(vaultDir); err == nil {
		fmt.Printf("Vault '%s' 已存在于仓库中，跳过创建\n", vaultName)
	} else {
		fmt.Printf("📦 创建 Vault 空间: %s\n", vaultName)
		for _, sub := range []string{"skills", "rules", "mcp"} {
			if err := os.MkdirAll(filepath.Join(vaultDir, sub), 0755); err != nil {
				return fmt.Errorf("创建 %s/%s 目录失败: %w", vaultName, sub, err)
			}
			// 添加 .gitkeep 保证空目录被 git 跟踪
			gitkeep := filepath.Join(vaultDir, sub, ".gitkeep")
			if err := os.WriteFile(gitkeep, []byte(""), 0644); err != nil {
				return fmt.Errorf("创建 .gitkeep 失败: %w", err)
			}
		}

		// 提交到仓库
		warnings, err := repo.CommitAndPush(fmt.Sprintf("vault: 创建 %s", vaultName))
		if err != nil {
			return fmt.Errorf("提交失败: %w", err)
		}
		for _, w := range warnings {
			fmt.Printf("⚠️  %s\n", w)
		}
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
	fmt.Printf("  dec vault save skill <path>     # 保存 Skill 到 %s\n", vaultName)
	fmt.Println("  dec vault list                  # 列出所有 Vault")
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
	targetVault, err := resolveTargetVault(projectConfig, saveVault)
	if err != nil {
		return err
	}

	repoDir, err := repo.GetRepoDir()
	if err != nil {
		return err
	}

	// 验证 vault 目录存在
	vaultDir := filepath.Join(repoDir, targetVault)
	if _, err := os.Stat(vaultDir); os.IsNotExist(err) {
		return fmt.Errorf("Vault '%s' 不存在于仓库中\n\n运行 dec vault init %s 先创建 Vault", targetVault, targetVault)
	}

	// 解析源路径
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("解析路径失败: %w", err)
	}

	// 保存资产到 vault 目录
	assetName, err := saveAssetToVault(itemType, absSource, vaultDir)
	if err != nil {
		return err
	}

	fmt.Printf("📦 保存 %s '%s' 到 Vault '%s'\n", itemType, assetName, targetVault)

	// 提交并推送
	commitMsg := fmt.Sprintf("save: %s/%s/%s", targetVault, itemType, assetName)
	warnings, err := repo.CommitAndPush(commitMsg)
	if err != nil {
		return fmt.Errorf("提交失败: %w", err)
	}
	for _, w := range warnings {
		fmt.Printf("⚠️  %s\n", w)
	}

	// 记录到项目资产追踪
	assetsConfig, _ := mgr.LoadAssetsConfig()
	assetsConfig.AddAsset(itemType, assetName, targetVault, time.Now().Format(time.RFC3339))
	_ = mgr.SaveAssetsConfig(assetsConfig)

	fmt.Printf("✅ %s '%s' 已保存到 Vault '%s'\n", itemType, assetName, targetVault)

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
		fmt.Printf("  %-24s %s\n", v, summary)
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
	query := strings.ToLower(args[0])

	// 先从远程拉取最新
	if err := repo.Pull(); err != nil {
		fmt.Printf("⚠️  拉取远程最新失败: %v\n", err)
	}

	repoDir, err := repo.GetRepoDir()
	if err != nil {
		return err
	}

	// 遍历所有 vault，搜索匹配的资产
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

	if !isValidAssetType(itemType) {
		return fmt.Errorf("不支持的资产类型: %s (支持: skill, rule, mcp)", itemType)
	}

	// 先从远程拉取最新
	if err := repo.Pull(); err != nil {
		fmt.Printf("⚠️  拉取远程最新失败: %v\n", err)
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

	repoDir, err := repo.GetRepoDir()
	if err != nil {
		return err
	}

	// 查找资产所在的 vault
	foundVault, assetPath, err := findAssetInVaults(repoDir, projectConfig.Vaults, itemType, assetName)
	if err != nil {
		return err
	}

	fmt.Printf("📥 从 Vault '%s' 下载 %s '%s'\n", foundVault, itemType, assetName)

	// 确定目标 IDE 列表
	ideNames, err := config.GetEffectiveIDEs(projectConfig)
	if err != nil {
		ideNames = []string{"cursor"}
	}

	// 为每个 IDE 安装资产
	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)
		if err := installAssetToIDE(itemType, assetName, assetPath, cwd, ideImpl); err != nil {
			fmt.Printf("⚠️  安装到 %s 失败: %v\n", ideName, err)
			continue
		}
	}

	// 更新 assets.yaml
	assetsConfig, _ := mgr.LoadAssetsConfig()
	assetsConfig.AddAsset(itemType, assetName, foundVault, time.Now().Format(time.RFC3339))
	if err := mgr.SaveAssetsConfig(assetsConfig); err != nil {
		fmt.Printf("⚠️  更新资产追踪失败: %v\n", err)
	}

	fmt.Printf("✅ %s '%s' 已下载到项目 (IDE: %s)\n", itemType, assetName, strings.Join(ideNames, ", "))

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

	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return fmt.Errorf("加载项目配置失败: %w", err)
	}

	repoDir, err := repo.GetRepoDir()
	if err != nil {
		return err
	}

	ideNames, err := config.GetEffectiveIDEs(projectConfig)
	if err != nil {
		ideNames = []string{"cursor"}
	}

	// 收集所有已追踪的资产
	allAssets := collectAllAssets(assetsConfig)
	if len(allAssets) == 0 {
		fmt.Println("当前项目没有追踪的 Vault 资产")
		return nil
	}

	fmt.Printf("📤 检查 %d 个已追踪资产...\n\n", len(allAssets))

	pushed := 0
	for _, asset := range allAssets {
		// 找到本地资产路径（从第一个 IDE 目录）
		ideImpl := ide.Get(ideNames[0])
		localPath := getLocalAssetPath(asset.Type, asset.Name, cwd, ideImpl)

		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			continue // 本地文件不存在，跳过
		}

		// 复制回 repo
		vaultDir := filepath.Join(repoDir, asset.Vault)
		if _, err := os.Stat(vaultDir); os.IsNotExist(err) {
			continue // vault 不存在，跳过
		}

		destPath := getAssetPath(repoDir, asset.Vault, asset.Type, asset.Name)
		switch asset.Type {
		case "skill":
			if err := copyDir(localPath, destPath); err != nil {
				fmt.Printf("⚠️  推送 %s/%s 失败: %v\n", asset.Type, asset.Name, err)
				continue
			}
		case "rule", "mcp":
			if err := copyFile(localPath, destPath); err != nil {
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

	// 提交并推送
	commitMsg := fmt.Sprintf("push: 更新 %d 个资产", pushed)
	warnings, err := repo.CommitAndPush(commitMsg)
	if err != nil {
		return fmt.Errorf("提交失败: %w", err)
	}
	for _, w := range warnings {
		fmt.Printf("⚠️  %s\n", w)
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

	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)
		if err := removeAssetFromIDE(itemType, assetName, cwd, ideImpl); err != nil {
			fmt.Printf("⚠️  从 %s 移除失败: %v\n", ideName, err)
		}
	}

	// 从 assets.yaml 移除
	assetsConfig, _ := mgr.LoadAssetsConfig()
	removed := assetsConfig.RemoveAsset(itemType, assetName)
	if removed {
		_ = mgr.SaveAssetsConfig(assetsConfig)
	}

	// 如果 --remote，也从 vault 仓库中删除
	if removeRemote {
		repoDir, err := repo.GetRepoDir()
		if err != nil {
			return fmt.Errorf("获取仓库目录失败: %w", err)
		}

		// 查找资产在哪个 vault 中
		vaultName, assetPath, err := findAssetInVaults(repoDir, projectConfig.Vaults, itemType, assetName)
		if err != nil {
			fmt.Printf("⚠️  远程资产未找到: %v\n", err)
		} else {
			// 删除文件
			if err := os.RemoveAll(assetPath); err != nil {
				return fmt.Errorf("删除远程资产失败: %w", err)
			}

			commitMsg := fmt.Sprintf("remove: %s/%s/%s", vaultName, itemType, assetName)
			warnings, err := repo.CommitAndPush(commitMsg)
			if err != nil {
				return fmt.Errorf("提交失败: %w", err)
			}
			for _, w := range warnings {
				fmt.Printf("⚠️  %s\n", w)
			}

			fmt.Printf("  已从远程 Vault '%s' 删除\n", vaultName)
		}
	}

	fmt.Printf("✅ %s '%s' 已移除\n", itemType, assetName)

	return nil
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

// removeAssetFromIDE 从指定 IDE 的项目目录中删除资产
func removeAssetFromIDE(itemType, assetName, projectRoot string, ideImpl ide.IDE) error {
	managed := managedName(assetName)

	switch itemType {
	case "skill":
		destDir := filepath.Join(ideImpl.SkillsDir(projectRoot), managed)
		return os.RemoveAll(destDir)

	case "rule":
		destPath := filepath.Join(ideImpl.RulesDir(projectRoot), managed+".mdc")
		if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil

	case "mcp":
		existingConfig, err := ideImpl.LoadMCPConfig(projectRoot)
		if err != nil {
			return nil // 配置不存在则跳过
		}
		delete(existingConfig.MCPServers, managed)
		return ideImpl.WriteMCPConfig(projectRoot, existingConfig)
	}
	return nil
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
// 注册命令
// ========================================

func init() {
	// vault save 标志
	vaultSaveCmd.Flags().StringVar(&saveVault, "vault", "", "目标 Vault（项目关联多个时必填）")

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
