# 0032 — MCP 改为 dec-server 上的 Streamable HTTP

- **状态**：已接受
- **日期**：2026-09-23
- **关联**：[0008](0008-service-facade-split.md)、[0021](0021-console-owned-runtime.md)、[0023](0023-facade-capability-parity.md)、[0025](0025-mcp-console-gateway.md)、[0030](0030-agent-tools-thin-shell.md)
- **影响范围**：`dec-server` 监听、IDE 内置 `dec` MCP 条目、运行时套件、Console Agent 网关

## 问题

`dec-mcp` 是 IDE 拉起的 stdio 进程。覆盖安装只换掉磁盘上的文件，Windows 不会重载已经映射进进程的映像。给 `mcp.json` 写 `DEC_RUNTIME_GENERATION` 只会再起一个进程，旧会话继续跑旧二进制。stdio 壳无法保证 Cursor 用的是当前这一份运行时。

ADR 0025 把 MCP 放在 Console 网关后面，是为了让 Agent 跟着 Console 当前的 SSH 目标。那条网关同时把冷启动和工具执行绑在一个会换端口的 loopback 上。

## 决策

### 1. 去掉 dec-mcp

IDE 内置 `dec` 条目写固定 URL `http://127.0.0.1:47654/mcp`。`dec-server` 在该地址提供 Streamable HTTP，与 Console 的 gRPC 同级，都进同一套业务。运行时套件不再包含 `dec-mcp`。Console 的 `/agent/tool_call` 与 `console.json` 发现一并删除。

冷启动不由 MCP 负责。URL 后面没有进程就是连接失败。打开 Console 仍是拉起并对齐这一个 `dec-server` 的路径。服务已在听时，缺主密码继续由 `dec-server` 打开 Console。

### 2. 两个固定 loopback 端口

gRPC 只听 `127.0.0.1:47653`，继续走 `grpc.Server.Serve`。MCP 只听 `127.0.0.1:47654`，普通 HTTP/1.1。MCP 端口不进配置。

`management_listen` 为空时使用 `127.0.0.1:47653`。写成别的地址则启动失败，不改去听那个地址，也不退回 `127.0.0.1:0`。任一端口被占用都把错误原样告诉用户，由用户自己腾出。已有单例锁仍表示服务已在跑：第二实例退出，Console 连已有实例。这不是换端口。

连远端时，隧道由 Console 建立，只转发 `47653`。本机 gRPC 不把业务转到远端。远端那台 `dec-server` 同样听 `47654`，只给那台机器上的 IDE。

### 3. 本机 Agent 只打本机

Cursor 固定打本机 `47654`。Console 连着远端时，本机 Agent 不到那台远端；远端操作走 Console 的 SSH 会话。`dec_list_connections` / `dec_connect` 不再切换 Agent 的目标。

### 4. 本机放行

MCP 只听 `127.0.0.1`。对端地址是 loopback 就放行，不看请求头。gRPC 仍用 `server.json` 里的 listen token：Console 读这份文件，IDE 不读。

同一用户的本机进程本来就能连上 `127.0.0.1:47654`，也能读到 `mcp.json`。把每次启动都换掉的 token 写进 IDE 配置，挡不住这些进程，却让 `mcp.json` 每次服务启动都变。

### 5. 工具执行

工具声明与 `Plan` 仍在 `internal/agenttools`（0030 的这一段保留）。Streamable HTTP 在进程内按计划执行 `invoke` / `run`，不再经 Console 网关转发。人用的能力仍必须在 Console 有入口（0023）。

## 被否方案

- **保留 stdio `dec-mcp`。** 已映射的进程不会跟着磁盘文件换。
- **端口被占就换一个。** 远端隧道和 IDE URL 都靠固定端口；换端口会变成两套地址。
- **双协议 SSE。** 一条长连接再加第二个 POST URL，新门面不需要。
- **MCP 仍挂在 Console 网关。** 网关换端口，且 Agent 被绑在 Console 进程上。
- **用 cmux 或 h2c 把两种协议塞进 `47653`。** 只少记一个地址，并让 Console 的 KeepAlive 跟 `http.Server` 超时绑在一起。当前 Cursor 只连本机 MCP，远端管理仍只隧道 `47653`。
- **本机 MCP 跟着 Console 的 SSH 当前目标。** 隧道活在 Console 里。Agent 再跟过去，就要在 `dec-server` 里复制一条会话，或把 Console 留在调用路径上。
- **把 listen token 写进 IDE 的 MCP 请求头。** token 每次启动都变，IDE 配置跟着变。对本机进程没有额外的隔离：能打到 loopback 的同一用户也能读到这份配置。
