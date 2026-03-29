# Dec 架构设计

本文档描述当前代码库的真实实现：**Dec 是一个命令驱动的个人 AI 资产管理工具**，支持多 Vault 空间、多 IDE 同步。

## 概览

Dec 解决的是"AI 资产如何跨项目、跨设备复用"的问题。

用户先用 `dec repo` 连接一个 Git 仓库，然后通过 `dec vault` 系列命令在仓库中创建多个 Vault 空间，管理 Skills、Rules、MCP 配置。每个项目可以关联多个 Vault，通过 pull/push/remove 命令管理资产的安装和同步。

当前命令体系：

- `dec repo <url>` — 连接 Git 仓库
- `dec config global` — 配置全局 IDE 列表
- `dec vault init <vault-name>` — 创建 Vault 空间
- `dec vault save <type> <path>` — 保存资产
- `dec vault list` — 列出所有 Vault
- `dec vault search <query>` — 搜索资产
- `dec vault pull <type> <name>` — 下载资产到项目
- `dec vault push` — 推送本地修改回 Vault
- `dec vault remove <type> <name>` — 移除资产
- `dec update` — 自更新

## 目录结构

### 全局目录

```text
~/.dec/
├── config.yaml              # 全局配置（IDE 列表、仓库地址等）
└── repo/                    # 连接的 Git 仓库（克隆到本地）
    ├── my-tools/            # Vault 空间 1
    │   ├── skills/
    │   ├── rules/
    │   └── mcp/
    └── common/              # Vault 空间 2
        ├── skills/
        ├── rules/
        └── mcp/
```

如果设置了 `DEC_HOME`，则以上目录会迁移到 `DEC_HOME` 下。

### 项目目录

```text
.dec/
├── config.yaml              # 项目配置（关联的 Vault 列表、IDE 覆盖）
└── assets.yaml              # 已安装资产追踪
```

`.dec/` 目录由 `dec vault init` 自动创建，并自动添加到 `.gitignore`。

### IDE 托管输出

| IDE | Skills | Rules | MCP |
|---|---|---|---|
| Cursor | `.cursor/skills/` | `.cursor/rules/` | `.cursor/mcp.json` |
| CodeBuddy | `.codebuddy/skills/` | `.codebuddy/rules/` | `.mcp.json` |
| Windsurf | `.windsurf/skills/` | `.windsurf/rules/` | `.windsurf/mcp.json` |
| Trae | `.trae/skills/` | `.trae/rules/` | `.trae/mcp.json` |

Dec 托管的产物统一使用 `dec-` 前缀，以便与用户手工维护的内容区分。

## 核心流程

### `dec repo <url>`

1. 克隆用户指定的 Git 仓库到 `~/.dec/repo/`
2. 将仓库地址写入全局 `~/.dec/config.yaml`

### `dec config global`

1. 配置全局 IDE 列表（cursor, codebuddy, windsurf, trae）
2. 全局配置作为默认值，项目级配置可覆盖

### `dec vault init <vault-name>`

1. 在 `~/.dec/repo/{vault-name}/` 下创建 `skills/`、`rules/`、`mcp/` 子目录（含 `.gitkeep`）
2. 提交并推送到远程仓库
3. 在当前项目创建 `.dec/config.yaml`，关联该 Vault
4. 如果项目已有配置，只添加 Vault 关联

### `dec vault save <type> <path>`

1. 确定目标 Vault（单 Vault 自动选择，多 Vault 需 `--vault` 指定）
2. 校验资产格式（skill=目录含 SKILL.md, rule=.mdc 文件, mcp=JSON 含 command）
3. 复制资产到 `repo/{vault}/{type}/{name}`
4. 提交并推送
5. 更新项目 `.dec/assets.yaml` 追踪

### `dec vault list`

1. 从远程拉取最新
2. 扫描 `~/.dec/repo/` 下所有非隐藏目录
3. 统计每个 Vault 的资产数量（skills/rules/mcps）

### `dec vault search <query>`

1. 从远程拉取最新
2. 扫描所有 Vault 目录
3. 大小写不敏感匹配资产名称

### `dec vault pull <type> <name>`

1. 从远程拉取最新
2. 优先在项目关联的 Vault 中搜索，未找到则扫描所有 Vault
3. 将资产安装到所有配置的 IDE 目录（使用 `dec-` 前缀）
4. MCP 类型采用合并策略，不覆盖用户已有配置
5. 更新 `.dec/assets.yaml`

### `dec vault push`

1. 读取 `.dec/assets.yaml` 中所有已追踪资产
2. 从第一个配置的 IDE 目录中读取本地版本
3. 复制回 `repo/{vault}/{type}/` 对应位置
4. 提交并推送

