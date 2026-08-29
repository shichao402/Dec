# 0018 — 实例锁定与管理客户端

- **状态**：已接受（已实现）
- **日期**：2026-08-28
- **关联**：[0008](0008-service-facade-split.md)
- **影响范围**：`dec-server` 启动门闩、`Authenticate` RPC、独立 Tauri 管理客户端

## 决策

`dec-server` 仍是一机单例。进程启动后 **全局锁定**：仅 `Ping` 与 `Authenticate` 可在未解锁时调用。人在管理客户端选择本机或远程实例，提交 Bitwarden 主密码（及可选 TOTP）；服务用该密码走 Identity 程序化解锁。成功则控制权与 BW session 同在进程内存、同为 1 小时 TTL；进程退出即失效。

`server.json` 的 token 只是本机 gRPC 传输密钥，不是所有权证明。远程会话使用 `Authenticate` 下发的 control token。非 loopback 监听必须配置 TLS。

管理 UI 是独立 Tauri 2 客户端（`client/`），不改 `dec` / TUI / MCP 交互。Node 工具一律子进程，不嵌入 Rust 运行时。

生产启动不因 `DEC_BW_PASSWORD` 自动进入已解锁；该变量仍可用于 `EnsureSession` 的程序化路径与测试。

## 被否方案

**A. 管理面板与 TUI 分两套锁。** 否决：只有一个 `dec-server`，锁定必须是进程状态。

**B. 用 `server.json` token 当作远程所有权。** 否决：远程拿不到该文件；本机读文件也不能证明主人身份。

**C. 把 UI embed 进 `dec-server` / 改 `dec` 为浏览器启动器。** 否决：客户端独立进程，本机与远程同一套连接模型。

