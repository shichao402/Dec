> 本文件为人工维护内容。

# 核心概念

本文件解释数据模型与运行时语义。方法签名请查阅
[api-reference.md](api-reference.md)。

## Project 与 Strategy 的关系

Project 是自动化任务的编排单位，Strategy 是运行在页面里的脚本。二者是
组合关系，不是继承关系。

一个 Project 通过 `strategies` 字段以「id 加版本」的形式固定依赖哪些
Strategy，这组引用称为 pin。pin 的版本必须与 Strategy 仓库中的版本
一致，否则该项目不会运行，判定结果见 `ProjectStrategyResolution` 的
`status`：`ready`、`missing`、`version-mismatch`、`disabled`。

Strategy 独立存在、独立启停。它可以被多个 Project 复用，页面上是否注入
由 manifest 的 `matches` 与 URL 匹配决定，而不是由 Project 决定。

## Strategy 的生命周期

Strategy 的运行时状态由 `StrategyRuntimeStatus` 表达：`issued`、
`scheduled`、`starting`、`running`、`failed`。

状态推进靠脚本主动上报的信号，信号类型见 `StrategyRuntimeSignalType`。
脚本启动后先上报 `started`，进入运行期后定期上报 `heartbeat`，初始化
完成后上报 `ready`。

心跳是存活的唯一依据。超过 `heartbeatMs` 的两倍仍未收到心跳，运行时会
判定失败并上报 `strategy-heartbeat-missed`。启动阶段超过 `startupMs`
同理。这两个阈值来自 manifest 的 `timeouts`。

失败后该 Strategy 会被隔离，记录在 `quarantinedStrategyIds` 中，后续
页面不再注入，直到重新保存或重启。

## 权限模型

Strategy 的能力受 manifest 的 `permissions` 声明限制，取值见
`StrategyPermission`。

必须清楚一点：`dom.read`、`dom.write`、`dom.click` 属于声明与审计用途，
不构成强制隔离，不应把它们当作安全边界来设计。

真正会在调用时被检查的是另外几个：`log` 对应 `agenticStrategy.log`
系列方法，`project.emit-event` 对应 `agenticStrategy.emit`，
`network.request` 对应 `agenticStrategy.request`，`main-world` 对应
`agenticStrategy.evaluateMainWorld`。未声明就调用会抛错。

`config.read` 决定 `agenticStrategy.config` 是否有内容。未声明时该对象
为 `{}`。

具体哪个方法需要哪个权限，见 [api-reference.md](api-reference.md) 的
宿主方法所需权限一节。

## 网络请求的 allowlist

`agenticStrategy.request` 不是任意请求。请求的 URL 必须命中 manifest 的
`networkMatches` 中的某个模式，否则抛错，错误文案包含 `allowlist`
字样。

这意味着新增一个外部接口调用时，必须同时修改 manifest 的
`networkMatches`，只改脚本不会生效。

## Workflow 与失败分类

Workflow 是有向节点图，入口由 `entryNodeId` 指定。节点类型由可辨识联合
的 `kind` 字段区分，取值见 [api-reference.md](api-reference.md)。

每个节点带 `timeoutMs`、`maxAttempts`、`checkpoint` 三个控制字段。
`checkpoint` 为真时该节点完成后会落盘，运行时中断后可从该点恢复。

失败原因统一归为 `WorkflowFailureCategory` 的取值，例如 `network`、
`timeout`、`authentication`、`page-changed`、`renderer`、`disk`、
`cancelled`。节点可以按分类配置 `onFailure` 跳转，实现差异化重试。

## 下载的幂等语义

下载由 `agenticStrategy.emit` 发出的 `download.intent` 事件触发，返回
一个 reservation。

每次下载都带 `idempotencyKey`，同一 key 重复请求不会产生第二份文件。
下载记录状态见 `DownloadRecordStatus`，从 `reserved` 开始，经
`downloading` 到 `completed` 或 `failed`。

## 恢复与故障隔离

页面崩溃、无响应或 renderer 被终止时，`RecoveryEngine` 按
`RecoverySnapshot` 的 `status` 推进：`idle`、`scheduled`、
`recovering`、`circuit-open`。连续失败会在时间窗内累计，超过阈值进入
`circuit-open`，停止自动恢复。

Strategy 阻塞 renderer 会导致该 Strategy 被隔离、renderer 被强制终止、
页面被重建。新页面不会注入已被隔离的 Strategy。

## 热重载的副作用

保存 Strategy、启停 Strategy 或修改 allowlist，都会重建
`WebContentsView` 并刷新页面。

因此页面内的瞬时状态不会保留，包括滚动位置、表单未提交内容、页面内
变量。依赖这些状态的流程要在脚本里重新初始化，或者放到页面重建之后。

## 事件模型

`window.agenticBrowser.onBrowserEvent` 接收全部 `BrowserEvent`，事件类型
见 `BrowserEventType`。

每个事件带全局递增的 `sequence`、`timestamp`、`url`，部分事件带
`tabId`、`projectId`、`runtimeId`。可用 `sequence` 判断是否漏事件。

`onBrowserEvent` 返回取消订阅函数，务必在不需要时调用，否则监听器会
累积。
