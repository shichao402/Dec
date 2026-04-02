package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/assets"
	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/spf13/cobra"
)

var (
	configIDEs []string
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "管理 Dec 配置",
	Long: `管理 Dec 全局和项目级配置。

示例:
  dec config show                      # 显示当前配置
  dec config global                    # 配置全局 IDE（所有支持的 IDE）
  dec config global --ide cursor       # 只配置 Cursor
  dec config global --ide cursor --ide codebuddy  # 配置多个 IDE`,
}

// ========================================
// config show
// ========================================

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "显示当前配置",
	Long: `显示 Dec 的全局配置和项目级配置。

会展示以下信息:
  - Dec 根目录和仓库状态
  - 全局配置（RepoURL、机器级 IDE）
  - 项目配置（如果在 Dec 项目中）
  - 已安装的 Skills、Rules、MCP

示例:
  dec config show     # 显示完整配置`,
	RunE: runConfigShow,
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	fmt.Println("📋 Dec 配置信息")
	fmt.Println(strings.Repeat("=", 50))

	// ========================================
	// Dec 根目录和仓库状态
	// ========================================
	fmt.Println()
	fmt.Println("🏠 Dec 根目录和仓库")
	fmt.Println("-" + strings.Repeat("-", 48))

	decRootDir, err := repo.GetRootDir()
	if err != nil {
		return fmt.Errorf("获取 Dec 根目录失败: %w", err)
	}
	fmt.Printf("  根目录: %s\n", decRootDir)

	connected, err := repo.IsConnected()
	if err != nil {
		return fmt.Errorf("检查仓库连接失败: %w", err)
	}
	status := "❌ 未连接"
	if connected {
		status = "✅ 已连接"
	}
	fmt.Printf("  仓库状态: %s\n", status)

	if connected {
		bareRepoDir, err := repo.GetBareRepoDir()
		if err == nil {
			fmt.Printf("  裸仓库路径: %s\n", bareRepoDir)
		}

		defaultBranch, err := repo.GetDefaultBranch()
		if err == nil {
			fmt.Printf("  默认分支: %s\n", defaultBranch)
		}
	}

	// ========================================
	// 全局配置
	// ========================================
	fmt.Println()
	fmt.Println("🌍 全局配置")
	fmt.Println("-" + strings.Repeat("-", 48))

	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("加载全局配置失败: %w", err)
	}

	if globalConfig.RepoURL != "" {
		fmt.Printf("  Repo URL: %s\n", globalConfig.RepoURL)
	} else {
		fmt.Println("  Repo URL: (未配置)")
	}

	globalConfigPath, err := config.GetGlobalConfigPath()
	if err == nil {
		fmt.Printf("  配置文件: %s\n", globalConfigPath)
	}

	// ========================================
	// 机器级 IDE 配置
	// ========================================
	fmt.Println()
	fmt.Println("💻 机器级 IDE 配置")
	fmt.Println("-" + strings.Repeat("-", 48))

	localConfig, err := config.LoadLocalConfig()
	if err != nil {
		return fmt.Errorf("加载本机配置失败: %w", err)
	}

	localConfigPath, err := config.GetLocalConfigPath()
	if err == nil {
		fmt.Printf("  配置文件: %s\n", localConfigPath)
	}

	if len(localConfig.IDEs) > 0 {
		fmt.Printf("  IDE 列表: %s\n", strings.Join(localConfig.IDEs, ", "))
	} else {
		fmt.Println("  IDE 列表: (使用默认: cursor)")
	}

	// ========================================
	// 项目配置（如果在项目中）
	// ========================================
	cwd, err := os.Getwd()
	if err == nil {
		projectMgr := config.NewProjectConfigManager(cwd)

		if projectMgr.Exists() {
			fmt.Println()
			fmt.Println("📦 项目配置")
			fmt.Println("-" + strings.Repeat("-", 48))

			projectConfig, err := projectMgr.LoadProjectConfig()
			if err != nil {
				fmt.Printf("  ⚠️  加载项目配置失败: %v\n", err)
			} else {
				decDir := projectMgr.GetDecDir()
				fmt.Printf("  项目路径: %s\n", cwd)
				fmt.Printf("  .dec 目录: %s\n", decDir)

				if len(projectConfig.Vaults) > 0 {
					fmt.Printf("  关联 Vaults: %s\n", strings.Join(projectConfig.Vaults, ", "))
				} else {
					fmt.Println("  关联 Vaults: (无)")
				}

				if len(projectConfig.IDEs) > 0 {
					fmt.Printf("  IDE 覆盖: %s\n", strings.Join(projectConfig.IDEs, ", "))
				} else {
					fmt.Println("  IDE 覆盖: (使用全局默认)")
				}

				// 显示有效的 IDE（应用继承规则）
				effectiveIDEs, err := config.GetEffectiveIDEs(projectConfig)
				if err == nil {
					fmt.Printf("  有效 IDE: %s\n", strings.Join(effectiveIDEs, ", "))
				}
			}

			// ========================================
			// 已安装资产
			// ========================================
			assetsConfig, err := projectMgr.LoadAssetsConfig()
			if err != nil {
				fmt.Printf("  ⚠️  加载资产配置失败: %v\n", err)
			} else {
				fmt.Println()
				fmt.Println("📚 已安装资产")
				fmt.Println("-" + strings.Repeat("-", 48))

				if len(assetsConfig.Skills) > 0 {
					fmt.Println("  Skills:")
					for _, skill := range assetsConfig.Skills {
						fmt.Printf("    • %s (from %s, %s)\n", skill.Name, skill.Vault, skill.InstalledAt)
					}
				} else {
					fmt.Println("  Skills: (无)")
				}

				if len(assetsConfig.Rules) > 0 {
					fmt.Println("  Rules:")
					for _, rule := range assetsConfig.Rules {
						fmt.Printf("    • %s (from %s, %s)\n", rule.Name, rule.Vault, rule.InstalledAt)
					}
				} else {
					fmt.Println("  Rules: (无)")
				}

				if len(assetsConfig.MCPs) > 0 {
					fmt.Println("  MCPs:")
					for _, mcp := range assetsConfig.MCPs {
						fmt.Printf("    • %s (from %s, %s)\n", mcp.Name, mcp.Vault, mcp.InstalledAt)
					}
				} else {
					fmt.Println("  MCPs: (无)")
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("=" + strings.Repeat("=", 48))

	return nil
}

// ========================================
// config global
// ========================================

var configGlobalCmd = &cobra.Command{
	Use:   "global",
	Short: "配置全局 IDE",
	Long: `为本机所有支持的 IDE 配置 Dec Skill。

默认配置所有支持的 IDE (cursor, codebuddy, windsurf, trae, claude, claude-internal, codex, codex-internal)。
可以通过 --ide 标志指定要配置的 IDE 子集。

配置会为每个 IDE 安装 Dec 的 Agent Skill，
这样 AI 助手可以在任何项目中协助使用 Dec 的功能。

示例:
  dec config global                              # 配置所有 IDE
  dec config global --ide cursor                 # 只配置 Cursor
  dec config global --ide cursor --ide codebuddy # 配置多个 IDE`,
	RunE: runConfigGlobal,
}

func runConfigGlobal(cmd *cobra.Command, args []string) error {
	// 确保仓库已连接
	connected, err := repo.IsConnected()
	if err != nil {
		return fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return fmt.Errorf("仓库未连接\n\n运行 dec repo <url> 先连接你的仓库")
	}

	// 确定要配置的 IDE 列表
	var targetIDEs []string
	if len(configIDEs) > 0 {
		// 用户指定了具体 IDE
		targetIDEs = configIDEs
	} else {
		// 使用所有支持的 IDE
		knownIDEs := []string{"cursor", "codebuddy", "windsurf", "trae", "claude", "claude-internal", "codex", "codex-internal"}
		targetIDEs = knownIDEs
	}

	// 验证 IDE 名称有效性
	for _, ideName := range targetIDEs {
		if err := validateIDEName(ideName); err != nil {
			return err
		}
	}

	fmt.Printf("🔧 配置 IDE: %s\n\n", strings.Join(targetIDEs, ", "))

	// 为每个 IDE 安装 Dec Skill
	for _, ideName := range targetIDEs {
		fmt.Printf("  配置 %s...\n", ideName)

		// 在每个 IDE 的用户级 skills 目录安装 Dec Skill
		if err := installDecSkillForIDE(ideName); err != nil {
			fmt.Printf("    ⚠️  %s\n", err.Error())
			continue
		}

		// TODO: 安装 Dec MCP 到每个 IDE
		// 这里可能需要更多的 IDE 特定配置逻辑
	}

	// 保存配置到全局 IDE 列表
	localConfig := &types.LocalConfig{
		IDEs: targetIDEs,
	}
	if err := config.SaveLocalConfig(localConfig); err != nil {
		return fmt.Errorf("保存 IDE 配置失败: %w", err)
	}

	fmt.Println("\n✅ 全局 IDE 配置完成")
	fmt.Println("\n后续步骤:")
	fmt.Println("  dec vault init <vault-name>   # 创建 Vault 空间")
	fmt.Println("  dec vault list                # 查看已有 Vault 和资产")
	fmt.Println("  dec vault search <keyword>    # 搜索 Vault 中的资产")
	fmt.Println("\n在项目中使用:")
	fmt.Println("  dec vault pull <type> <name>  # 拉取资产到当前项目")
	fmt.Println("  dec vault import <type> <path>  # 导入资产到 Vault")

	return nil
}

// installDecSkillForIDE 为指定 IDE 安装 Dec Skill
func installDecSkillForIDE(ideName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户主目录失败: %w", err)
	}

	// 构建 IDE 用户级 skills 目录
	// 约定：~/.{ide-name}/skills/
	skillsDir := filepath.Join(homeDir, "."+ideName, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return fmt.Errorf("创建 %s skills 目录失败: %w", ideName, err)
	}

	// 创建 Dec Skill 目录，已存在则覆盖
	decSkillDir := filepath.Join(skillsDir, "dec")
	if err := os.MkdirAll(decSkillDir, 0755); err != nil {
		return fmt.Errorf("创建 Dec Skill 目录失败: %w", err)
	}

	// 写入 SKILL.md
	skillMD := filepath.Join(decSkillDir, "SKILL.md")
	skillContent := assets.DecSkillContent

	if err := os.WriteFile(skillMD, []byte(skillContent), 0644); err != nil {
		return fmt.Errorf("写入 SKILL.md 失败: %w", err)
	}

	return nil
}

// validateIDEName 验证 IDE 名称有效性
func validateIDEName(ideName string) error {
	if ide.IsValid(ideName) {
		return nil
	}
	validIDEs := ide.List()
	sort.Strings(validIDEs)
	return fmt.Errorf("不支持的 IDE: %s (支持: %s)", ideName, strings.Join(validIDEs, ", "))
}

// ========================================
// 注册命令
// ========================================

func init() {
	// config global 标志
	configGlobalCmd.Flags().StringSliceVar(&configIDEs, "ide", nil, "指定要配置的 IDE（可多次指定，默认配置所有支持的 IDE）")

	// 注册子命令
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configGlobalCmd)

	RootCmd.AddCommand(configCmd)
}
