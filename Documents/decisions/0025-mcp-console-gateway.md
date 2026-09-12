# 0025 — MCP 经唯一 Console 网关代理，不再直连 dec-server

- **状态**：已接受（已实现）
- **日期**：2026-09-12
- **关联**：[0008](0008-service-facade-split.md)（服务 / 门面）、[0018](0018-instance-lock-and-console.md)（Console 单例）、[0021](0021-console-owned-runtime.md)（运行时所有权）、[0022](0022-console-bitwarden-unlock.md)（认证只在 Console）、[0023](0023-facade-capability-parity.md)（门面能力口径）
- **影响范围**：`client/src-tauri/`（loopback Agent 网关）、`internal/mcp/`、`internal/consoleopen/`、内置 `mcp.json` 模板、门面文档

## 问题

`dec-mcp` 按 IDE 工作区拉起，进程级绑定 `--project-root`（常为未展开的 `${workspaceFolder}`），并自行发现 / 拉起 / 重启本机 `dec-server`。这和当前产品模型冲突：

1. **常驻没有意义。** 保活已经由 Console 的 KeepAlive 承担；MCP 长驻既拖不住正确的目标，又留下 ACP 孤儿进程。
2. **项目绑定会随机漂移。** Agent 需要操作 Console 已登记的受管项目，而不是猜当前 cwd。
3. **SSH 隧道只活在 Console 里。** MCP 直连本机服务用不上 Console 当前连的远端。
4. **多 MCP 打同一个 Console 过去做不到。** Console 没有入站 Agent API；多进程只能打 `dec-server`，能力与当前连接都对不齐。

## 决策

### 1. Console 是 Agent 的唯一后端入口

`dec-mcp` 不再读 `server.json`、不再拉起或重启 `dec-server`。它只发现本机唯一 Console 的 loopback 网关（`$DEC_HOME/run/console.json`），把 tool 调用转到 Console **当前连接**的目标（本机或已建 SSH 隧道的远端）。

连不上网关时：本机交互环境拉起或聚焦 Console 并等待就绪；CI / 测试 / `DEC_NO_CONSOLE_LAUNCH=1` 返回结构化错误。运行时安装、版本对齐、拉起 `dec-server` 仍只由 Console 按 [0021](0021-console-owned-runtime.md) 执行。

### 2. 发现与鉴权

| 项 | 决定 |
|----|------|
| 传输 | 仅 `127.0.0.1` TCP（禁止 `0.0.0.0`） |
| 鉴权 | 启动时随机 token；请求带 `Authorization: Bearer` |
| 发现 | `console.json`：`endpoint`、`token`、`pid`；权限与 `server.json` 同策略 |
| 单例 | 沿用 `tauri-plugin-single-instance`；第二次启动不另开端口，只聚焦 |

网关转发 gRPC 时 metadata 使用 `facade=mcp` 与调用方 `client-id`，操作记录不得全部显示成 Console UI。

### 3. 无进程级项目绑定

MCP stdio 进程不绑 `project_root`。每个 tool 自带 `project_root` 与 `plane`。`plane=local` 且 root 为空时失败，并提示先列出受管项目；`plane=global` 的 root 必须为空（[0015](0015-project-config-boundary.md)）。

内置 IDE 配置改为用户级一条 `dec-mcp`，不再写 `--project-root ${workspaceFolder}`。

### 4. 多 MCP 共用一实例

多个 Cursor 窗口 / 多个 `dec-mcp` 进程打同一个 Console 网关是首版目标。写互斥仍在下游 `dec-server` 按 project（[0008](0008-service-facade-split.md)）；网关不得再加全局写队列。Console 的 `invoke` 不得在整段 RPC 期间握 session 锁，以免 Agent 与 UI 互相堵住。

同 project 第二个写操作仍返回 busy；Console UI 可 `WatchOperation` 旁观。

### 5. 能力与认证

Agent 工具映射 Console 已有的 invoke / run（含受管项目、创本地资产、列出/切换已存连接）。Authenticate 永不经网关收集主密码（[0022](0022-console-bitwarden-unlock.md)）。Console 自更新与全局 IDE/Bitwarden 设置不做 MCP 工具。`plane=both` 等自动化参数仍按 [0023](0023-facade-capability-parity.md) 登记。

## 理由

- Agent 与人看到同一台设备、同一份受管项目，SSH 隧道不必在 MCP 里再做一份。
- Console 已经是单例保活进程；MCP 只当 stdio 适配器，空闲可退。
- 下游并发模型已存在，缺的是 Console 入站口，而不是再做一个 Agent 专用服务。

## 被否方案

**A. MCP 仍直连本机 `dec-server`，只去掉 `--project-root`。**  
否决：Agent 用不上 Console 当前 SSH 连接，也不是「自由使用 Console」。

**B. 让 `dec-mcp` 自己建 SSH 隧道。**  
否决：隧道与当前目标会和 Console 分叉，违反单例管理。

**C. 第一期就让 Cursor 直连 Streamable HTTP、去掉 stdio。**  
否决：现有 IDE 配置仍是 stdio；网关协议做成可复用即可，stdio shim 覆盖当前宿主。

**D. MCP 版本不一致时继续 RestartServer。**  
否决：会打断 Console 与所有其它门面；升级权归 Console（[0021](0021-console-owned-runtime.md)）。
