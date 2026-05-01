# Dec 架构设计

本文档描述当前代码库的实现结构与运行机制。

用户侧命令说明与使用建议以以下文档为准：

- `README.md`：项目概览与快速开始
- `pkg/assets/dec/SKILL.md`：Dec Skill 的完整使用说明
- `pkg/assets/dec-extract-asset/SKILL.md`：Dec 资产沉淀 Skill 的完整使用说明

## 概览

Dec 是一个以 Cobra CLI 为自动化接口、并在交互式无参启动时默认进入 TUI Shell 的个人 AI 资产管理工具，用于把 Skills、Rules、MCP 配置保存在个人 Vault 中，并在不同项目、不同 IDE 间复用。

当前命令体系围绕四类动作展开：

- 仓库连接：`dec config repo`
- 项目配置：`dec config init` / `dec config show`
- 资产管理：`dec list` / `dec search` / `dec pull` / `dec push`
- 版本更新：`dec update`

## 文档边界

为了减少重复：

- `README.md` 负责概览、安装、快速上手
- `pkg/assets/dec/SKILL.md` 负责完整的用户操作语义和 Skill 资产说明
- `pkg/assets/dec-extract-asset/SKILL.md` 负责把当前项目能力沉淀进 Dec 的操作语义
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
| Claude | `.claude/skills/` | `.claude/rules/` | `.claude/mcp.json` |
| Claude Internal | `.claude/skills/` | `.claude/rules/` | `.claude/mcp.json` |
| Codex | `.codex/skills/` | `.codex/rules/` | `.codex/config.toml` |
| Codex Internal | `.codex/skills/` | `.codex/rules/` | `.codex/config.toml` |

Dec 托管产物统一使用 `dec-` 前缀，以便和用户手工维护的内容区分。

其中 `claude-internal` 在项目级复用 `.claude/`，`codex-internal` 在项目级复用 `.codex/`；只有用户级目录仍然分别是 `~/.claude-internal/` 和 `~/.codex-internal/`。

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

`dec config global` 的作用是安装 Dec 跟随分发的内置资产，并把默认 IDE 列表写入 `~/.dec/config.yaml`。

### 3. 用户级内置资产安装

`dec config global` 不是把某个单独说明文档写进 IDE，而是安装一组跟随 Dec 二进制分发的内置资产。

当前内置资产 bundle 为：

- `dec`：通用 Dec 操作 Skill
- `dec-extract-asset`：把当前项目中的本地能力沉淀为 Dec 资产的 Skill

安装机制约束：

- 内置资产内容由 `pkg/assets/` 通过 embed 打包进二进制
- `cmd/config.go` 负责把 bundle 安装到各 IDE 的用户级目录
- 安装器按资产类型分发，目前实际写入的是 Skills
- Rule / MCP 已有独立安装函数入口，但当前 bundle 仍为空，后续可在不改命令语义的前提下继续扩展

用户级路径与项目级路径不是同一套映射：

- `claude-internal` 项目级继续复用 `.claude/`，但用户级安装目标是 `~/.claude-internal/`
- `codex-internal` 项目级继续复用 `.codex/`，但用户级安装目标是 `~/.codex-internal/`
- 这类差异由 `pkg/ide/` 中的 IDE 抽象负责解析

### 3.1 内置资产源码、项目缓存与 Vault 资产的边界

这里有三套很容易混淆的内容平面，必须严格区分：

- **Dec 产品源码**：当前仓库中的 `pkg/assets/`、`cmd/`、`pkg/`、`Documents/`、`README.md`、`version.json` 等。这些内容通过构建进入 Dec 二进制，属于 Dec 自身发布物的一部分。
- **项目级落地产物**：业务仓库中的 `.dec/`、`.cursor/`、`.claude/`、`.codex/`、`.codebuddy/`、`.mcp.json` 等。这些是 `dec config init` / `dec pull` 作用到某个项目后的结果。
- **Vault 资产源**：远端 Vault 仓库中的 `<vault>/skills/`、`<vault>/rules/`、`<vault>/mcp/`，以及当前项目里作为其 push 输入源的 `.dec/cache/`。

因此有两个不能再混淆的规则：

- 修改 `pkg/assets/dec/SKILL.md`、`pkg/assets/dec-extract-asset/SKILL.md` 或其他内置资产源码时，**不要**使用 `dec push` 试图“发布”它们。`dec push` 不读取 `pkg/assets/`，也不会更新 Dec 安装包或 GitHub Release。
- `dec push` 只负责把当前项目 `.dec/cache/` 中的已启用资产写回个人 Vault 仓库；它适用于用户资产同步，不适用于 Dec 自身内置资产发布。

