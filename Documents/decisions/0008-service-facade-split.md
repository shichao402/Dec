# 0008 — Dec 服务 / 门面拆分

- **状态**：已接受（已实现）
- **日期**：2026-08-13
- **关联**：[0002](0002-secrets-synctarget-root.md)、[0003](0003-user-enabled-secret-bundles.md)、[0007](0007-machine-secrets-root.md)；[BUNDLE-SECRETS-MODEL.md](../BUNDLE-SECRETS-MODEL.md)；[TUI_ARCHITECTURE.md](../TUI_ARCHITECTURE.md)
- **部分取代**：[0022](0022-console-bitwarden-unlock.md) 已取代服务自行打开认证页面的叙事；服务/session 归属与门面拆分仍有效
- **影响范围**：进程模型、入口二进制、`internal/app` 调用方、Bitwarden session 归属、内置 MCP 启动命令、构建/安装/更新

## 问题

改造前为**单二进制多入口**（TUI / `dec mcp` / `dec exec`），业务与 Bitwarden session 都在**各自进程内存**里：

1. TUI 与 IDE 拉起的 `dec mcp` **无法共享 session**，Agent 侧常要重复解锁或依赖 `DEC_BW_PASSWORD`
2. MCP 注释强制「同进程直调 `internal/app`」才能复用 session，与「多门面」天然冲突
3. 机器级 secrets、多工作区、`dec exec` 注入路径让「谁持有权威状态」越来越难靠单进程约定维持

需要明确：**一机一个服务**持有权威状态与 session；TUI / MCP 等只做门面。

## 决策

### 进程角色

| 程序 | 角色 | 用户面？ |
|------|------|----------|
| **`dec-server`** | 本机单例服务：session、程序化认证、pull/push、资产与 secrets 权威编排 | 否（由门面自动拉起） |
| **`dec`** | TUI 门面（保持无参打开 TUI） | 是 |
| **`dec-mcp`** | Agent MCP 门面（stdio MCP → 调服务） | 否（IDE 配置） |
| **`dec-exec`** | 本地 env 注入 shim | 否（hidden / MCP `command`） |

仍遵守 TUI-first：**不新增**用户面 Cobra 子命令（无 `dec unlock` / `dec pull` / `dec daemon` 等）。用户只接触 `dec`（TUI）与 `--version` 等最小 CLI。

### 一机一服务 + 自动拉起

- 每台设备只应有一个 `dec-server` 实例（单例锁）。
- 门面（TUI / MCP）启动时：连不上服务 → **自动拉起** `dec-server` → 等待就绪 → 再连。
- **不做降级**：禁止「无服务时门面内嵌 `internal/app` / 嵌入式单次模式」双路径。CI / Agent / 脚本与日常用户走同一条「有服务才干活」路径。

### 生命周期与 session

- Bitwarden session / userKey **仅存 `dec-server` 进程内存**；禁止落盘（延续既有硬约束）。
- **Session 失效边界 = 服务进程退出**（不是某个门面退出）。
- **空闲退出定义**：当前 **门面连接数 = 0** 起算，连续空闲超过配置时长 → 服务退出并清掉内存 session。  
  - 有任一 TUI / MCP 连接（即使无 RPC）都不算空闲。  
  - **默认 30 分钟**；用户可在 TUI **Settings** 配置，写入本机配置文件 `~/.dec/config.yaml`（字段 `server_idle_timeout`）。  
  - **不用环境变量覆盖**；测试通过改配置文件或注入配置，不引入 `DEC_SERVER_IDLE_TIMEOUT` 一类旁路。  
  - 下次门面拉起服务后需重新认证（Console 人工认证或 `DEC_BW_PASSWORD` 程序化认证）。
- 人工认证由 **Console** 独占；服务不打开页面或收集终端输入。本机交互 MCP 缺 session 时拉起/聚焦 Console 并等待，非交互环境返回结构化错误（[0022](0022-console-bitwarden-unlock.md)）。

### `dec-exec`（本期边界）

- **独立程序**；**暂时不经过服务、不碰 session**。
- 语义保持现状：只读已落地的 `env/*.env`（机器默认 → 项目 bundle → 项目 env，见 0007），合并后 exec 子进程。
- 日后若要把「缺 env 时先 pull」并进 exec，另开决策；**不在本 ADR 实施范围内默认做**。

### IPC（跨平台写死）

目标环境：**macOS + Linux + Windows** 同一套语义，禁止「按 OS 分叉两套协议」。

