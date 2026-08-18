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
| **Bundles** | 扫描 vault bundle、浏览/搜索资产、调整 `enabled_bundles` 并保存（保存按平面校验 vault 声明，被拒条目在事件区列明） |
| **Project** | 项目级 IDE / editor 覆盖、**项目变量**（`.dec/vars.yaml` 只读预览，按 `e` 挂起外部编辑器） |
| **Run** | pull / push / remove；一次 pull 解析 project bundle 列表 → Dec Git bundle + Bitwarden secrets bundle；成功对照远端的启用集自动 prune 本地孤儿（无法确认只报告） |
| **Remote** | 上下文无关的完整远端浏览器/编辑器（Dec Git vault 全量 + Bitwarden 全部 folder + 无文件夹只读区）；`e` temp 编辑、`n` 登记到光标所在 folder / `N` 登记到新 folder、`a`/`A` 全选/全不选、远端/本地删除拆分、跨上下文 typed confirm（ADR 0004） |
| **Settings** | 连接 Git 仓库、Bitwarden 配置、全局 IDE / editor、**本机 vars** 外部编辑、服务版本 mismatch 提示与重启 `dec-server`；用户级 bundle 启用只做只读计数展示 |

侧栏导航：`Home` → `Bundles` → `Project` → `Run` → `Remote` → `Settings`（`tab` / `shift+tab` 切换）。

`dec --user` 进入用户平面，页列为 `Home` → `Bundles` → `Run` → `Remote` → `Settings`。Bundles / Run 的读写落点按平面解析（`~/.dec/cache`、`~/.dec/secrets`、`~/.cursor` 等），只暴露 `scope: user` 的 bundle；**Remote 页可见性不按平面过滤**（全量远端库存 + 本机清理分区）。Project 页不开放（项目变量在用户平面无对应概念）。见 [0009](decisions/0009-bundle-binary-scope.md) §4 与 [0004](decisions/0004-remote-page.md)。

用户平面的 `Workspace.Root` 是空串，**不得**发起任何项目配置读写：Home 加载完 overview 后的 vault project 推断只在项目平面发起。空项目根会让 `.dec/` 退化成相对 `dec-server` cwd 的路径，进而覆盖全局配置；`ProjectConfigManager` 已在实现侧直接拒绝，TUI 这层不发起是为了不去撞那道墙。见 [0015](decisions/0015-project-config-boundary.md)。

用户平面的 Bundles 页是 `GlobalConfig.EnabledBundles` 的**唯一写入口**（Settings 只读展示计数，避免两处基于各自快照互相覆盖）。候选除 vault 扫描结果外，还合入 `known_secret_bundles` 与 Bitwarden folder 枚举。补进来的候选按 vault scope 分流（ADR 0013）：vault 尚无 manifest 的标 `SecretsOnly`，展示「仓库未登记」，勾选保存时由 `ensureVaultBundlesForUserEnable` 补一份 `scope: user` 声明，标记随之消失；vault 已有 manifest 但 scope 属于另一平面的标 `OtherPlane`，展示「属于项目平面」、复选框为 `[-]` 且不可勾选——跨平面要先显式改 manifest 的 scope，绝不由一次勾选静默改写。保存时先校验/修复共享 vault 再写本机启用列表，被拒条目不进 `enabled_bundles`。无 Bitwarden session 时候选退化为 known ∪ 已启用，不为列候选触发 web unlock。

`SecretsOnly` 还要再按**远端核对结果**分流：`known_secret_bundles` 是只增不减的本机缓存（只有 pull reconcile / 删除 bundle 才摘），远端删掉 folder 后名字会一直留着。远端枚举成功且名单里没有该名字时，未启用的直接 `ForgetSecretBundles` 摘掉本机残留（不进候选），已启用的标 `RemoteMissing`，展示「远端无内容 · 本机残留」——它已启用，得留着让用户能取消勾选。无 session / 枚举失败 / 该 bundle 绑了别名 folder（远端枚举只覆盖 `bundle/*`）时标 `RemoteUnverified`，展示「未核对远端」。「远端确实没有」与「这次没问过远端」必须分开：混同会让候选谎报「Bitwarden 已有同名 secrets」，并诱导用户勾出一个拉不到内容的空 user bundle。

## 4. 模块分层

