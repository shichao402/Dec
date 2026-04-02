# Dec 架构设计

本文档描述当前代码库的实现结构与运行机制。

用户侧命令说明与使用建议以以下文档为准：

- `README.md`：项目概览与快速开始
- `pkg/assets/dec/SKILL.md`：Dec Skill 资产的完整使用说明

## 概览

Dec 是一个命令驱动的个人 AI 资产管理工具，用于把 Skills、Rules、MCP 配置保存在个人 Vault 中，并在不同项目、不同 IDE 间复用。

当前命令体系围绕三类动作展开：

- 连接仓库：`dec repo`
- 配置 IDE：`dec config global`
- 管理资产：`dec vault init/import/list/search/pull/push/remove`
- 版本更新：`dec update`

## 文档边界

为了减少重复：

- `README.md` 负责概览、安装、快速上手
- `pkg/assets/dec/SKILL.md` 负责完整的用户操作语义和 Skill 资产说明
- 本文档只保留架构、目录结构、模块边界和关键运行机制

## 目录结构

### Dec 根目录

```text
~/.dec/
├── config.yaml              # 全局配置（Repo URL 等）
├── local/
│   └── config.yaml          # 本机 IDE 列表
└── repo.git/                # 本地 bare repo 缓存
```

如果设置了 `DEC_HOME`，上述目录都会迁移到 `DEC_HOME` 下。

### 项目目录

```text
.dec/
├── config.yaml              # 项目配置（Vault 列表、项目级 IDE 覆盖）
├── assets.yaml              # 已安装资产追踪
└── templates/              # 已拉取/已导入资产的原始模板缓存
```

### IDE 托管输出

| IDE | Skills | Rules | MCP |
|---|---|---|---|
| Cursor | `.cursor/skills/` | `.cursor/rules/` | `.cursor/mcp.json` |
| CodeBuddy | `.codebuddy/skills/` | `.codebuddy/rules/` | `.mcp.json` |
| Windsurf | `.windsurf/skills/` | `.windsurf/rules/` | `.windsurf/mcp.json` |
| Trae | `.trae/skills/` | `.trae/rules/` | `.trae/mcp.json` |
| Claude | `.claude/skills/` | `.claude/rules/` | `.claude/mcp.json` |
| Claude Internal | `.claude-internal/skills/` | `.claude-internal/rules/` | `.claude-internal/mcp.json` |
| Codex | `.codex/skills/` | `.codex/rules/` | `.codex/mcp.json` |
| Codex Internal | `.codex-internal/skills/` | `.codex-internal/rules/` | `.codex-internal/mcp.json` |

Dec 托管产物统一使用 `dec-` 前缀，以便和用户手工维护的内容区分。

## 关键运行机制

### 1. 仓库连接与写事务

- `dec repo <url>` 会把远端仓库连接到本地 `repo.git` bare repo 缓存
- 读操作基于 bare repo 的最新远端引用
- 写操作通过短生命周期临时 worktree 完成，结束后自动清理
- Dec 依赖系统 `git`，认证由用户自己的 Git 环境负责

### 2. 有效 IDE 解析

资产部署目标由以下优先级决定：

1. 项目级 `.dec/config.yaml`
2. 本机级 `~/.dec/local/config.yaml`
3. 默认值 `cursor`

`dec config global` 的作用是安装 Dec Skill，并把本机默认 IDE 列表写入 `~/.dec/local/config.yaml`。

### 3. 资产生命周期

#### `import`

- 校验资产格式
- 写入 `repo.git` 对应 Vault 的目录结构
- 提交并推送到远端
- 在项目侧保存模板与追踪信息

#### `pull`

- 从 Vault 查找资产
- 把原始模板保存到 `.dec/templates/`
- 安装到有效 IDE 对应目录
- 更新 `.dec/assets.yaml`

#### `push`

- 读取 `.dec/assets.yaml` 中已追踪资产
- 以 `.dec/templates/` 中的模板为推送源
- 回写到对应 Vault 后提交并推送

#### `remove`

- 从托管 IDE 目录删除本地资产
- 从 `.dec/assets.yaml` 删除追踪记录
- 使用 `--remote` 时，同时删除 Vault 里的远端资产

### 4. MCP 合并策略

MCP 采用非覆盖式合并：

- Vault 中的条目以 `dec-{name}` 写入 IDE 的 MCP 配置
- 用户手工维护的非 `dec-*` 条目保持不变
- 已不再托管的 `dec-*` 条目会被清理

### 5. 资产发现方式

Dec 直接扫描 Vault 目录结构发现资产，不依赖额外索引文件。

## 模块划分

### `cmd/`

命令行入口层，负责参数解析、命令编排和用户输出。

- `root.go`：根命令与版本信息
- `repo.go`：仓库连接命令
- `config.go`：配置查看与全局 IDE 配置
- `vault.go`：Vault 子命令与资产编排
- `update.go`：版本检查与自更新

### `pkg/config/`

配置读写与优先级解析。

- `global.go`：全局配置、本机 IDE 配置、有效 IDE 解析
- `project.go`：项目级 `.dec/config.yaml` 与 `.dec/assets.yaml`

### `pkg/repo/`

Git 仓库连接、bare repo 管理、事务 worktree。

### `pkg/ide/`

IDE 抽象层，负责不同 IDE 的目录与 MCP 配置差异。

- `registry.go`：注册 cursor、codebuddy、windsurf、trae、claude、claude-internal、codex、codex-internal

### `pkg/assets/`

内置资产内容。目前包含 Dec 自身的 Skill 文本，用于 `dec config global` 安装。

### `pkg/types/`

声明全局配置、项目配置、资产追踪、MCP 配置等结构体。

### `pkg/version/`

版本信息加载、比较与编译期注入支持。

### `pkg/update/`

检查 GitHub Release、下载新版本并执行自更新。

## 关键设计点

### 命令驱动而非声明式同步

Dec 不依赖 `dec sync` 之类的全量同步入口，而是通过 `pull` / `push` / `remove` 让用户显式控制每次操作。

### 多 Vault 支持

一个仓库可以包含多个 Vault，一个项目也可以关联多个 Vault。`import`、`pull --all` 等命令可结合 `--vault` 精确指定目标空间。

### 托管范围有限

Dec 只管理自己生成的 `dec-*` 产物，不主动修改用户手工维护的非托管内容。

### 基于文件系统的真实状态

Vault 中的资产以目录和文件直接组织，代码通过扫描真实目录结构发现状态，而不是依赖单独的索引数据库。

## 当前边界

以下能力不在当前实现中：

- `dec init` / `dec sync`
- 独立的 `pkg/vault/` 编排层
- `dec serve` / `dec publish-notify`
- `technology.yaml` / `packs.yaml` 配置体系

## 已知问题与限制

### CodeBuddy MCP 配置路径特殊

CodeBuddy 的 MCP 配置位于项目根目录 `.mcp.json`，不是 `.codebuddy/mcp.json`。该差异已经在 IDE 抽象层中单独处理。

### 文件权限不保留原始值

复制文件和目录时使用固定权限位，不保留源文件权限。

### 测试覆盖仍不完整

当前测试主要集中在 `pkg/ide/`、`pkg/repo/`、`pkg/version/` 等基础模块，命令层仍有补充空间。