### `dec vault remove <type> <name>`

1. 从所有配置的 IDE 目录中删除资产
2. 从 `.dec/assets.yaml` 移除追踪
3. 可选 `--remote`：同时删除远程 Vault 中的资产并推送

### `dec update`

1. 检查远程版本信息（GitHub Release）
2. 与当前版本号比较
3. 如果有新版本，下载对应平台的二进制
4. 备份现有二进制，并替换为新版本
5. 失败时恢复原始二进制
6. 更新检查状态文件（每天最多检查一次）

## 模块划分

### `cmd/`

命令行入口层，负责参数解析、用户输出和核心业务逻辑。

- `root.go`：根命令与版本信息
- `repo.go`：仓库连接命令
- `config.go`：全局/项目配置命令
- `vault.go`：Vault 全部子命令（init/save/list/search/pull/push/remove）及辅助函数
- `update.go`：版本检查和更新

### `pkg/config/`

配置读写。

- `global.go`：全局 `config.yaml`，IDE 列表优先级解析（项目 > 全局 > 默认）
- `project.go`：项目级 `ProjectConfigManager`，管理 `.dec/config.yaml` 和 `.dec/assets.yaml`

### `pkg/repo/`

Git 仓库操作。

- `repo.go`：`GitOps` 实现，包含 `Connect()`、`Pull()`、`CommitAndPush()`、`IsConnected()` 等

### `pkg/ide/`

IDE 抽象层，封装不同 IDE 的目录结构差异。

- `ide.go`：IDE 接口定义（`SkillsDir()`, `RulesDir()`, `MCPConfigPath()`, `LoadMCPConfig()`, `WriteMCPConfig()` 等）
- `registry.go`：注册 cursor, codebuddy, windsurf, trae 四种 IDE 实现

### `pkg/types/`

声明项目配置、MCP 配置等结构体。

- `pack.go`：`GlobalConfig`, `ProjectConfig`, `AssetsConfig`, `AssetEntry`, `MCPConfig`, `MCPServer` 等类型定义

### `pkg/version/`

版本号管理和比较。

- 从 `version.json` 加载版本信息
- Semantic versioning 版本号比较
- 编译时版本注入

### `pkg/update/`

版本检查和自更新功能。

- 检查 GitHub Release 上的最新版本
- 后台版本检查（24小时一次）
- 下载新版本二进制，执行自更新（备份、替换、恢复机制）

## 关键设计点

### 命令驱动而非声明式同步

新架构中没有 `dec sync` 命令。所有资产操作通过显式的 `pull`/`push`/`remove` 命令完成，用户对每次操作有完全控制权。

### 多 Vault 支持

一个仓库中可以创建多个 Vault 空间，一个项目可以关联多个 Vault。当项目只关联一个 Vault 时，save 命令自动选择目标；多个时需要 `--vault` 指定。

### 托管范围有限

Dec 只会操作自己托管的 `dec-*` 内容，不会删除用户手工维护的非托管资产。

### MCP 合并保留用户配置

pull MCP 时采用合并策略：
- 将 vault 中的 MCP server 片段以 `dec-{name}` 键名写入 IDE 的 MCP 配置
- 用户自己维护的非 `dec-*` 条目保持不变

### 资产路径发现

通过直接扫描 `repo/{vault}/{skills|rules|mcp}/` 目录结构来发现资产，不依赖索引文件。

### Git 原生操作

Dec 依赖标准 `git` 命令进行仓库操作（通过 `pkg/repo/repo.go` 中的 `GitOps`），用户需要自行配置 Git 认证。

## 当前边界

以下能力**不在当前实现中**：

- `dec init` / `dec sync`（已删除，由 vault 命令体系替代）
- `pkg/vault/`（已删除，逻辑合并到 `cmd/vault.go`）
- `pkg/service/`（已删除，不再需要同步编排层）
- `dec serve` / `dec publish-notify`
- `technology.yaml` / `packs.yaml` 配置体系
- 依赖 GitHub Actions 的自动发布与自动注册流程

## 已知问题与限制

### CodeBuddy MCP 配置路径

CodeBuddy 的 MCP 配置文件位置为项目根目录的 `.mcp.json`，与其他 IDE 的 `.{ide}/mcp.json` 结构不同。已通过 IDE 抽象层正确处理。

### 文件权限不保留原始值

`copyFile()` 和 `copyDir()` 使用固定的权限位（0755/0644），不保留源文件权限。对于需要特殊权限的脚本可能有影响。

### 测试覆盖

Phase 2 重写后 `cmd/vault.go` 的测试尚未补充（需要在 Phase 4 完成）。当前只有 `pkg/ide/` 和 `pkg/version/` 有单元测试。