```text
cmd/
  root.go              # 入口路由、dec --version
  freshness_check.go   # hidden __freshness-check

internal/serviceapi/        # TUI / MCP 共用的服务客户端
internal/servicehost/       # dec-server gRPC 与 project 操作协调
internal/app/               # 服务端用例层
  bundle_writer.go     # ADR 0014：Bundle 唯一写入门面
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

工作区目录 basename 与 vault 中 `projects/<basename>.yaml` 同名时，Home 初始化 **自动应用** vault project，写入本地 `project_name` 与 `enabled_bundles`，无需手工复制 bundle 列表。详见 [ARCHITECTURE.md — Project 初始化](./ARCHITECTURE.md#project-初始化tuifirst)。**仅项目平面**：`dec --user` 没有工作区目录，不做这一步（[0015](decisions/0015-project-config-boundary.md)）。

### 5.3 Run 页 pull

1. 解析 `project_name` → vault `projects/<name>.yaml` → bundles 列表（或本地 `enabled_bundles`）
2. 逐 bundle 拉 Dec Git → `.dec/cache/<bundle>/`
3. 自动拉 Bitwarden secrets bundle → Secure Note 项目根相对路径；SSH Key → `~/.ssh/`
4. 零重叠校验 → 从 cache 渲染 IDE + 非敏感 vars 占位符替换
5. **孤儿 reconcile**：仅对本次启用且远端对照成功的 SyncTarget / vault 目标集清理本地孤儿；无法确认则只报告（见 [0010](decisions/0010-pull-orphan-and-ops.md)）

缺 Bitwarden session 时由 `dec-server` 自动触发 web unlock（服务进程内存 session，见 [BUNDLE-SECRETS-MODEL.md](./BUNDLE-SECRETS-MODEL.md#bitwarden-认证)）。

**结果区必须解释「零结果」**。「请求 0 · 成功 0 · 失败 0」自身不说明任何事情，用户无法区分「没启用」「启用的 bundle 已从仓库删除」「资产被过滤」。因此：

- `PulledCount == 0` 时结果区显式渲染 `SkippedReason`，而不只依赖事件流；
- 「`enabled_bundles` 引用的 bundle 在仓库中已不存在」这类判断在解析阶段就有结论，必须进 `PullProjectAssetsResult`（`MissingBundles` + `NonFatalWarnings`）而非只发一条 reporter 事件——事件区只保留最近十余条，开头的告警会被后续 secrets 事件挤掉，正是用户看到一排 0 却无从解释的原因。

同理，Bundles 页保存后 `RejectedBundles` 逐条显示（[0013](decisions/0013-secrets-belong-to-declared-target.md) §7a）。凡是「结构化结果里已有结论」的信息，都不靠会滚走的事件区承载。

### 5.4 Project 页变量编辑

- 按 `e` 通过 `tea.ExecProcess` 挂起 TUI、拉起外部编辑器编辑 `.dec/vars.yaml`
- **禁止**在 TUI 内直接调用 `editor.Open`（会与 Bubble Tea 持有的 TTY 冲突）
- `.dec/cache/` 不存在时显示提示，占位符扫描依赖 prior pull

### 5.4b Settings 本机 vars / 服务重启

- 本机用户级 vars（`~/.dec/local/vars.yaml`）同样按 `e` 外部编辑
- 门面与 `dec-server` 版本不一致时提示；Settings 可确认重启服务（清空进程内 Bitwarden session）
- Settings 连接 HTTPS 私仓若明确认证失败，进入 [0011](decisions/0011-private-repo-gcm-bootstrap.md)
  确认态：`y` 后以流式 operation 解锁/扫描 Bitwarden，候选只展示 folder、`.gcm/*`
  路径和 username；用户选择后 Apply + 验证并自动重试原保存。`n/Esc` 不读取/应用凭证。
- Run 页 pull 时若 `git fetch` 明确 HTTPS 认证失败（含凭证过期），同样进入上述确认态；
  Apply 成功后自动重试 pull。Bitwarden 尚无匹配 Note 时，先到 Remote 页登记 `.gcm/*`
  （登记不依赖私仓可达），再回到确认步骤。
- Repo Bootstrap 的 web unlock URL 必须随 operation event 实时回传；禁止改成 unary 后在响应末尾一次性返回。

### 5.4c Remote 页要点

- 进入默认拉全量远端库存（`ListRemoteInventory`）；`r` 强制刷新
- 远端分区不做任何平面 / 启用过滤：`[bundle]` 整包项跟 vault 声明走，本地分区跟 `cache/` 目录实况走
- 初次进页只展开到 bundle 这一层（Dec 侧 `cache/<bundle>`、Secrets 侧 folder 分组），子项默认折叠（`TreeNode.CollapseDefault`）
- `e`：Secure Note / SSH Hosts → temp → 写回远端（不种本地）
- `n`：光标所在 folder 登记；`N`：手输新 folder（**仅** `bundle/<名>`，ADR 0014）。Processor 同级（`note` / `.env` / `.gcm` / `.sshkey`），来源由类型声明（temp/路径/系统选文件/本机生成）；不枚举远端 folder 候选。folder 不存在时由登记本身按需创建
- `a` 全选 / `A` 全不选
- 列表态 `q` / `ctrl+c` 退出 TUI；表单 / 确认态 `Esc` 先退回列表
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
