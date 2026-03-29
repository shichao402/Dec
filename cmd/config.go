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
description: >
  Dec 个人 AI 知识仓库代理。支持跨项目复用 Skills、Rules、MCP 配置。
  推荐用户保存新创建的资产、搜索已有资产、或在新项目中拉取所需资产。
---

# Dec 代理

Dec 是一个个人 AI 知识仓库，帮助你积累和复用 AI 资产（Skills、Rules、MCP 配置）。

## 何时使用

### 主动建议用户的场景

1. **用户创建或修改了 Skill/Rule**
   - 建议运行 ` + "`" + `dec vault save skill <path>` + "`" + ` 保存以供后续项目复用
   - 可添加标签便于搜索：` + "`" + `--tag <tag1> --tag <tag2>` + "`" + `

2. **用户提到需要之前做过的工具/配置**
   - 运行 ` + "`" + `dec vault search "<query>"` + "`" + ` 搜索已有资产
   - 或用 ` + "`" + `dec vault list` + "`" + ` 列出所有资产

3. **用户在新项目中需要标准工具**
   - 运行 ` + "`" + `dec vault pull <type> <name>` + "`" + ` 下载资产到当前项目
   - 类型：skill、rule、mcp

4. **用户修改了已拉取的资产**
   - 建议运行 ` + "`" + `dec vault save` + "`" + ` 回写到 Vault
   - 这样其他项目能获取最新版本

## 快速参考

### Vault 资产管理

| 操作 | 命令 | 说明 |
|------|------|------|
| 列出所有资产 | ` + "`" + `dec vault list` + "`" + ` | 显示 Vault 中的所有 Skills、Rules、MCP |
| 按类型列出 | ` + "`" + `dec vault list --type skill` + "`" + ` | 只列出 skill、rule 或 mcp |
| 搜索资产 | ` + "`" + `dec vault search "<query>"` + "`" + ` | 按名称、描述或标签搜索 |
| 保存 Skill | ` + "`" + `dec vault save skill <dir-path>` + "`" + ` | 目录需包含 SKILL.md |
| 保存 Rule | ` + "`" + `dec vault save rule <file.mdc>` + "`" + ` | Rule 文件格式 |
| 保存 MCP | ` + "`" + `dec vault save mcp <server.json>` + "`" + ` | MCP server 片段 |
| 添加标签 | ` + "`" + `dec vault save skill <path> --tag <tag>` + "`" + ` | 支持多个 --tag |
| 拉取到项目 | ` + "`" + `dec vault pull skill <name>` + "`" + ` | 自动部署到当前 IDE |
| 推送更新 | ` + "`" + `dec vault push` + "`" + ` | 本地修改推送到远程 |

### 连接和初始化

| 操作 | 命令 | 说明 |
|------|------|------|
| 关联 Vault | ` + "`" + `dec repo <url>` + "`" + ` | 连接个人 Vault 仓库（GitHub URL） |
| 配置全局 IDE | ` + "`" + `dec config global` + "`" + ` | 为本机 IDE 配置 Dec Skill |
| 查询帮助 | ` + "`" + `dec vault --help` + "`" + ` | 查看所有 Vault 命令 |

## 资产格式

### Skill（目录）

Skill 必须是一个包含 ` + "`" + `SKILL.md` + "`" + ` 的目录：

` + "`" + `` + "`" + `` + "`" + `
my-skill/
├── SKILL.md        # 必须
├── main.py
├── utils.js
└── ...
` + "`" + `` + "`" + `` + "`" + `

在 ` + "`" + `SKILL.md` + "`" + ` 的 front matter 中定义：

` + "`" + `` + "`" + `` + "`" + `yaml
---
name: my-skill
description: 做什么的
---
` + "`" + `` + "`" + `` + "`" + `

### Rule（文件）

Rule 是单个 ` + "`" + `.mdc` + "`" + ` 文件：

` + "`" + `` + "`" + `` + "`" + `bash
dec vault save rule .cursor/rules/my-rule.mdc
` + "`" + `` + "`" + `` + "`" + `

### MCP（JSON 片段）

MCP 必须是单个 server 片段，而非完整的 ` + "`" + `mcp.json` + "`" + `：

` + "`" + `` + "`" + `` + "`" + `json
{
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres"],
  "env": {
    "DATABASE_URL": "\${DATABASE_URL}"
  }
}
` + "`" + `` + "`" + `` + "`" + `

## 故障排查

### "Vault 未连接"

运行 ` + "`" + `dec repo <url>` + "`" + ` 关联你的 Vault 仓库。

### "找不到资产"

1. 确认资产名称：` + "`" + `dec vault search "<partial-name>"` + "`" + `
2. 列出所有资产：` + "`" + `dec vault list`
3. 按类型筛选：` + "`" + `dec vault list --type skill` + "`" + `

### "拉取失败"

1. 检查 Vault 连接：` + "`" + `dec repo --help` + "`" + `
2. 验证资产存在：` + "`" + `dec vault search <name>` + "`" + `
3. 查看详细错误：运行命令时会输出诊断信息

### "保存失败"

常见原因：
- Skill 目录缺少 ` + "`" + `SKILL.md` + "`" + `
- Rule 文件不是 ` + "`" + `.mdc` + "`" + ` 格式
- MCP JSON 无效或缺少必要字段

## 最佳实践

1. **定期保存**：完成一个可复用工具后立即保存，不要等到忘记
2. **使用标签**：为资产添加描述性标签（` + "`" + `--tag testing`、` + "`" + `--tag api` + "`" + `），便于搜索
3. **资产版本化**：保存时 Vault 自动 Git 提交，方便追踪变更历史
4. **团队共享**：将 Vault 仓库 URL 分享给团队成员，他们可以获取同样的资产

## 相关文档

- 完整指南：项目中运行 ` + "`" + `dec --help` + "`" + ` 或 ` + "`" + `dec vault --help` + "`" + `
- Vault 仓库地址：通过 ` + "`" + `dec repo` + "`" + ` 指定
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