内置资产的正确发布路径是：

- 在 Dec 源码仓库中修改对应文件，例如 `pkg/assets/dec/SKILL.md`
- 提交到当前仓库，并在需要发版时更新 `version.json`
- push 到 `main`，由 GitHub Actions 基于版本变更创建 tag / Release / `ReleaseLatest`

换句话说：

- **内置 Skill 变更** 是 Dec 产品改动，走正常代码提交与版本发布流程
- **`.dec/cache/` 变更** 才是用户资产改动，走 `dec push`

### 4. 资产生命周期

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

额外约束：

- `push` 的读取源只有项目 `.dec/cache/`，不会读取 Dec 源码仓库中的 `pkg/assets/`
- 因此修改内置 Skill / Rule / MCP 时，验证可以依赖测试与自举验收，但发布必须走源码仓库的 commit + version + release 流程，不能用 `dec push` 代替

#### `push --remove`

- 在远端仓库中查找匹配资产
- 删除远端文件或目录
- 同步清理本地 `.dec/config.yaml` 和 `.dec/cache/` 中对应条目

### 5. MCP 合并策略

MCP 采用非覆盖式合并：

- Vault 中的条目以 `dec-{name}` 写入 IDE 的 MCP 配置
- 用户手工维护的非 `dec-*` 条目保持不变
- 已不再托管的 `dec-*` 条目会被清理

### 6. freshness 被动检查

Dec 在每次 CLI 命令结束后，会被动检查远端 Vault 是否有新提交，并在下一条命令开始前打印一次 `dec pull` 提示。核心目标是零阻塞主命令，代价是提示会延后到"下一条命令"。

实现位于 `pkg/freshness/` 与 `cmd/freshness_check.go`：

- **分离子进程执行 fetch**：PostRun 阶段 `cmd.Start()` 启动 hidden cobra 子命令 `__freshness-check --project-root`，立即返回不 `Wait()`
  - Unix：`syscall.SysProcAttr{Setsid: true}`（`pkg/freshness/async_unix.go`）
  - Windows：`CreationFlags |= DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP`（`pkg/freshness/async_windows.go`）
- **两阶段协作**：当前命令 PostRun fork 子进程写 cache → 下一条命令 PreRun 读 cache 打印提示
- **cache**：`~/.dec/local/freshness-result.<sha1>.json`，24h TTL（沿用 `Interval()`），atomic `.tmp` + rename 写入
- **lock**：`~/.dec/local/freshness.lock`，`O_CREATE|O_EXCL` 独占。busy 时静默 skip，避免与 `dec pull/push` 撞车；mtime 超过 10 分钟视为僵尸自动回收
- **dec pull 成功后清 cache**：`cmd/pull.go` 调 `freshness.InvalidateCache(cwd)`，防止旧 local commit 让下一次提示误报
- **throttle 与 cache 共用 SHA1**：`pkg/freshness/freshness.go:hashProjectRoot()` 让 `last-freshness-check.<hash>` 与 `freshness-result.<hash>.json` 指向同一项目

所有静默路径（disabled / 未 pull / lock busy / throttled / cache 中 `Err` 非空）都不会打扰用户。

### 7. 变量替换

变量替换发生在 pull 后、安装到 IDE 目录之后。

优先级：

1. `.dec/vars.yaml` 中的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 中的 `vars`
3. `~/.dec/local/vars.yaml` 中的 `vars`

未定义的占位符会保留原样，并在 pull 输出中提示。

## 模块划分

### `cmd/`

命令行入口层，负责参数解析、命令编排和用户输出。
同时，根入口还负责在默认 TUI 和传统 CLI 之间做分流。

- `root.go`：根命令、版本信息，以及 `dec` 无参启动时的入口分流
- `repo.go`：仓库连接命令
- `config.go`：配置初始化、展示与全局 IDE 配置
- `pull.go`：项目拉取与安装
- `push.go`：缓存推送与远端删除
- `list.go` / `search.go`：仓库资产浏览
- `vault.go`：共享的 Vault 扫描、缓存路径、安装辅助函数
- `update.go`：版本检查与自更新

`cmd/*` 当前仍是 CLI 适配层，但 `config init`、`config repo`、`config global` 与 `pull` 的非交互编排已经开始下沉到 `pkg/app/`，后续 `push` / 默认 TUI 会继续沿这条边界演进。

### `pkg/app/`

用例层，负责把底层 repo/config/ide 能力编排成可复用的结构化操作结果，而不是直接向终端打印文本。

当前已落地的边界：