| 项 | 决定 |
|----|------|
| 传输 | **`127.0.0.1` TCP**（只绑 loopback，禁止 `0.0.0.0` / 非本机网卡） |
| 鉴权 | 启动时生成随机 **token**；门面每个连接必须携带；token **不**等于 Bitwarden session |
| 发现 | `~/.dec/run/`（或 `DEC_HOME/run/`）写入 `endpoint`（如 `127.0.0.1:<port>`）与 `token`；文件权限尽量收紧（Unix `0600`；Windows 仅当前用户） |
| 单例 | 同目录下进程锁 / pid；第二实例发现已有存活端点则退出并让门面改连已有实例 |
| 协议 | 首版用 **gRPC**（unary + server-streaming）；三端同一 `.proto` |

理由：loopback + token 在三平台是**同一代码路径**。Unix socket / named pipe 在 Windows 与 Unix 上 API 与路径约定分裂，首版不做。

运行时文件**不得**写入 session / 主密码 / userKey。

### 进度回传 + 同 project 互斥（首版就做）

服务按 **project 作用域** 维护「当前活跃操作」（同一时刻每 project 至多一个 pull/push/preview/delete 写操作）：

1. **互斥**：门面发起写操作时，服务对该 project 加锁。若已有活跃操作 → 返回 **busy**，并附带「谁在做什么」（操作类型 + 发起门面标识 + 起始时间）。
2. **发起方进度**：发起方在**同一次调用**里收到进度流（gRPC server-stream），结束给结果。
3. **旁观方进度（本版新增）**：另一门面可对该 project **watch** 当前活跃操作，收到同一份进度（如 TUI 显示「MCP 正在 pull…」并跟随进度条）。watch 是**只读旁观**，不发起、不改状态。

| 做法 | 含义 | 首版？ |
|------|------|--------|
| **跟操作走的进度流** | 发起方一次调用内收进度 + 结果 | **采用** |
| **按 project watch 活跃操作** | 旁观门面订阅「该 project 当前这一个操作」的进度 | **采用（本版新增）** |
| **全局事件总线** | 一条连接订阅全机所有 project 所有事件 | **不做**（仍过度） |

短读（状态、列表、session 是否就绪）一次请求一次响应即可，不必开进度流。  
TUI 切页仍不取消在飞的那次 `Pull`（延续 async_io）。旁观流的生命周期跟「该活跃操作」走：操作结束 → 旁观流收到终结事件并关闭。

### 职责划分

| 能力 | 归属 |
|------|------|
| Bitwarden session 与程序化认证 | 服务；人工认证 UI 归 Console |
| pull / push / preview / delete / Remote 元数据 | 服务 |
| project 概览、connect repo、init、bundles 启用 | 服务 |
| 操作进度 / 日志 | **跟这次操作走的进度流** → 发起方门面展示 |
| TUI 页面、快捷键、外部编辑 `tea.ExecProcess` | TUI 门面 |
| MCP tool 注册与 JSON 响应整形 | MCP 门面 |
| 读本地 `.secrets/**/env` 并注入子进程 | `dec-exec`（本地） |
| 服务空闲超时 | 本机配置文件；TUI Settings 编辑 |

### RPC 面（首版清单）

门面 → 服务的最小方法集（命名可在实施时微调，语义固定）：

| 方法 | 形态 | 对应现状 |
|------|------|----------|
| `Ping` / `GetServerInfo` | unary | 就绪探测、版本 |
| `Shutdown` | unary | 更新/生命周期控制；仅持有本机 token 的门面可调用 |
| `GetStatus` | unary | `dec_status` / Home 概览（需 `project_root`） |
| `ConnectRepo` | unary | `dec_connect_repo` |
| `InitProject` | unary | `dec_init_project` |
| `ListAssets` / `SetAssets` | unary | `dec_list_assets` / `dec_set_assets` |
| `Pull` / `Push` / `PreviewPush` | **带进度流** | Run 页 / 同名 MCP：同一次调用里陆续推进度，最后给结果；project 忙时返回 busy |
| `GetActiveOperation` | unary | 查某 project 是否有活跃操作及其元信息（类型/发起方/起始时间） |
| `WatchOperation` | **server-stream** | 旁观某 project 的活跃操作进度（TUI 显示「MCP 正在 pull…」） |
| `ListSecrets` / `ListDeleteCandidates` | unary | 同名 MCP / Remote |
| `Delete` | unary；若要逐步进度则同 Pull 用进度流 | 同名 MCP / Remote |
| `EnsureSession` / `SessionStatus` | unary | 请求 session；**不**向门面回传 session 明文或主密码；人工认证由 Console 协调 |
| （配置）读写含 `server_idle_timeout` 的本机设置 | unary | Settings；与现有全局配置保存同一路径 |

