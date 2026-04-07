# Dec 架构设计

本文档描述当前代码库的实现结构与运行机制。

用户侧命令说明与使用建议以以下文档为准：

- `README.md`：项目概览与快速开始
- `pkg/assets/dec/SKILL.md`：Dec Skill 的完整使用说明

## 概览

Dec 是一个命令驱动的个人 AI 资产管理工具，用于把 Skills、Rules、MCP 配置保存在个人 Vault 中，并在不同项目、不同 IDE 间复用。

当前命令体系围绕四类动作展开：

- 仓库连接：`dec config repo`
- 项目配置：`dec config init` / `dec config show`
- 资产管理：`dec list` / `dec search` / `dec pull` / `dec push`
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
├── config.yaml              # 全局配置（repo_url、默认 IDE、默认 editor）
├── local/
│   └── vars.yaml            # 本机级变量定义
└── repo.git/                # 本地 bare repo 缓存
```

如果设置了 `DEC_HOME`，上述目录都会迁移到 `DEC_HOME` 下。

### 项目目录

```text
.dec/
├── config.yaml              # 项目配置（v2: available/enabled + vault/item/type 结构）
├── cache/                   # 资产缓存（pull 时写入，push 时读取）
├── .version                 # 最近一次 pull 的 commit 记录
└── vars.yaml                # 项目变量定义
```

在当前项目语义下，`.dec/` 适合作为共享配置的一部分纳入版本控制：

- `config.yaml`：记录项目级 IDE / editor 覆盖，以及 available / enabled 资产清单
- `cache/`：保存 pull 下来的原始资产文件，也是 push 的读取源
- `.version`：记录当前项目最近一次 pull 对应的远端 commit
- `vars.yaml`：记录项目级变量与资产级变量覆盖

### 项目配置格式

项目配置当前版本为 `v2`，采用 `vault -> item -> type` 结构：

```yaml
version: v2

ides:
  - cursor

editor: code --wait

available:
  team:
    api-style:
      rules: true
    postgres:
      mcp: true

enabled:
  team:
    api-style:
      rules: true
```

兼容策略：

- 读取到没有 `version` 的旧配置时，按 `v1` 处理
- `v1` 结构会在加载时自动迁移为 `v2` 并回写到 `.dec/config.yaml`
- 迁移完成后，流程继续按 `v2` 配置执行

### 仓库中的 Vault 结构

远端仓库仍按 Vault 目录组织真实资产文件：

```text
<repo>/
└── <vault>/
    ├── skills/
    │   └── <name>/
    │       └── SKILL.md
    ├── rules/
    │   └── <name>.mdc
    └── mcp/
        └── <name>.json