- `project.go`：`config init` 的仓库扫描、项目配置写入、vars 模板准备
- `overview.go`：TUI 首页所需的项目概览聚合，包括仓库连接、项目配置、启用资产数、有效 IDE 和编辑器
- `assets.go`：TUI Assets 页所需的资产选择状态加载与保存，包括 enabled 切换持久化、保留 IDE/editor、确保 vars 模板
- `operations.go`：`pull` 的复用编排层，负责配置校验、旧布局迁移、清理失效资产、缓存写入、IDE 安装、变量替换与结构化结果汇总
- `settings.go`：全局设置与仓库连接用例，负责 repo connect、默认 IDE 保存、用户级内置资产安装与全局 vars 模板准备
- `events.go`：`Reporter` / `OperationEvent` 事件模型，供 CLI 与 TUI 共享执行过程

当前 CLI 仍保留交互式编辑器打开、最终输出和用户提示；`pkg/app` 负责承接可复用的非交互业务步骤，供 CLI 与 TUI 共享，其中 `config init`、`config repo`、`config global` 与 `pull` 已经走到这条边界上。

### `internal/tui/`

交互式展示层，当前承接默认入口下的最小可用 TUI Shell。

- `app.go`：Bubble Tea 程序启动与 IO 绑定
- `model.go`：全局 Shell model，负责首页、导航、Assets / Run / Settings 页交互、状态栏、日志区与刷新逻辑

当前阶段已经接入：

- `dec` 在交互式无参数场景下进入 TUI
- 首页展示仓库/项目概览、导航、状态栏和最近日志
- `Assets` 页可加载仓库资产、按关键字筛选、切换启用状态，并保存到 `.dec/config.yaml`
- `Run` 页可触发一次 `pull`，展示阶段进度、执行日志和最近一次结果
- `Settings` 页可编辑 repo URL、勾选默认 IDE，并通过同一用例层完成仓库连接、内置资产安装与全局 vars 模板准备
- `pull` 或 `Settings` 保存完成后会刷新首页概览、资产状态与全局设置；`Project` 页仍保留占位页

### `pkg/config/`

配置读写与优先级解析。

- `global.go`：全局配置、旧本机配置兼容迁移、有效 IDE / editor 解析
- `project.go`：项目级 `.dec/config.yaml` 与 `.dec/vars.yaml`，以及 v1 -> v2 自动迁移

### `pkg/repo/`

Git 仓库连接、bare repo 管理、事务 worktree。

### `pkg/ide/`

IDE 抽象层，负责不同 IDE 的目录与 MCP 配置差异。

- `registry.go`：注册 cursor、codebuddy、claude、claude-internal、codex、codex-internal
- 同时区分项目级输出目录与用户级内置资产安装目录

### `pkg/assets/`

内置资产内容与装载逻辑。目前包含 `dec` 与 `dec-extract-asset` 两个内置 skill，并为未来内置 rule / mcp 预留统一 bundle 结构。

这里的文件不是 Vault 里的用户资产副本，而是 Dec 产品源码的一部分。任何对 `pkg/assets/*` 的修改，都必须通过重新构建和发布 Dec 才会进入用户安装结果。

### `pkg/types/`

声明全局配置、项目配置、资产列表、MCP 配置等结构体，并包含项目配置 v2 的 YAML 编解码逻辑。

### `pkg/vars/`

变量文件加载、占位符提取、变量解析与替换。

### `pkg/version/`

版本信息加载、比较与编译期注入支持。

### `pkg/update/`

检查 GitHub Release、下载新版本并执行自更新。

### `pkg/freshness/`

被动式远端 freshness 检查。负责 fork 分离子进程执行 `git fetch`、结果 cache、lock 互斥、以及跨平台 detach 行为（`async_unix.go` / `async_windows.go`）。调用入口在 `cmd/root.go` 的 PreRun / PostRun 以及 hidden 子命令 `cmd/freshness_check.go`。

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

Codex / Codex Internal 的项目级 MCP 配置位于 `.codex/config.toml`，不是 `mcp.json`。其中 `codex-internal` 在项目级同样复用 `.codex/`；只有用户级目录仍然是 `~/.codex-internal/`。Dec 会把托管的 MCP server 写入 `config.toml` 的 `[mcp_servers.<name>]` 段，同时保留现有的其他 Codex 配置。

### 文件权限不保留原始值

复制文件和目录时使用固定权限位，不保留源文件权限。

### `push --remove` 按名称查找远端资产

删除远端资产时，CLI 入口目前仍是 `dec push --remove <type> <name>`，不直接传 vault；若多个 Vault 下存在同名同类型资产，会按仓库扫描顺序命中第一个结果。

### 测试覆盖仍不完整

当前测试已经覆盖配置迁移、repo/ide 抽象和变量处理，但文档示例与部分命令组合场景仍有继续补充空间。
