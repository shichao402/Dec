package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/assets"
	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/editor"
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
  dec config repo <url>              # 连接个人仓库
  dec config show                    # 显示当前配置
  dec config global                  # 配置全局 IDE（所有支持的 IDE）
  dec config global --ide cursor     # 只配置 Cursor
  dec config init                    # 初始化项目配置`,
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
  - 全局配置（RepoURL、默认 IDE）
  - 项目配置（可用资产、已启用资产）

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
	if connected && strings.TrimSpace(globalConfig.RepoURL) != "" {
		if err := repo.EnsureConnectedRepoMatches(globalConfig.RepoURL); err != nil {
			return fmt.Errorf("校准仓库连接失败: %w", err)
		}
	}

	if globalConfig.RepoURL != "" {
		fmt.Printf("  Repo URL: %s\n", globalConfig.RepoURL)
	} else {
		fmt.Println("  Repo URL: (未配置)")
	}

	if connected {
		bareRemoteURL, err := repo.GetBareRemoteURL()
		if err == nil {
			fmt.Printf("  当前远端: %s\n", bareRemoteURL)
			if globalConfig.RepoURL != "" {
				if repo.RepoURLsEquivalent(bareRemoteURL, globalConfig.RepoURL) {
					fmt.Println("  连接校验: ✅ 与全局配置一致")
				} else {
					fmt.Println("  连接校验: ⚠️ 与全局 repo_url 不一致")
				}
			}
		}
	}

	globalConfigPath, err := config.GetGlobalConfigPath()
	if err == nil {
		fmt.Printf("  配置文件: %s\n", globalConfigPath)
	}

	if len(globalConfig.IDEs) > 0 {
		fmt.Printf("  默认 IDE: %s\n", strings.Join(globalConfig.IDEs, ", "))
	} else {
		fmt.Println("  默认 IDE: (使用默认: cursor)")
	}

	if strings.TrimSpace(globalConfig.Editor) != "" {
		fmt.Printf("  默认编辑器: %s\n", globalConfig.Editor)
	} else if defaultEditor := editor.DefaultCommand(); defaultEditor != "" {
		fmt.Printf("  默认编辑器: (使用默认: %s)\n", defaultEditor)
	} else {
		fmt.Println("  默认编辑器: (未配置，且未检测到可用编辑器)")
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

				availableCount := 0
				if projectConfig.Available != nil {
					availableCount = projectConfig.Available.Count()
				}
				enabledCount := projectConfig.Enabled.Count()
				fmt.Printf("  可用资产: %d 个\n", availableCount)
				fmt.Printf("  已启用: %d 个\n", enabledCount)

				if len(projectConfig.IDEs) > 0 {
					fmt.Printf("  IDE 覆盖: %s\n", strings.Join(projectConfig.IDEs, ", "))
				} else {
					fmt.Println("  IDE 覆盖: (使用全局默认)")
				}

				if strings.TrimSpace(projectConfig.Editor) != "" {
					fmt.Printf("  编辑器覆盖: %s\n", projectConfig.Editor)
				} else {
					fmt.Println("  编辑器覆盖: (使用全局默认)")
				}

				// 显示有效的 IDE（应用继承规则）
				effectiveIDEs, err := config.GetEffectiveIDEs(projectConfig)
				if err == nil {
					fmt.Printf("  有效 IDE: %s\n", strings.Join(effectiveIDEs, ", "))
				}

				effectiveEditor, err := config.GetEffectiveEditor(projectConfig)
				if err == nil {
					if strings.TrimSpace(effectiveEditor) != "" {
						fmt.Printf("  有效编辑器: %s\n", effectiveEditor)
					} else {
						fmt.Println("  有效编辑器: (未配置，且未检测到可用编辑器)")
					}
				}
			}

			// ========================================
			// 已启用资产
			// ========================================
			fmt.Println()
			fmt.Println("📚 已启用资产")
			fmt.Println("-" + strings.Repeat("-", 48))

			if projectConfig.Enabled.IsEmpty() {
				fmt.Println("  (无)")
			} else {
				enabled := projectConfig.Enabled
				if len(enabled.Skills) > 0 {
					fmt.Println("  Skills:")
					for _, s := range enabled.Skills {
						fmt.Printf("    - %s  (vault: %s)\n", s.Name, s.Vault)
					}
				}
				if len(enabled.Rules) > 0 {
					fmt.Println("  Rules:")
					for _, r := range enabled.Rules {
						fmt.Printf("    - %s  (vault: %s)\n", r.Name, r.Vault)
					}
				}
				if len(enabled.MCPs) > 0 {
					fmt.Println("  MCPs:")
					for _, m := range enabled.MCPs {
						fmt.Printf("    - %s  (vault: %s)\n", m.Name, m.Vault)
					}
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("=" + strings.Repeat("=", 48))

	return nil
}

// ========================================
// config init
// ========================================

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化项目配置",
	Long: `初始化当前项目的 Dec 配置。

从远程仓库获取所有可用资产，生成项目配置和变量模板文件，
并打开编辑器让你选择需要的资产。

保存并关闭编辑器后，配置即生效。
之后运行 dec pull 即可拉取选中的资产。

示例:
  dec config init`,
	RunE: runConfigInit,
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	// 确保仓库已连接
	connected, err := repo.IsConnected()
	if err != nil {
		return fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return fmt.Errorf("仓库未连接\n\n运行 dec config repo <url> 先连接你的仓库")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	mgr := config.NewProjectConfigManager(cwd)

	// 如果已有配置，保留已有的顶层自定义字段
	var existingConfig *types.ProjectConfig
	if mgr.Exists() {
		loadedConfig, err := mgr.LoadProjectConfig()
		if err == nil {
			existingConfig = loadedConfig
		}
		fmt.Println("⚠️  项目已配置过 (.dec/config.yaml 已存在)")
		fmt.Println("   将更新 available 列表，保留已有的 enabled / editor / ides 配置。")
	}

	// 从 repo 获取所有资产
	var allAssets []repoAssetInfo
	if err := withReadRepoDir(func(repoDir string) error {
		folders, err := readFolderEntries(repoDir)
		if err != nil {
			return fmt.Errorf("读取仓库失败: %w", err)
		}
		for _, f := range folders {
			assets := listFolderAssets(f.path, f.name)
			allAssets = append(allAssets, assets...)
		}
		return nil
	}); err != nil {
		return err
	}

	if len(allAssets) == 0 {
		fmt.Println("仓库中还没有资产。请先通过其他方式添加资产到仓库。")
		return nil
	}

	// 构建 available 列表
	available := buildAssetList(allAssets)

	// 生成配置文件，保留已有 enabled / editor / ides
	enabled := &types.AssetList{}
	projectEditor := ""
	var projectIDEs []string
	if existingConfig != nil {
		if !existingConfig.Enabled.IsEmpty() {
			enabled = existingConfig.Enabled
		}
		projectEditor = existingConfig.Editor
		projectIDEs = existingConfig.IDEs
	}
	projectConfig := &types.ProjectConfig{
		IDEs:      projectIDEs,
		Editor:    projectEditor,
		Available: available,
		Enabled:   enabled,
	}
	if err := mgr.SaveProjectConfig(projectConfig); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}
	varsCreated, err := mgr.EnsureVarsConfigTemplate()
	if err != nil {
		return fmt.Errorf("写入变量定义模板失败: %w", err)
	}

	configPath := filepath.Join(mgr.GetDecDir(), "config.yaml")
	fmt.Printf("📝 配置已生成: %s\n", configPath)
	if varsCreated {
		fmt.Printf("📝 变量模板已生成: %s\n", mgr.GetVarsPath())
	} else {
		fmt.Printf("📝 变量模板已保留: %s\n", mgr.GetVarsPath())
	}
	fmt.Println("   将 available 中的资产复制到 enabled 即为启用。")
	fmt.Println("   如资产模板包含 {{VAR_NAME}}，在 vars.yaml 中填写对应变量。")
	fmt.Println()

	interactiveEditor, err := config.GetEffectiveEditor(projectConfig)
	if err != nil {
		return fmt.Errorf("获取交互编辑器配置失败: %w", err)
	}

	// 打开编辑器
	if err := editor.Open(configPath, interactiveEditor); err != nil {
		fmt.Printf("⚠️  无法打开编辑器: %v\n", err)
		fmt.Printf("   请手动编辑 %s 后运行 dec pull\n", configPath)
		return nil
	}

	// 解析编辑后的配置
	projectConfig, err = mgr.LoadProjectConfig()
	if err != nil {
		return fmt.Errorf("解析配置失败: %w", err)
	}

	enabledCount := projectConfig.Enabled.Count()
	fmt.Println("\n✅ 项目配置已完成")
	if enabledCount > 0 {
		fmt.Printf("   已启用 %d 个资产\n", enabledCount)
	} else {
		fmt.Println("   未启用任何资产（可稍后编辑 .dec/config.yaml）")
	}
	fmt.Println("\n后续步骤:")
	fmt.Println("  编辑 .dec/vars.yaml          # 如资产使用了 {{VAR_NAME}} 占位符")
	fmt.Println("  dec pull                     # 拉取所有已启用的资产")

	return nil
}