```

Dec 通过扫描这些目录发现资产，不依赖额外索引文件。

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

### 1. 仓库连接与事务

- `dec config repo <url>` 会把远端仓库连接到本地 `repo.git` bare repo 缓存
- 读操作基于 bare repo 的最新远端引用
- 写操作通过短生命周期临时 worktree 完成，结束后自动清理
- Dec 依赖系统 `git`，认证由用户自己的 Git 环境负责

### 2. 有效 IDE 与编辑器解析

资产部署目标由以下优先级决定：

1. 项目级 `.dec/config.yaml`
2. 全局 `~/.dec/config.yaml`
3. 默认值 `cursor`

交互式编辑器由以下优先级决定：

1. 项目级 `.dec/config.yaml`
2. 全局 `~/.dec/config.yaml`
3. 自动探测到的系统编辑器

`dec config global` 的作用是安装 Dec Skill，并把默认 IDE 列表写入 `~/.dec/config.yaml`。

### 3. 资产生命周期

#### `config init`

- 扫描远端仓库中的所有 Vault 和资产
- 生成或更新项目级 `.dec/config.yaml`
- 保留已有 `enabled` / `ides` / `editor`
- 生成 `.dec/vars.yaml` 模板
- 打开编辑器让用户调整 `enabled`

#### `pull`

- 读取 `.dec/config.yaml` 中 `enabled` 的资产
- 校验它们是否仍在 `available` 中
- 清理 `.dec/cache/` 和 IDE 中已经不再启用的资产
- 从远端仓库读取资产内容
- 把原始内容写入 `.dec/cache/`
- 安装到有效 IDE 对应目录
- 对安装后的文件执行变量替换
- 把拉取来源 commit 记录到 `.dec/.version`

#### `push`

- 读取 `.dec/config.yaml` 中 `enabled` 的资产
- 从 `.dec/cache/` 查找对应缓存文件
- 将缓存文件回写到远端 Vault 目录
- 提交并推送到远端仓库

#### `push --remove`

- 在远端仓库中查找匹配资产
- 删除远端文件或目录
- 同步清理本地 `.dec/config.yaml` 和 `.dec/cache/` 中对应条目

### 4. MCP 合并策略

MCP 采用非覆盖式合并：

- Vault 中的条目以 `dec-{name}` 写入 IDE 的 MCP 配置
- 用户手工维护的非 `dec-*` 条目保持不变
- 已不再托管的 `dec-*` 条目会被清理

### 5. 变量替换

变量替换发生在 pull 后、安装到 IDE 目录之后。

优先级：

1. `.dec/vars.yaml` 中的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 中的 `vars`
3. `~/.dec/local/vars.yaml` 中的 `vars`

未定义的占位符会保留原样，并在 pull 输出中提示。

## 模块划分

### `cmd/`

命令行入口层，负责参数解析、命令编排和用户输出。

- `root.go`：根命令与版本信息
- `repo.go`：仓库连接命令
- `config.go`：配置初始化、展示与全局 IDE 配置
- `pull.go`：项目拉取与安装
- `push.go`：缓存推送与远端删除
- `list.go` / `search.go`：仓库资产浏览
- `vault.go`：共享的 Vault 扫描、缓存路径、安装辅助函数
- `update.go`：版本检查与自更新

### `pkg/config/`

配置读写与优先级解析。

- `global.go`：全局配置、旧本机配置兼容迁移、有效 IDE / editor 解析
- `project.go`：项目级 `.dec/config.yaml` 与 `.dec/vars.yaml`，以及 v1 -> v2 自动迁移

### `pkg/repo/`

Git 仓库连接、bare repo 管理、事务 worktree。

### `pkg/ide/`

IDE 抽象层，负责不同 IDE 的目录与 MCP 配置差异。

- `registry.go`：注册 cursor、codebuddy、windsurf、trae、claude、claude-internal、codex、codex-internal

### `pkg/assets/`

内置资产内容。目前包含 Dec 自身的 Skill 文本，用于 `dec config global` 安装。

### `pkg/types/`

声明全局配置、项目配置、资产列表、MCP 配置等结构体，并包含项目配置 v2 的 YAML 编解码逻辑。

### `pkg/vars/`

变量文件加载、占位符提取、变量解析与替换。

### `pkg/version/`

版本信息加载、比较与编译期注入支持。

### `pkg/update/`

检查 GitHub Release、下载新版本并执行自更新。

## 关键设计点

### 命令驱动而非声明式同步

Dec 不依赖 `dec sync` 之类的全量同步入口，而是通过 `config init` / `pull` / `push` / `push --remove` 让用户显式控制状态变化。

### 多 Vault 支持

一个仓库可以包含多个 Vault；项目配置中的 `available` 和 `enabled` 也显式记录每个资产所属的 Vault。

### 托管范围有限

Dec 只管理自己生成的 `dec-*` 产物，不主动修改用户手工维护的非托管内容。

### 基于文件系统的真实状态

Vault 中的资产以目录和文件直接组织，代码通过扫描真实目录结构发现状态，而不是依赖单独的索引数据库。

## 当前边界

以下能力不在当前实现中：

- `dec sync`
- 独立的 `pkg/vault/` 编排层
- `dec serve` / `dec publish-notify`
- `technology.yaml` / `packs.yaml` 配置体系

## 已知问题与限制

### CodeBuddy MCP 配置路径特殊

CodeBuddy 的 MCP 配置位于项目根目录 `.mcp.json`，不是 `.codebuddy/mcp.json`。该差异已经在 IDE 抽象层中单独处理。

### 文件权限不保留原始值

复制文件和目录时使用固定权限位，不保留源文件权限。

### `push --remove` 按名称查找远端资产

删除远端资产时，CLI 入口目前仍是 `dec push --remove <type> <name>`，不直接传 vault；若多个 Vault 下存在同名同类型资产，会按仓库扫描顺序命中第一个结果。

### 测试覆盖仍不完整

当前测试已经覆盖配置迁移、repo/ide 抽象和变量处理，但文档示例与部分命令组合场景仍有继续补充空间。
