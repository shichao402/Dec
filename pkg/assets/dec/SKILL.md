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

1. **用户在新项目中需要初始化 Dec**
   - 运行 `dec config init` 生成项目配置，从 available 复制需要的资产到 enabled
   - 运行 `dec pull` 拉取所有已启用的资产

2. **用户提到需要之前做过的工具/配置**
   - 运行 `dec search "<query>"` 搜索已有资产
   - 或用 `dec list` 列出所有资产

3. **用户修改了已拉取的资产**
   - 修改 `.dec/cache/` 中的缓存文件
   - 运行 `dec push` 推送修改到远程仓库
   - **禁止**手动去修改其他 IDE 目录中的同名文件，统一通过 dec 命令同步

4. **用户需要删除远程资产**
   - 运行 `dec push --remove <type> <name>` 删除（需交互确认）

## 快速参考

### 资产管理

| 操作 | 命令 | 说明 |
|------|------|------|
| 列出所有资产 | `dec list` | 显示仓库中的所有 Skills、Rules、MCP |
| 搜索资产 | `dec search "<query>"` | 按资产名称搜索 |
| 拉取资产 | `dec pull` | 拉取 config.yaml 中所有已启用的资产 |
| 拉取指定版本 | `dec pull --version <ref>` | 拉取指定 commit/tag 版本 |
| 推送修改 | `dec push` | 将缓存中的修改推回远程仓库 |
| 删除远程资产 | `dec push --remove <type> <name>` | 需交互确认 |

### 配置和初始化

| 操作 | 命令 | 说明 |
|------|------|------|
| 连接仓库 | `dec config repo <url>` | 连接个人仓库（GitHub URL） |
| 配置全局 IDE | `dec config global` | 为本机 IDE 配置 Dec Skill |
| 初始化项目 | `dec config init` | 生成项目配置，选择启用的资产 |
| 查看配置 | `dec config show` | 显示全局和项目配置 |

## 配置文件格式

项目配置位于 `.dec/config.yaml`，采用 available/enabled 双区结构：

```yaml
available:          # 仓库中所有可用资产（dec config init 自动生成）
  rules:
    - name: my-rule
      vault: my-vault
  mcps:
    - name: my-mcp
      vault: my-vault

enabled:            # 已启用资产（从 available 复制到这里即为启用）
  rules:
    - name: my-rule
      vault: my-vault
```

- `dec config init` 自动填充 available，enabled 留空
- 用户从 available 复制想要的资产到 enabled
- `dec pull` 只拉取 enabled 中的资产
- pull 时自动校验 enabled vs available，清理不再启用的旧资产

## 资产格式

### Skill（目录）

Skill 必须是一个包含 `SKILL.md` 的目录。

### Rule（文件）

Rule 是单个 `.mdc` 文件。

### MCP（JSON 片段）

MCP 必须是单个 server 片段 JSON，其中 `command` 必填，`args`、`env` 按需提供。

## 故障排查

### "仓库未连接"

运行 `dec config repo <url>` 连接你的仓库。

### "找不到资产"

1. 确认资产名称：`dec search "<partial-name>"`
2. 列出所有资产：`dec list`

### "拉取失败"

1. 检查仓库连接：`dec config show`
2. 验证资产存在：`dec search <name>`
3. 检查 enabled 配置是否正确

### "配置校验警告"

pull 前会校验 enabled 中的资产是否在 available 中存在。如果看到警告：
- 检查资产名是否拼写正确
- 运行 `dec config init` 更新 available 列表

## 修改资产的正确流程

1. **修改 `.dec/cache/` 中的缓存文件**
2. **推送修改**：`dec push`
3. **在其他项目中拉取**：`dec pull`

**禁止**：直接操作 `~/.dec/repo.git` 或其他底层目录。所有操作必须通过 `dec` CLI 完成。

## 更多信息

运行 `dec --help` 查看完整帮助。
