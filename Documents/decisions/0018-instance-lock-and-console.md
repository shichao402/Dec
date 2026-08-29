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

## 管理边界与项目登记

一次 Console 连接的管理边界是一台 `dec-server` 所在设备。解锁后，Console 通过同一套 gRPC 服务管理该设备的 Global 平面、项目目录、资产选择和同步任务，不在客户端本地执行 `internal/app`。

设备的 Global 平面使用 `workspace_plane=global` 且 `project_root` 必须为空；项目平面使用 `workspace_plane=local` 与明确的项目根路径。协议仅兼容读取旧称 `user` / `project`，新客户端不再发送旧称。

受管项目采用“显式登记为主、手动范围扫描导入为辅”的模型：

- 路径选择器浏览的是目标服务器文件系统；
- 自动扫描默认关闭，只有用户选定扫描根后才查找 `.dec/config.yaml`；
- 登记信息保存在目标设备全局配置中，只作为 Console 管理入口；
- “移除管理”只删除登记，不删除项目目录、`.dec` 配置或已落地资产。

Console 连接期间持有 `KeepAlive`；Pull 等长操作跟 `dec-server` 生命周期走，切页后通过 `GetActiveOperation` / `WatchOperation` 恢复进度与结构化结果。

## Console 异步交互契约

Console 的异步状态由 Shell 级 action registry 统一管理，页面不得自行维护 `busy` / `loading` / `saving` 生命周期：

- 每个动作声明设备、资源锁、读写类型、稳定 key 和用户可见文案；
- 同设备、同资源的 write / operation 互斥，read-read 可并行；连接、认证、断开和重启属于 session 独占动作；
- 只有 session 转换使用模态锁。普通读写和长任务允许切页，冲突按钮由注册表统一禁用；
- query 同 key 去重；强制刷新以 generation 取代旧请求，旧响应不得覆盖新状态；
- Invoke 的同步事件与 RunOperation / WatchOperation 的流式事件进入同一个 registry；长任务按 `operation_id` 归属；
- 连接成功及受管项目变化后，Console 轮询 Global 与项目根的活跃操作，自动旁观本 Console 或 MCP 发起的任务；
- 运行状态全局可见，成功反馈短暂保留，错误保留到用户关闭；结构化业务结论仍由结果区渲染。

该约定与 `Documents/TUI_ARCHITECTURE.md` §5.5 一致：任务跟 Shell，不跟当前页面。

## 被否方案

**A. 管理面板与 TUI 分两套锁。** 否决：只有一个 `dec-server`，锁定必须是进程状态。

**B. 用 `server.json` token 当作远程所有权。** 否决：远程拿不到该文件；本机读文件也不能证明主人身份。

**C. 把 UI embed 进 `dec-server` / 改 `dec` 为浏览器启动器。** 否决：客户端独立进程，本机与远程同一套连接模型。

