package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/pkg/app"
	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/editor"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/spf13/cobra"
)

var (
	configIDEs         []string
	configProjectIDEs  []string
	configProjectClear bool
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
  dec config project                 # 查看项目级 IDE 覆盖
  dec config project --ide cursor    # 设置项目级 IDE 覆盖
  dec config project --clear         # 清除项目级 IDE 覆盖
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
		globalSelection, err := config.ResolveEffectiveIDEs(nil)
		if err == nil {
			fmt.Printf("  默认 IDE: %s\n", strings.Join(globalSelection.IDEs, ", "))
			for _, warning := range globalSelection.Warnings {
				fmt.Printf("  IDE 警告: %s\n", warning)
			}
		} else {
			fmt.Printf("  默认 IDE: 配置错误: %v\n", err)
		}
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
				var effectiveIDEs []string
				var ideWarnings []string
				if len(projectConfig.IDEs) > 0 {
					selection, selectionErr := config.ResolveEffectiveIDEs(projectConfig)
					if selectionErr == nil {
						effectiveIDEs = selection.IDEs
						ideWarnings = selection.Warnings
					}
					err = selectionErr
				} else {
					effectiveIDEs, err = config.GetEffectiveIDEs(projectConfig)
				}
				if err == nil {
					fmt.Printf("  有效 IDE: %s\n", strings.Join(effectiveIDEs, ", "))
					for _, warning := range ideWarnings {
						fmt.Printf("  IDE 警告: %s\n", warning)
					}
				} else {
					fmt.Printf("  有效 IDE: 配置错误: %v\n", err)
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
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	prepared, err := app.PrepareProjectConfigInit(cwd, nil)
	if err != nil {
		return err
	}

	if prepared.ExistingConfig {
		fmt.Println("⚠️  项目已配置过 (.dec/config.yaml 已存在)")
		fmt.Println("   将更新 available 列表，保留已有的 enabled / editor / ides 配置。")
	}

	if prepared.AssetCount == 0 {
		fmt.Println("仓库中还没有资产。请先通过其他方式添加资产到仓库。")
		return nil
	}

	mgr := config.NewProjectConfigManager(cwd)
	fmt.Printf("📝 配置已生成: %s\n", prepared.ConfigPath)
	if prepared.VarsCreated {
		fmt.Printf("📝 变量模板已生成: %s\n", prepared.VarsPath)
	} else {
		fmt.Printf("📝 变量模板已保留: %s\n", prepared.VarsPath)
	}
	fmt.Println("   将 available 中的资产复制到 enabled 即为启用。")
	fmt.Println("   如资产模板包含 {{VAR_NAME}}，在 vars.yaml 中填写对应变量。")
	fmt.Println()

	interactiveEditor, err := config.GetEffectiveEditor(prepared.ProjectConfig)
	if err != nil {
		return fmt.Errorf("获取交互编辑器配置失败: %w", err)
	}

	// 打开编辑器
	if err := editor.Open(prepared.ConfigPath, interactiveEditor); err != nil {
		fmt.Printf("⚠️  无法打开编辑器: %v\n", err)
		fmt.Printf("   请手动编辑 %s 后运行 dec pull\n", prepared.ConfigPath)
		return nil
	}

	// 解析编辑后的配置
	projectConfig, err := mgr.LoadProjectConfig()
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

// ========================================
// config global
// ========================================

var configGlobalCmd = &cobra.Command{
	Use:   "global",
	Short: "配置全局 IDE",
	Long: `为本机所有支持的 IDE 安装 Dec 内置资产。

默认配置所有当前支持的 IDE。
可以通过 --ide 标志指定要配置的 IDE 子集。

配置会为每个 IDE 安装 Dec 跟随分发的内置 Skills，
当前包括 dec 和 dec-extract-asset。
这样 AI 助手可以在任何项目中协助使用 Dec，并把当前项目里的可复用能力沉淀回 Dec。
同时会创建 ~/.dec/local/vars.yaml 模板，用于机器级占位符变量。

示例:
  dec config global                              # 配置所有 IDE
  dec config global --ide cursor                 # 只配置 Cursor
  dec config global --ide cursor --ide codebuddy # 配置多个 IDE`,
	RunE: runConfigGlobal,
}

func runConfigGlobal(cmd *cobra.Command, args []string) error {
	result, err := app.SaveGlobalSettings(app.SaveGlobalSettingsInput{IDEs: configIDEs}, nil)
	if err != nil {
		return err
	}

	fmt.Printf("🔧 配置 IDE: %s\n\n", strings.Join(result.IDEs, ", "))
	for _, ideName := range result.IDEs {
		fmt.Printf("  配置 %s...\n", ideName)
	}
	for _, warning := range result.InstallWarnings {
		fmt.Printf("    ⚠️  %s\n", warning)
	}

	fmt.Println("\n✅ 全局 IDE 配置完成")
	if result.VarsCreated {
		fmt.Printf("📝 本机变量模板已生成: %s\n", result.VarsPath)
	} else {
		fmt.Printf("📝 本机变量模板已保留: %s\n", result.VarsPath)
	}
	fmt.Println("\n后续步骤:")
	fmt.Println("  dec config init              # 初始化项目配置")
	fmt.Println("  dec list                     # 查看已有 Vault 和资产")
	fmt.Println("  dec search <keyword>         # 搜索资产")
	fmt.Printf("  编辑 %s  # 填写机器级占位符变量\n", result.VarsPath)
	fmt.Println("\n在项目中使用:")
	fmt.Println("  dec pull                     # 拉取资产到当前项目")

	return nil
}

// ========================================
// config project
// ========================================

var configProjectCmd = &cobra.Command{
	Use:   "project",
	Short: "配置项目级 IDE 覆盖",
	Long: `管理当前项目的 IDE 覆盖配置（.dec/config.yaml 中的 ides 字段）。

无标志时打印当前项目级 IDE 状态（是否覆盖、生效集、警告等）。
使用 --ide 指定项目级覆盖；使用 --clear 清除覆盖回落到全局。
--ide 与 --clear 不能同时使用。

示例:
  dec config project                                # 查看当前状态
  dec config project --ide cursor                   # 覆盖为仅 cursor
  dec config project --ide cursor --ide codebuddy   # 覆盖多个 IDE
  dec config project --clear                        # 清除项目级覆盖`,
	RunE: runConfigProject,
}

func runConfigProject(cmd *cobra.Command, args []string) error {
	if configProjectClear && len(configProjectIDEs) > 0 {
		return fmt.Errorf("--ide 与 --clear 不能同时使用")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	// 无标志：只读模式，打印当前状态。
	if !configProjectClear && len(configProjectIDEs) == 0 {
		state, err := app.LoadProjectSettings(cwd, nil)
		if err != nil {
			return err
		}
		printProjectSettingsState(state)
		return nil
	}

	// --clear 幂等：先加载判断是否已无覆盖。
	if configProjectClear {
		state, err := app.LoadProjectSettings(cwd, nil)
		if err != nil {
			return err
		}
		if !state.OverrideActive {
			fmt.Println("ℹ️  本项目未设置 IDE 覆盖，无需清除")
			return nil
		}
	}

	result, err := app.SaveProjectSettings(app.SaveProjectSettingsInput{
		ProjectRoot:   cwd,
		IDEs:          configProjectIDEs,
		ClearOverride: configProjectClear,
	}, nil)
	if err != nil {
		return err
	}

	if configProjectClear {
		fmt.Println("✅ 已清除项目级 IDE 覆盖，现回落到全局默认")
	} else {
		fmt.Printf("✅ 已保存项目级 IDE 覆盖: %s\n", strings.Join(result.SelectedIDEs, ", "))
	}
	fmt.Printf("📁 配置文件: %s\n", result.ConfigPath)
	if len(result.EffectiveIDEs) > 0 {
		fmt.Printf("🎯 生效 IDE: %s\n", strings.Join(result.EffectiveIDEs, ", "))
	}
	for _, warning := range result.Warnings {
		fmt.Printf("  ⚠️  %s\n", warning)
	}
	return nil
}

func printProjectSettingsState(state *app.ProjectSettingsState) {
	fmt.Println("📦 项目级 IDE 配置")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("  配置文件: %s\n", state.ConfigPath)
	if state.ProjectConfigReady {
		fmt.Println("  配置状态: ✅ 已初始化")
	} else {
		fmt.Println("  配置状态: ⚠️  未初始化 (.dec/config.yaml 不存在)")
	}

	if state.OverrideActive {
		fmt.Printf("  覆盖状态: ✅ 已覆盖 (%s)\n", strings.Join(state.SelectedIDEs, ", "))
	} else {
		fmt.Println("  覆盖状态: (未覆盖，继承全局)")
	}

	if len(state.GlobalIDEs) > 0 {
		fmt.Printf("  全局 IDE: %s\n", strings.Join(state.GlobalIDEs, ", "))
	} else {
		fmt.Println("  全局 IDE: (未配置，使用默认 cursor)")
	}

	if len(state.EffectiveIDEs) > 0 {
		fmt.Printf("  生效 IDE: %s\n", strings.Join(state.EffectiveIDEs, ", "))
	}
	for _, warning := range state.IDEWarnings {
		fmt.Printf("  ⚠️  %s\n", warning)
	}
}

func installBuiltinAssetsForIDE(ideName string) error {
	return app.InstallBuiltinAssetsForIDE(ideName)
}

func validateIDEName(ideName string) error {
	return app.ValidateIDEName(ideName)
}

// ========================================
// 注册命令
// ========================================

func init() {
	// config global 标志
	configGlobalCmd.Flags().StringSliceVar(&configIDEs, "ide", nil, "指定要配置的 IDE（可多次指定，默认配置所有支持的 IDE）")

	// config project 标志
	configProjectCmd.Flags().StringSliceVar(&configProjectIDEs, "ide", nil, "指定项目级 IDE 覆盖（可多次指定）")
	configProjectCmd.Flags().BoolVar(&configProjectClear, "clear", false, "清除项目级 IDE 覆盖，回落到全局默认")

	// 注册子命令
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configGlobalCmd)
	configCmd.AddCommand(configProjectCmd)
	configCmd.AddCommand(configRepoCmd)
	configCmd.AddCommand(configInitCmd)

	RootCmd.AddCommand(configCmd)
}