所有项目相关调用带 **`project_root`（或等价作用域）**；机器平面操作（如 user-enabled bundles）明确标为 machine scope。

**首版不进 RPC**：`dec-exec` 合并与子进程启动；全局事件总线（跨 project 广播）。

## 理由

- 一机一服务解决多门面 session 与权威状态分裂，且仍满足「session 不落盘」
- 三（四）个独立程序强制契约外置，避免再把内核拷回门面
- 自动拉起 + 不做降级，保证单一运行路径与测试面
- 空闲退出在「常驻占资源」与「session 复用」之间折中：有门面连接则复用 session；无人连超过配置时长则释放；时长由用户在 Settings 配置并落盘
- **loopback TCP + token + gRPC** 换三平台单一实现，避免 socket/pipe 双栈
- **进度跟操作走 + 按 project 旁观**：发起方走同调用进度流；旁观方 watch 该 project 活跃操作，满足「TUI 显示 MCP 正在 pull」；仍不做跨 project 全局总线
- `dec-exec` 暂留本地：不阻塞拆分主路径，且与「只读已落地 env」现状一致

## 被否方案

**A. 继续单二进制多入口、进程内 session。**  
否决：TUI 与 MCP 无法共享 session；Agent 体验持续恶化。

**B. 无服务时门面嵌入式降级（内嵌 app）。**  
否决：双路径心智与测试成本；已明确不做降级。

**C. 服务常驻到关机/显式退出，空闲不退。**  
否决：本期选择空闲退出以省资源；接受 session 随服务退出失效。

**D. `dec-exec` 首版即强制经服务。**  
否决：增加拆分耦合；exec 当前不需要 session；暂维持本地 shim。

**E. 新增用户面 `dec unlock` / `dec daemon` 子命令。**  
否决：违反唯一人机入口；人工认证按需由 Console 承载，服务由门面自动拉起。

**F. Session 落盘或写 `BW_SESSION` 文件以便跨进程。**  
否决：硬约束禁止；改由服务进程内存承载即可跨门面共享。

**G. Unix domain socket（macOS/Linux）+ Windows named pipe 双传输。**  
否决：三平台要维护两套监听/拨号/路径/权限与测试矩阵；个人单机威胁模型下 loopback + token 足够。

**H. 首版做「全局事件总线」（跨 project 把所有操作广播给所有门面）。**  
否决：首版只需**按 project 旁观当前活跃操作**（`WatchOperation`），满足「TUI 看到 MCP 正在 pull」。全机广播的扇出/权限/生命周期复杂度无首版刚需，留待以后。

**I. 空闲按「无 RPC 活动」而非「无连接」计时。**  
否决：TUI/MCP 常驻连接但长时间不操作时不应踢掉服务（否则等于工作时段反复丢 session）；以**连接数归零**起算更符合「还有门面在用」。

**J. 用环境变量覆盖空闲超时。**  
否决：与「Settings 落盘、用户可配」分叉；测试与运维改配置文件即可。

## 实施分期（本 ADR 接受后）

| 期 | 内容 | 本期？ |
|----|------|--------|
| 0 | 本决策文档 + 索引 | **已完成** |
| 1 | `dec-server` 单例 + gRPC + 按 project 活跃操作锁 / busy / WatchOperation；门面自动拉起/连接 | **已完成** |
| 2 | 将 `internal/app` 权威调用迁到服务；MCP/TUI 变薄客户端 | **已完成** |
| 3 | 内置 MCP 模板改为 `dec-mcp`；文档与规则改平 | **已完成** |
| 4 | 四程序跨平台构建、安装与 RUP component 更新 | **已完成** |

## 文档与规则改平

已将表述「session 在 TUI/Dec 进程」改为「session 在 `dec-server` 进程」：

- `Documents/BUNDLE-SECRETS-MODEL.md`
- `Documents/ARCHITECTURE.md` / `TUI_ARCHITECTURE.md`
- `.cursor/rules/bitwarden-auth.mdc`、`tui-first.mdc`

## 参考

- 现状入口：`cmd/root.go`、`cmd/mcp.go`、`cmd/exec.go`
- 进程内 session：`internal/secrets/session.go`
- MCP 同进程约束注释：`internal/mcp/server.go`
