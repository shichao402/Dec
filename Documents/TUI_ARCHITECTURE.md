# Dec TUI 架构

Dec 以 **TUI**（`internal/tui/`）为第一交互入口。无参运行 `dec` 在交互式终端中启动 TUI Shell；CLI 仅保留 `dec --version` 与内部 hidden 命令 `__freshness-check`。

相关文档：

- [README.md](../README.md) — 概览与快速开始
- [ARCHITECTURE.md](./ARCHITECTURE.md) — 模块划分与运行机制
- [BUNDLE-SECRETS-MODEL.md](./BUNDLE-SECRETS-MODEL.md) — Project / bundle 与 secrets bundle 同构模型
- [.cursor/rules/tui-first.mdc](../.cursor/rules/tui-first.mdc) — TUI 优先约束

## 1. 设计目标

- **TUI-first**：日常操作（连接仓库、初始化 project、选择 bundle、pull/push/remove）均在 TUI 完成，不暴露 `dec pull`、`dec config` 等用户面 CLI 子命令。
- **单二进制**：Bubble Tea 生态（`bubbletea`、`bubbles`、`lipgloss`），无 Node.js 或前端运行时。
- **服务 / 门面**：TUI 与 Agent MCP 都通过本机 gRPC 调 `dec-server`；只有服务进程调用 `internal/app/`。TUI 禁止内嵌 app 降级或 shell out 调业务子命令。
- **结构化事件**：长任务通过 `Reporter` / `OperationEvent` 暴露进度，TUI Run 页渲染日志与阶段状态。

## 2. 入口路由

```text
dec（无参）+ stdin/stdout/stderr 均为 TTY + TERM != dumb + DEC_NO_TUI != 1
  → internal/tui/ 启动 Bubble Tea

dec --version / dec --help / dec __freshness-check
  → Cobra CLI

非 TTY 或 DEC_NO_TUI=1
  → 简短说明后退出，不启动 TUI
```

实现位于 `cmd/root.go` 的 `decideEntryMode`；测试见 `cmd/root_test.go`。

## 3. 页面与能力映射

| 页面 | 职责 |
|------|------|
| **Home** | 项目概览、建议下一步、**project 初始化**（自动匹配 vault 同名 project、选择或新建） |
| **Bundles** | 扫描 vault bundle、浏览/搜索资产、调整项目 `enabled_bundles` / `enabled` 并保存 |
| **Project** | 项目级 IDE / editor 覆盖、**项目变量**（`.dec/vars.yaml` 只读预览，按 `e` 挂起外部编辑器） |
| **Run** | pull / push / remove；一次 pull 解析 project bundle 列表 → Dec Git bundle + Bitwarden secrets bundle；成功对照远端的启用集自动 prune 本地孤儿（无法确认只报告） |
| **Remote** | 上下文无关的完整远端浏览器/编辑器（Dec Git vault 全量 + Bitwarden 全部 folder + 无文件夹只读区）；`e` temp 编辑、`n` 登记到光标所在 folder / `N` 登记到新 folder、`a`/`A` 全选/全不选、远端/本地删除拆分、跨上下文 typed confirm（ADR 0004） |
| **Settings** | 连接 Git 仓库、Bitwarden 配置、全局 IDE / editor、本机用户级 bundle 启用、**本机 vars** 外部编辑、服务版本 mismatch 提示与重启 `dec-server` |

侧栏导航：`Home` → `Bundles` → `Project` → `Run` → `Remote` → `Settings`（`tab` / `shift+tab` 切换）。

`dec --user` 进入用户平面，页列为 `Home` → `Bundles` → `Run` → `Remote` → `Settings`。Bundles / Run 的读写落点按平面解析（`~/.dec/cache`、`~/.dec/secrets`、`~/.cursor` 等），只暴露 `scope: user` 的 bundle；**Remote 页可见性不按平面过滤**（全量远端库存 + 本机清理分区）。Project 页不开放（项目变量在用户平面无对应概念）。见 [0009](decisions/0009-bundle-binary-scope.md) §4 与 [0004](decisions/0004-remote-page.md)。

## 4. 模块分层

```text
cmd/
  root.go              # 入口路由、dec --version
  freshness_check.go   # hidden __freshness-check

internal/serviceapi/        # TUI / MCP 共用的服务客户端
internal/servicehost/       # dec-server gRPC 与 project 操作协调
internal/app/               # 服务端用例层
  project.go           # project init、配置写入
  assets.go            # Assets 页选择与持久化
  operations.go        # pull / push / remove 编排
  settings.go          # Settings 页仓库与全局配置
  overview.go          # Home 概览
  events.go            # Reporter / OperationEvent

internal/tui/          # Bubble Tea 展示层
  app.go               # tea.Program 启动
  model.go             # Shell model、页面渲染与快捷键

internal/config/ internal/repo/ internal/ide/ internal/vars/ internal/types/
internal/freshness/         # 远端新鲜度后台检查
```

TUI **不得**直接调用 `cmd/*`、`internal/app` 或 `fmt.Printf` 式业务输出；所有动作走 `internal/serviceapi`，由服务把 `Reporter` 事件映射为进度流。TUI 还会按 project 旁观 MCP 发起的活跃操作。

## 5. 关键交互流

### 5.1 首次使用

1. 运行 `dec` → TUI 启动
2. **Settings** → 连接个人 Git 仓库 URL、配置本机 IDE
3. **Home** → 初始化 project（推断目录 basename → 自动匹配 vault `projects/<name>.yaml`，或选择/新建）
4. **Assets** → 确认 bundle / 资产启用
5. **Run** → pull（Dec bundle → `.dec/cache/` + secrets bundle → 项目根 / `~/.ssh/` → 渲染 IDE）