// buildAssetList 从扫描结果构建 AssetList
func buildAssetList(allAssets []repoAssetInfo) *types.AssetList {
	list := &types.AssetList{}
	for _, a := range allAssets {
		ref := types.AssetRef{Name: a.Name, Vault: a.Vault}
		list.Add(a.Type, ref)
	}
	return list
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
同时会创建 ~/.dec/local/vars.yaml 模板，用于机器级占位符变量。

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

	// 保存配置到全局配置文件
	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("加载全局配置失败: %w", err)
	}
	globalConfig.IDEs = targetIDEs
	if err := config.SaveGlobalConfig(globalConfig); err != nil {
		return fmt.Errorf("保存 IDE 配置失败: %w", err)
	}
	varsCreated, err := config.EnsureGlobalVarsTemplate()
	if err != nil {
		return fmt.Errorf("写入本机变量定义模板失败: %w", err)
	}
	globalVarsPath, err := config.GetGlobalVarsPath()
	if err != nil {
		return fmt.Errorf("获取本机变量定义路径失败: %w", err)
	}

	fmt.Println("\n✅ 全局 IDE 配置完成")
	if varsCreated {
		fmt.Printf("📝 本机变量模板已生成: %s\n", globalVarsPath)
	} else {
		fmt.Printf("📝 本机变量模板已保留: %s\n", globalVarsPath)
	}
	fmt.Println("\n后续步骤:")
	fmt.Println("  dec config init              # 初始化项目配置")
	fmt.Println("  dec list                     # 查看已有 Vault 和资产")
	fmt.Println("  dec search <keyword>         # 搜索资产")
	fmt.Printf("  编辑 %s  # 填写机器级占位符变量\n", globalVarsPath)
	fmt.Println("\n在项目中使用:")
	fmt.Println("  dec pull                     # 拉取资产到当前项目")

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
	configCmd.AddCommand(configRepoCmd)
	configCmd.AddCommand(configInitCmd)

	RootCmd.AddCommand(configCmd)
}
