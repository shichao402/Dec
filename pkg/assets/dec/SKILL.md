---
name: dec
description: >
  Dec 个人 AI 知识仓库代理。支持跨项目复用 Skills、Rules、MCP 配置。
  推荐用户保存新创建的资产、搜索已有资产、或在新项目中拉取所需资产。
---

# Dec 代理

Dec 是一个个人 AI 知识仓库，帮助你积累和复用 AI 资产（Skills、Rules、MCP 配置）。

## 何时使用

### 主动建议用户的场景

1. **用户创建了新的 Skill/Rule（首次入库）**
   - 运行 `dec vault import <type> <path>` 将新资产导入到 Vault
   - 多 Vault 时指定目标：`--vault <name>`

2. **用户提到需要之前做过的工具/配置**
   - 运行 `dec vault search "<query>"` 搜索已有资产
   - 或用 `dec vault list` 列出所有资产

3. **用户在新项目中需要标准工具**
   - 运行 `dec vault pull <type> <name>` 下载资产到当前项目
   - 类型：skill、rule、mcp

4. **用户修改了已拉取的资产（更新已有）**
   - 在项目中的托管副本上完成修改（如 `.codebuddy/rules/` 下的文件）
   - 对显式修改过的资产，优先使用 `dec vault import <type> <path>` 回写到 Vault
   - 如需把已追踪模板统一推送到远端，再运行 `dec vault push`
   - 运行 `dec vault pull <type> <name>` 拉取更新，同步到其他 IDE
   - **禁止**手动去修改其他 IDE 目录中的同名文件，统一通过 dec 命令同步

## 快速参考

### Vault 资产管理

| 操作 | 命令 | 说明 |
|------|------|------|
| 列出所有资产 | `dec vault list` | 显示 Vault 中的所有 Skills、Rules、MCP |
| 搜索资产 | `dec vault search "<query>"` | 当前实现按资产名称搜索 |
| 导入新资产 | `dec vault import <type> <path>` | 首次将本地资产入库到 Vault |
| 指定 Vault | `dec vault import <type> <path> --vault <name>` | 多 Vault 时指定目标 |
| 拉取到项目 | `dec vault pull <type> <name>` | 从 Vault 下载资产到当前项目 |
| 批量拉取 | `dec vault pull --all` | 拉取所有 Vault 的所有资产 |
| 批量拉取指定 Vault | `dec vault pull --all --vault <name>` | 拉取指定 Vault 的所有资产 |
| 推送修改 | `dec vault push` | 将已追踪资产的本地修改推回 Vault |

### 连接和初始化

| 操作 | 命令 | 说明 |
|------|------|------|
| 关联 Vault | `dec repo <url>` | 连接个人 Vault 仓库（GitHub URL） |
| 配置全局 IDE | `dec config global` | 为本机 IDE 配置 Dec Skill |
| 查询帮助 | `dec vault --help` | 查看所有 Vault 命令 |

## 资产格式

### Skill（目录）

Skill 必须是一个包含 `SKILL.md` 的目录。

在 `SKILL.md` 的 front matter 中定义 name 和 description。

### Rule（文件）

Rule 是单个 `.mdc` 文件。使用命令导入：

    dec vault import rule .codebuddy/rules/my-rule.mdc

### MCP（JSON 片段）

MCP 必须是单个 server 片段 JSON，其中 `command` 必填，`args`、`env` 按需提供。

## 故障排查

### "Vault 未连接"

运行 `dec repo <url>` 关联你的 Vault 仓库。

### "找不到资产"

1. 确认资产名称：`dec vault search "<partial-name>"`
2. 列出所有资产：`dec vault list`

### "拉取失败"

1. 检查 Vault 连接：`dec config show`，必要时重新执行 `dec repo <url>`
2. 验证资产存在：`dec vault search <name>`
3. 查看详细错误：运行命令时会输出诊断信息

### "保存失败"

常见原因：
- Skill 目录缺少 `SKILL.md`
- Rule 文件不是 `.mdc` 格式
- MCP JSON 无效或缺少必要字段

## 修改资产的正确流程

当需要修改已拉取到项目中的资产时，严格按以下顺序操作：

1. **在项目中的托管副本上完成修改**（如 `.codebuddy/rules/xxx.mdc`），不要手动同步到其他 IDE 目录
2. **显式回写该资产**：`dec vault import <type> <path>`
3. **按需统一推送已追踪模板**：`dec vault push`
4. **拉取更新**：`dec vault pull <type> <name>`，由 dec 自动同步到其他 IDE 目录

**禁止**：直接操作 `~/.dec/repo.git`、项目 `.dec/templates/` 或其他底层缓存目录。所有操作必须通过 `dec` CLI 完成。
如果 `dec` 命令失败，将错误反馈给用户，由用户决定处理方式。

## 最佳实践

1. 及时入库：新建资产后立即用 `dec vault import` 导入；显式修改资产后优先继续用 `dec vault import` 回写
2. 统一同步：需要把已追踪模板批量回推远端时，再使用 `dec vault push`
3. 资产版本化：Vault 自动 Git 提交，方便追踪变更
4. 团队共享：将 Vault 仓库 URL 分享给团队成员

## 更多信息

运行 `dec --help` 或 `dec vault --help` 查看完整帮助。
