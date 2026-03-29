package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
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
  dec config global                    # 配置全局 IDE（所有支持的 IDE）
  dec config global --ide cursor       # 只配置 Cursor
  dec config global --ide cursor --ide codebuddy  # 配置多个 IDE`,
}

// ========================================
// config global
// ========================================

var configGlobalCmd = &cobra.Command{
	Use:   "global",
	Short: "配置全局 IDE",
	Long: `为本机所有支持的 IDE 配置 Dec Skill 和 MCP。

默认配置所有支持的 IDE (cursor, codebuddy, windsurf, trae)。
可以通过 --ide 标志指定要配置的 IDE 子集。

配置会为每个 IDE 安装 Dec 的 Skill 和 MCP，
这样在任何项目中都可以使用 Dec 的功能。

示例:
  dec config global                    # 配置所有 IDE
  dec config global --ide cursor       # 只配置 Cursor
  dec config global --ide cursor --ide windsurf  # 配置多个 IDE`,
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
		knownIDEs := []string{"cursor", "codebuddy", "windsurf", "trae"}
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
	fmt.Println("  或在项目中 dec vault init <vault-name> 关联 Vault")

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

	// 创建 Dec Skill 目录（dec-agent）
	decSkillDir := filepath.Join(skillsDir, "dec-agent")

	// 如果已存在则跳过
	if _, err := os.Stat(decSkillDir); err == nil {
		return nil // 已安装
	}

	// 创建 Dec Skill 基础结构
	if err := os.MkdirAll(decSkillDir, 0755); err != nil {
		return fmt.Errorf("创建 Dec Skill 目录失败: %w", err)
	}

	// 创建 SKILL.md
	skillMD := filepath.Join(decSkillDir, "SKILL.md")
	skillContent := `---
name: dec-agent
description: Dec 个人知识仓库代理
---

# Dec 代理

Dec 个人知识仓库的代理 Skill，使 IDE 能够感知和使用 Dec 管理的资产。

## 功能

- 自动发现已安装的 Vault 资产
- 支持 Vault 搜索和资产管理命令
- 集成项目级 IDE 配置

## 使用

在项目中运行:
  dec vault list        # 列出可用资产
  dec vault search      # 搜索资产
  dec vault pull        # 下载资产到项目
`

	if err := os.WriteFile(skillMD, []byte(skillContent), 0644); err != nil {
		return fmt.Errorf("写入 SKILL.md 失败: %w", err)
	}

	return nil
}

// validateIDEName 验证 IDE 名称有效性
func validateIDEName(ideName string) error {
	validIDEs := []string{"cursor", "codebuddy", "windsurf", "trae"}
	for _, valid := range validIDEs {
		if ideName == valid {
			return nil
		}
	}
	return fmt.Errorf("不支持的 IDE: %s (支持: %s)", ideName, strings.Join(validIDEs, ", "))
}

// ========================================
// 注册命令
// ========================================

func init() {
	// config global 标志
	configGlobalCmd.Flags().StringSliceVar(&configIDEs, "ide", nil, "指定要配置的 IDE（可多次指定，默认配置所有支持的 IDE）")

	// 注册子命令
	configCmd.AddCommand(configGlobalCmd)

	RootCmd.AddCommand(configCmd)
}
