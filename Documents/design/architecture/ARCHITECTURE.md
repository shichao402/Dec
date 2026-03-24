# Dec 架构设计

本文档描述当前代码库的真实实现：**Dec 是一个个人 AI 资产 Vault 与多 IDE 同步工具**，而不是传统的规则包管理器。

## 概览

Dec 解决的是“AI 资产如何跨项目、跨设备复用”的问题。

它把 Skills、Rules、MCP 配置保存在个人 Vault（Git 仓库）中，再由项目里的声明式配置决定哪些资产要同步到哪些 IDE。

当前源码主线只有三类命令：

- `dec init`
- `dec sync`
- `dec vault ...`

## 目录结构

### 全局目录

```text
~/.dec/
├── config.yaml              # 全局配置（如 vault_source）
├── config/
│   └── system.json          # 安装脚本下载的系统配置
└── vault/                   # 个人 Vault（Git 仓库）
```

如果设置了 `DEC_HOME`，则以上目录会迁移到 `DEC_HOME` 下。

### 项目目录

```text
.dec/config/
├── ides.yaml                # 当前项目要同步到哪些 IDE
└── vault.yaml               # 当前项目声明依赖哪些 Vault 资产
```

### IDE 托管输出

| IDE | Skills | Rules | MCP |
|---|---|---|---|
| Cursor | `.cursor/skills/` | `.cursor/rules/` | `.cursor/mcp.json` |
| CodeBuddy | `.codebuddy/skills/` | `.codebuddy/rules/` | `.mcp.json` |
| Windsurf | `.windsurf/skills/` | `.windsurf/rules/` | `.windsurf/mcp.json` |
| Trae | `.trae/skills/` | `.trae/rules/` | `.trae/mcp.json` |

Dec 托管的产物统一使用 `dec-` 前缀，以便与用户手工维护的内容区分。

## 核心流程

### `dec init`

1. 创建 `.dec/config/ides.yaml`
2. 创建 `.dec/config/vault.yaml`
3. 如有需要，尝试安装 Dec Skill

### `dec vault init`

1. 连接已有 Vault，或创建新的 GitHub 私有仓库
2. 在本地 `DEC_HOME/vault` 建立 Git 工作目录
3. 把 Vault 来源写入全局 `config.yaml`

### `dec vault save`

1. 校验本地 Skill / Rule / MCP 资产格式
2. 保存到 Vault 工作目录
3. 提交到 Vault 本地 Git 仓库
4. 尝试更新当前项目的追踪信息

### `dec vault pull`

1. 从 Vault 解析指定资产
2. 同步到当前项目配置的全部 IDE
3. 自动把资产名称写入 `.dec/config/vault.yaml`
4. 更新 `.dec/tracking.json`

### `dec sync`

1. 读取 `.dec/config/ides.yaml`
2. 读取 `.dec/config/vault.yaml`
3. 打开本地 Vault，并尝试刷新远端状态
4. 将声明的 Skills / Rules / MCP 同步到目标 IDE
5. 更新 `.dec/tracking.json`

## 模块划分

### `cmd/`

命令行入口层，负责参数解析与用户输出。

- `root.go`：根命令与版本信息
- `init.go`：初始化项目配置
- `sync.go`：同步 Vault 资产到 IDE
- `vault.go`：Vault 子命令集合

### `pkg/config/`

配置读写与渲染。

- `project_v2.go`：项目级 `ides.yaml` / `vault.yaml`
- `global.go`：全局 `config.yaml`
- `system.go`：安装脚本使用的系统配置

### `pkg/vault/`

Vault 核心实现。

负责：

- 初始化 / 打开 Vault
- 资产保存、列出、搜索、拉取、推送
- 索引与追踪数据维护
- 与 Git 仓库交互

### `pkg/service/`

同步编排层。

`sync_v2.go` 负责：

- 解析项目声明
- 备份当前 IDE 中已有的 `dec-*` 托管内容
- 清理旧托管内容
- 写入新的 Skill / Rule / MCP
- 失败时回滚恢复
- 更新追踪状态

### `pkg/ide/`

IDE 抽象层，封装不同 IDE 的目录结构差异。

### `pkg/paths/`

统一管理 `DEC_HOME`、项目配置目录和 IDE 输出路径。

### `pkg/types/`

声明项目配置、MCP 配置等结构体。

### `pkg/version/`

读取版本信息，支持编译时注入与源码目录回退。

## 关键设计点

### 托管范围有限

Dec 只会覆盖自己托管的 `dec-*` 内容，不会删除用户手工维护的非托管资产。

### MCP 合并保留用户配置

同步 MCP 时：

- 当前声明的 `dec-*` 条目会被更新
- 旧的、未声明的 `dec-*` 条目会被移除
- 用户自己维护的非 `dec-*` 条目会被保留

### 同步失败可恢复

`synchronize -> clean -> write` 过程中如果任一步失败，Dec 会尝试恢复目标 IDE 中原有的托管资产，避免同步把环境留在半完成状态。

## 当前边界

以下能力**不在当前实现中**：

- 顶层 `dec list`
- `dec serve`
- `dec publish-notify`
- `technology.yaml` / `mcp.yaml` / `packs.yaml` 项目配置体系
- 依赖 GitHub Actions 的自动发布与自动注册流程

如果后续引入上述能力，应先更新源码，再同步更新本文档与根 `README.md`。
