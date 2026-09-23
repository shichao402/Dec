# 0030 — Agent 工具清单与编排下移，dec-mcp 零业务知识

> **部分已被 [0032](0032-mcp-streamable-http.md) 取代**：stdio 壳、`dec-mcp` 读清单，以及 Console `POST /agent/tool_call` 转发不再使用。工具声明、`jsonschema` 与 `Plan` 仍在 `internal/agenttools`，这一段继续有效。

- **状态**：清单与 Plan 仍有效；壳与网关已被 0032 取代
- **日期**：2026-09-17
- **关联**：[0021](0021-console-owned-runtime.md)、[0025](0025-mcp-console-gateway.md)、[0023](0023-facade-capability-parity.md)、[0008](0008-service-facade-split.md)
- **影响范围**：`internal/agenttools/`、`cmd/dec-server --dump-agent-tools`、`plan_agent_tool` RPC、`client/src-tauri` Agent 网关 `POST /agent/tool_call`、`internal/mcp/` stdio 壳、`~/.dec/run/agent-tools.json`

## 问题

`dec-mcp` 把 21 个工具的名字、描述、JSON schema 与调用编排全部编译进二进制。服务端改一次 RPC 名（例如 ADR 0029 的 `set_requires`），旧壳仍拿着过期 schema 静默打错方法；实测 IDE 不会自动重拉退出的 stdio 子进程，Console「杀掉旧 MCP」只会把静默出错变成直接不可用。ADR 0025 已写明壳只适配 stdio，现状是它还兼任半个业务门面。

## 决策

### 1. 清单唯一真相源在 Go

新包 `internal/agenttools` 持有全部工具声明、`jsonschema` 推导与 `Plan(name, args)` 编排。`dec-server --dump-agent-tools` 只打印 JSON，不起服务。Console 在对齐 `~/.dec/bin` 后原子写出 `~/.dec/run/agent-tools.json`（0700/0600 + rename），与运行时套件同源同版本。

### 2. 壳只读清单、原样转发

`dec-mcp` 启动读清单，用 SDK `AddTool` / `RemoveTools` 动态注册（自动 `notifications/tools/list_changed`）。`tools/call` 一律 `POST /agent/tool_call {name, arguments}`，不解析业务字段。清单缺失时只注册 bootstrap 工具 `dec_console_status`；调用它会拉起 Console 并写出完整清单，响应带回 `manifest_version`，壳发现变化则重载。

### 3. 服务端给计划，Console 按步执行

只读 invoke `plan_agent_tool` 返回步骤表（`invoke` / `run` / `console_active_operation` / `error`）与组装形态（`single` / `planes` / `keyed`）。写类工具逐步走 `RunOperation`，保住 broker 按 project 写互斥与 `WatchOperation` 旁观（0025 §4）。`plane=both` 拆成两次独立步骤，不得塞进一个 unary。

### 4. owner 区分归属

`dec_console_status` / `dec_list_connections` / `dec_connect` 标 `owner=console`，未连接也可；其余 `owner=server` 经计划执行。

### 5. 版本门闩

壳只校验清单 `protocol`；与 Console SemVer 严格相等仍只约束 Console ↔ `dec-server`（0021）。清单 `version` 形如 `v1.13.73#a1b2c3d4`（SemVer + 工具表内容摘要），同版本改 schema 也会变摘要。`/agent/tool_call` 回带磁盘上的 `manifest_version`，壳不一致则重载。壳不参与运行时自升级。

## 被否方案

- Console 杀掉旧 `dec-mcp` 指望 IDE 重拉：实测不会重拉。
- 壳自己执行 `dec-server --dump-agent-tools`：壳开始碰运行时二进制，违反 0021 所有权。
- 统一 unary 代执行写操作：丢掉 busy 互斥与事件流。
- 清单放在网关在线拉取且无本地缓存：冷启动被迫拉起 Console。
