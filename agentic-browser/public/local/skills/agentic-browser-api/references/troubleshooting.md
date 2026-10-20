> 本文件为人工维护内容。

# 排错指南

按症状查表。每条先给出最可能的原因，再给出确认手段。

## Strategy 没有被注入

依次确认三件事：manifest 的 `matches` 是否命中当前 URL、该 Strategy
是否已通过放行、运行时是否处于运行状态。

`matches` 支持通配，但不会匹配到不同 Origin。放行状态与运行时状态都可
从快照中读取。

## 注入后立即失败并显示 timeout

两条独立阈值：启动阶段受 `startupMs` 约束，运行阶段受 `heartbeatMs`
约束。

脚本在顶层做同步长耗时计算会直接触发启动超时。把长任务拆成多段，每段
之间让出事件循环。

具体数值见 [api-reference.md](api-reference.md) 的限额常量一节。

## Strategy 被隔离且不再注入

按现有实现，阻塞 renderer 会造成不可逆后果，该 Strategy 的 id 进入
隔离列表，新页面不再注入。

恢复方式是重新保存该 Strategy 或重启运行时。仅重新加载页面不会解除
隔离。

## 请求被 allowlist 拦截

要同时满足两个条件：manifest 声明了 `network.request` 权限，且目标
URL 命中 `networkMatches`。

只声明权限不声明匹配规则同样会被拒。匹配按 Origin 与路径前缀判断，
带端口的 URL 需要把端口写进匹配规则。

## 宿主调用报 payload 过大

`agenticStrategy` 的宿主调用有字节上限，超出后调用失败而不是截断。

需要传递较大数据时分批发送，或改为由主进程通过事件逐条拉取。日志有
更小的独立上限，超长会被截断，截断不会报错。

## 日志出现但事件没收到

事件监听按订阅关系分发，未订阅的类型不会出现在回调里。

确认订阅用的类型名与脚本里 `emit` 的类型名完全一致，且订阅在事件
发出之前就已建立。页面重建后需要重新订阅。

## 在 Strategy 里拿不到 window.agenticBrowser

这两个对象分属不同上下文。渲染进程主 world 才有
`window.agenticBrowser`，页面隔离 world 只有 `agenticStrategy`。

需要跨上下文通信时，用 `agenticStrategy.emit` 发事件，再由渲染进程
通过 `onBrowserEvent` 接收。

## 渲染进程 Console 里没有 agenticStrategy

同上，方向相反。`agenticStrategy` 只对 Strategy 脚本可见，属于设计
约束，不是权限问题。

## 保存后行为没有变化

保存或启停 Strategy 都会重建页面视图。如果没有重建，通常说明保存
没有真正落到该 Strategy，或运行时未在运行。

另一种情况是脚本命中了隔离，此时保存不会恢复，需要重启运行时。

## 继续补充的位置

新增条目时追加到本文件末尾，保持「症状标题 + 原因 + 确认手段」的
结构。只收录已被验证过的因果关系，未确认的猜测不要写入。