### 5.2 新机器同名 project 自动匹配

工作区目录 basename 与 vault 中 `projects/<basename>.yaml` 同名时，Home 初始化 **自动应用** vault project，写入本地 `project_name` 与 `enabled_bundles`，无需手工复制 bundle 列表。详见 [ARCHITECTURE.md — Project 初始化](./ARCHITECTURE.md#project-初始化tuifirst)。

### 5.3 Run 页 pull

1. 解析 `project_name` → vault `projects/<name>.yaml` → bundles 列表（或本地 `enabled_bundles`）
2. 逐 bundle 拉 Dec Git → `.dec/cache/<bundle>/`
3. 自动拉 Bitwarden secrets bundle → Secure Note 项目根相对路径；SSH Key → `~/.ssh/`
4. 零重叠校验 → 从 cache 渲染 IDE + 非敏感 vars 占位符替换
5. **孤儿 reconcile**：仅对本次启用且远端对照成功的 SyncTarget / vault 目标集清理本地孤儿；无法确认则只报告（见 [0010](decisions/0010-pull-orphan-and-ops.md)）

缺 Bitwarden session 时由 `dec-server` 自动触发 web unlock（服务进程内存 session，见 [BUNDLE-SECRETS-MODEL.md](./BUNDLE-SECRETS-MODEL.md#bitwarden-认证)）。

### 5.4 Project 页变量编辑

- 按 `e` 通过 `tea.ExecProcess` 挂起 TUI、拉起外部编辑器编辑 `.dec/vars.yaml`
- **禁止**在 TUI 内直接调用 `editor.Open`（会与 Bubble Tea 持有的 TTY 冲突）
- `.dec/cache/` 不存在时显示提示，占位符扫描依赖 prior pull

### 5.4b Settings 本机 vars / 服务重启

- 本机用户级 vars（`~/.dec/local/vars.yaml`）同样按 `e` 外部编辑
- 门面与 `dec-server` 版本不一致时提示；Settings 可确认重启服务（清空进程内 Bitwarden session）

### 5.4c Remote 页要点

- 进入默认拉全量远端库存（`ListRemoteInventory`）；`r` 强制刷新
- `e`：Secure Note / SSH Hosts → temp → 写回远端（不种本地）
- `n`：光标所在 folder 登记；`N`：手输新 folder。Processor 同级（`note` / `.env` / `.gcm` / `.sshkey`），来源由类型声明（temp/路径/系统选文件/本机生成）；不枚举远端 folder 候选
- `a` 全选 / `A` 全不选
- `d`：远端分区只改远端；本地分区只清本机；跨上下文需 typed confirm
- 「无文件夹」只读折叠区，不可勾选删除

### 5.5 跨页异步 IO（强制约定）

远端 Bitwarden / Git、以及其它较慢 IO，**不得**绑定到「当前页面是否仍可见」：

1. **任务跟 Shell，不跟页面**：切页禁止 `cancel` 在飞请求（见 `internal/tui/async_io.go`）。
2. **独立 busy 信号**：`asyncLoad.loading` + `ioBusyLabel` / 状态栏，避免用户以为卡住。
3. **防重复**：已在飞且未升级范围则 no-op；缓存仍有效且未 force 则 no-op。
4. **世代丢弃**：仅「新一代请求取代旧请求」时 abort；完成回调用 `gen` 丢弃过期结果。
5. **状态栏全局可见**：即使人不在发起页，busy 文案仍应显示。

参考实现（已迁）：

- Remote 候选列表：`deleteLoad`（内部标识沿用，页签为 Remote）
- Shell 并联刷新：`shellRefresh`（overview / assets / settings / projectSettings / vars）
- 独立 vars 重载：`projectVarsLoad`
- Builtin IDE assets：`builtinAssetsLoad`
- Vault project 应用 / 本地 project 生成 / 仓库扫描：`vaultApplyLoad` / `localProjectLoad` / `projectInitLoad`
- Push 预览：`pushPreviewLoad`（`pushStage=loading`）

用户主动的 pull/push/remove/delete **执行**仍可用 Esc 取消（与「切页不打断加载」不同）。

## 6. 测试策略

| 层级 | 位置 | 覆盖 |
|------|------|------|
| 用例层单测 | `internal/app/*_test.go` | 结构化结果与事件序列 |
| TUI model | `internal/tui/model_test.go` | 状态迁移、快捷键、Reporter 接入 |
| 宽度回归 | `internal/tui/width_test.go` | 60 / 80 / 100 / 140 列不溢出 |
| Snapshot | `internal/tui/snapshot_test.go` | Home / Assets / Run / Settings × 80 / 100 / 140 |
| 入口路由 | `cmd/root_test.go` | TTY / DEC_NO_TUI / 子命令分流 |
| PTY 集成 | `internal/tui/pty_integration_test.go` | `go test -tags=integration`（非 Windows） |

更新 snapshot golden：`go test ./internal/tui/ -run TestSnapshot -update`

## 7. 非交互与 Agent

- `DEC_NO_TUI=1` 或 stdout 非 TTY：不启动 TUI，输出简短说明
- Agent 通过 `dec-mcp`，内部 CI 代码通过服务客户端调用 `dec-server`，而非假设存在 CLI 子命令

## 8. 已知限制

- **freshness**：`internal/freshness/` 后台检查已实现，与 TUI 启动 / Run 页的深度集成待完善
- **remove 按名称查找**：多个 vault 存在同名同类型资产时，按扫描顺序命中第一个
- **CodeBuddy MCP**：项目根 `.mcp.json`；Codex 为 `.codex/config.toml`
