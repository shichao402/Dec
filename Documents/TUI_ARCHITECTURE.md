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
- **分层复用**：TUI 与 Agent / CI 共用 `pkg/app/` 用例层；TUI 禁止 shell out 调 CLI 子命令。
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
| **Assets** | 扫描 vault bundle、浏览/搜索资产、调整 `enabled_bundles` / `enabled` 并保存 |
| **Project** | 项目级 IDE / editor 覆盖、**项目变量**（`.dec/vars.yaml` 只读预览，按 `e` 挂起外部编辑器） |
| **Run** | pull / push / remove；一次 pull 解析 project bundle 列表 → Dec Git bundle + Bitwarden secrets bundle |
| **Settings** | 连接 Git 仓库、Bitwarden 配置、全局 IDE / editor、本机 `~/.dec/local/vars.yaml` |

侧栏导航：`Home` → `Assets` → `Project` → `Run` → `Settings`（`tab` / `shift+tab` 切换）。

## 4. 模块分层

```text
cmd/
  root.go              # 入口路由、dec --version
  freshness_check.go   # hidden __freshness-check

pkg/app/               # 用例层（TUI 与 Agent 共用）
  project.go           # project init、配置写入
  assets.go            # Assets 页选择与持久化
  operations.go        # pull / push / remove 编排
  settings.go          # Settings 页仓库与全局配置
  overview.go          # Home 概览
  events.go            # Reporter / OperationEvent

internal/tui/          # Bubble Tea 展示层
  app.go               # tea.Program 启动
  model.go             # Shell model、页面渲染与快捷键

pkg/config/ pkg/repo/ pkg/ide/ pkg/vars/ pkg/types/
pkg/freshness/         # 远端新鲜度后台检查
```

TUI **不得**直接调用 `cmd/*` 或 `fmt.Printf` 式业务输出；所有动作走 `pkg/app` 并订阅 `Reporter` 事件。

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

缺 Bitwarden session 时自动触发 web unlock（进程内存 session，见 [BUNDLE-SECRETS-MODEL.md](./BUNDLE-SECRETS-MODEL.md#bitwarden-认证)）。

### 5.4 Project 页变量编辑

- 按 `e` 通过 `tea.ExecProcess` 挂起 TUI、拉起外部编辑器编辑 `.dec/vars.yaml`
- **禁止**在 TUI 内直接调用 `editor.Open`（会与 Bubble Tea 持有的 TTY 冲突）
- `.dec/cache/` 不存在时显示提示，占位符扫描依赖 prior pull

## 6. 测试策略

| 层级 | 位置 | 覆盖 |
|------|------|------|
| 用例层单测 | `pkg/app/*_test.go` | 结构化结果与事件序列 |
| TUI model | `internal/tui/model_test.go` | 状态迁移、快捷键、Reporter 接入 |
| 宽度回归 | `internal/tui/width_test.go` | 60 / 80 / 100 / 140 列不溢出 |
| Snapshot | `internal/tui/snapshot_test.go` | Home / Assets / Run / Settings × 80 / 100 / 140 |
| 入口路由 | `cmd/root_test.go` | TTY / DEC_NO_TUI / 子命令分流 |
| PTY 集成 | `internal/tui/pty_integration_test.go` | `go test -tags=integration`（非 Windows） |

更新 snapshot golden：`go test ./internal/tui/ -run TestSnapshot -update`

## 7. 非交互与 Agent

- `DEC_NO_TUI=1` 或 stdout 非 TTY：不启动 TUI，输出简短说明
- Agent / CI 应调用 `pkg/app/` API（如 `app.PullProjectAssets`），而非假设存在 CLI 子命令

## 8. 已知限制

- **freshness**：`pkg/freshness/` 后台检查已实现，与 TUI 启动 / Run 页的深度集成待完善
- **remove 按名称查找**：多个 vault 存在同名同类型资产时，按扫描顺序命中第一个
- **CodeBuddy MCP**：项目根 `.mcp.json`；Codex 为 `.codex/config.toml`
