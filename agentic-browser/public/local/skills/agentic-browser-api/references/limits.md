> 本文件为人工维护内容。

# 限制与不支持项

设计自动化方案前先读这一节，避免基于错误的假设开工。

## Strategy 不可用的能力

Strategy 跑在页面隔离 world 中，不是 Node.js 环境。以下能力全部
不可用：

- Node.js 内置模块，例如 `fs`、`path`、`child_process`，无法
  `require` 或 `import`。
- 文件系统读写，没有任何形式的直接访问。
- shell 命令执行。
- 原始 IPC，无法直接与主进程通信，只能通过 `agenticStrategy` 提供的
  成员。
- 跨 Origin 的任意请求，必须命中 manifest 的 `networkMatches`。

需要文件或系统能力时，只能通过 `agenticStrategy.emit` 发事件，由主进程
侧处理。

## 进程外接口不存在

当前没有任何形式的进程外编程接口：没有 HTTP 服务、没有 CLI 子命令、
没有 MCP 端点。

`src/main/phase0-probe-server.ts` 是本地探测服务器，仅用于 smoke 测试，
不属于产品 API，不要依赖它做集成。

## 权限声明的真实含义

`dom.read`、`dom.write`、`dom.click` 只用于声明与审计，不构成强制
隔离。

不要把它们当作安全边界。真正在调用时被检查的权限只有 `log`、
`project.emit-event`、`network.request`、`main-world` 四项，外加决定
配置是否下发的 `config.read`。

## 热重载会丢失页面状态

保存 Strategy、启停 Strategy、修改 allowlist，都会重建
`WebContentsView` 并刷新页面。

页面内瞬时状态不会保留，包括滚动位置、未提交的表单、页面内变量。需要
跨重载保持的状态必须由脚本在启动时重新建立，或落到主进程侧。

## 阻塞会导致隔离

Strategy 阻塞 renderer，会触发以下连锁反应，且不可逆：

- 该 Strategy 的 id 被加入 `quarantinedStrategyIds`。
- renderer 被强制终止。
- 页面由 `RecoveryEngine` 重建。
- 新页面不再注入被隔离的 Strategy。

恢复的唯一方式是重新保存或重启运行时。因此脚本里不要写同步长耗时逻辑，
不要在事件回调里做重计算。

## 心跳与超时的硬约束

心跳间隔超过 `heartbeatMs` 的两倍即判定失败，启动阶段超过 `startupMs`
同样失败。

这两个阈值来自 manifest 的 `timeouts`，取值见
[api-reference.md](api-reference.md) 的限额常量一节。脚本里的长任务
必须分段并让出事件循环，否则会被误判为阻塞。

## 宿主调用的体积限制

`agenticStrategy` 的宿主调用 payload 有字节上限，超出会被拒绝，调用
直接失败。

日志消息长度另有更小的上限，超长内容会被截断。具体数值见
[api-reference.md](api-reference.md) 的限额常量一节。

`evaluateMainWorld` 的源码长度也受限制，且需要 `main-world` 权限，
未声明会抛错。

## 其他边界

- `onBrowserEvent` 返回的取消订阅函数必须调用，否则监听器持续累积。
- 事件流不保证持久化，重启后 `sequence` 重新计数，历史事件不可回放。
- 工作区变更以返回的快照为准，不要假设本地缓存仍然有效。

## 与其他文档的分工

本文件只记录能力边界，不收录具体写法。可照抄的组合写法见
[cookbook.md](cookbook.md)，遇到失败时的排查路径见
[troubleshooting.md](troubleshooting.md)。
