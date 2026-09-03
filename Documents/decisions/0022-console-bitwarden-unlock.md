# 0022 — Console 统一承载 Bitwarden 人工解锁

- **状态**：已接受
- **日期**：2026-09-03
- **关联**：[0008](0008-service-facade-split.md)、[0018](0018-instance-lock-and-console.md)、[0020](0020-retire-tui.md)
- **部分取代**：[0008](0008-service-facade-split.md) 的服务弹出浏览器解锁；补充 [0018](0018-instance-lock-and-console.md) 的自动唤起行为
- **影响范围**：Console `Authenticate`、MCP 缺 session 协调、远端/CI 错误契约、认证内存边界

## 问题

旧流程让 `dec-server` 在需要 Bitwarden session 时启动临时 HTTP 页面并打开系统浏览器。这与
Console 已成为唯一人机入口的架构冲突，也无法可靠区分本机桌面、SSH 远端、无桌面主机、
CI 和测试。服务端自行弹窗还会把窗口生命周期、聚焦、并发等待与测试防护分散到业务层。

实例控制权与 Bitwarden vault session 继续遵循 0018 的同一启动门闩：锁定态只允许
`Ping`、`Authenticate` 及明确的置备白名单。本决策只替换人工认证 UI，并允许持有本机
listen token 的 MCP 请求在进入业务逻辑前唤起 Console、等待同一个门闩打开。

## 决策

### 1. Console Authenticate 是唯一人工入口

所有需要人输入主密码、TOTP 或确认设备登录的认证 UI 都由 Dec Console 展示。
`dec-server` 不启动认证网页、不打开系统浏览器，也不在终端收集认证材料。

Console 通过 `Authenticate` 驱动认证状态机，并可在同一页面继续提交主密码或 TOTP。
认证成功后，服务继续原操作或唤醒等待同一认证结果的调用。

### 2. 本机交互 MCP 自动协调 Console

本机桌面交互会话中的 `dec-mcp` 遇到缺失或过期 session 时：

1. 请求系统拉起尚未运行的 Console，或聚焦已运行的 Console；
2. 只发送固定的 `dec://unlock/local` 意图，不携带凭据、token、项目路径或业务参数；
3. 等待认证完成、用户取消或超时；
4. 成功后透明重试原操作；取消和超时返回结构化错误。

并发请求共享同一认证协调，不得重复打开窗口或创建多套认证中间态。MCP 不接收主密码、
TOTP、vault key 或 session 明文。门面通过 `x-dec-interactive-auth` 显式声明交互能力，
服务还必须验证请求使用本机 listen token；CI、测试与禁用环境始终发送非交互声明。

### 3. 远端无桌面与 CI 不拉起 Console

远端无桌面主机、CI、测试及明确的非交互上下文缺 session 时，立即返回结构化错误，至少
以稳定错误前缀区分 `CONSOLE_UNLOCK_REQUIRED`、`CONSOLE_UNLOCK_UNAVAILABLE`、
`CONSOLE_UNLOCK_TIMEOUT` 与 `CONSOLE_UNLOCK_CANCELED`，并携带
可操作提示。它们不得尝试打开、聚焦或安装 Console，也不得无限等待人工输入。

管理远端设备时，人工认证仍发生在操作者当前使用的 Console；Console 将认证输入提交给
目标 `dec-server`。目标主机本身不需要桌面环境。

### 4. 保留程序化认证

`DEC_BW_PASSWORD` 保留给开发、Agent 和受控自动化。它只从首次拉起目标 `dec-server`
的进程环境进入程序化认证路径，不经 RPC 传给已运行服务，也不得写入代码、配置、日志或
磁盘。缺少该变量或仍需 2FA 时，交互上下文转由 Console；非交互上下文返回结构化错误。

CI 默认继续使用平台 Secrets 中的业务变量，不把 Bitwarden 主密码、session 或 vault key
作为流水线凭据。

### 5. 敏感状态只在内存

Bitwarden session、vault/user key、主密码、TOTP，以及认证过程中的临时密钥、challenge
和 2FA 中间态都只存在于相关进程内存，完成、取消、超时或进程退出后清除。禁止写入
`server.json`、`connections.json`、环境缓存、日志、operation 结果或任何状态文件。

允许落盘的设备信任材料仍仅限 `deviceIdentifier` 与 `two_factor_remember`；它们不是
session 或 vault key。用户明确选择保存主密码时，只能使用 Console 的系统凭据库接口。

### 6. 测试不得自动弹 Console

测试环境和测试启动的子进程必须禁用 Console 自动拉起/聚焦。单元测试通过注入协调器桩
覆盖成功、取消、超时和不可交互分支；集成测试若使用 `DEC_BW_PASSWORD`，凭据只注入测试
进程环境。任何“测试中允许真实弹 Console”的全局开关都不得新增。

## 理由

- 唯一人工入口与 Console-first 一致，认证体验、窗口生命周期和错误呈现只有一套。
- 服务与 MCP 保持薄门面边界，敏感输入不在多个 UI/HTTP 通道间扩散。
- 本机 Agent 可在需要时等待人完成认证；远端与 CI 则得到稳定、可判定的失败契约。
- 继续保持实例控制权与 vault session 的同一内存门闩，不引入第二套登录状态。

## 被否方案

**A. 保留服务端临时网页作为 Console 的回退。**  
否决：形成第二个人工入口，继续带来弹窗归属、聚焦、测试和远端语义分裂。

**B. MCP 在终端或协议参数中收集主密码/TOTP。**  
否决：扩大敏感材料经过的进程和日志面，也违反 Console 唯一人机入口。

**C. 远端服务自行寻找操作者桌面或转发解锁 URL。**  
否决：远端可能无桌面，URL 转发重新引入临时认证站点；认证应由当前 Console 明确提交。

**D. 将 session、vault key 或 2FA 中间态落盘以跨重启续用。**  
否决：扩大整库解密能力的持久化攻击面；重启后重新认证是明确的安全边界。

**E. 测试自动拉起真实 Console。**  
否决：测试会挂起并污染开发桌面，且无法稳定覆盖取消、超时与非交互分支。

