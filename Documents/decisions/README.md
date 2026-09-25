# 决策记录

记录 Dec 的架构决策：**决定了什么**、**为什么**、**否掉了哪些方案**。

设计文档（`ARCHITECTURE.md`、`BUNDLE-SECRETS-MODEL.md` 等）描述系统**当前**是什么样；
决策记录描述它**为什么**是这样。两者冲突时以决策记录为准，并应尽快把设计文档改平。

## 何时新增

- 定下一个后续会被反复追问「当初为什么这么选」的取舍
- 推翻既有做法
- 引入或删除兼容层

## 约定

- 文件名 `NNNN-短横线标题.md`，编号递增，不复用
- 决策一旦被取代，在原文件顶部标注 `已被 NNNN 取代`，不删除原文
- 必须写「被否方案」及其否决理由——这是记录里最容易被省掉、日后最有价值的部分

## 索引

| 编号 | 标题 | 状态 |
|------|------|------|
| [0001](0001-secrets-landing-path.md) | Secrets 落地路径：消费者路径即落地路径 | 已被 0002 取代 |
| [0002](0002-secrets-synctarget-root.md) | Secrets SyncTarget：`.secrets` 同步根镜像 | 已接受；project 级可写归属被 0014 取消 |
| [0003](0003-user-enabled-secret-bundles.md) | 用户级 Bundle 启用（机器平面） | 已接受；并集语义被 0009 取代，TUI 入口被 0012 取代 |
| [0004](0004-remote-page.md) | Remote 页：上下文无关完整远端编辑器（方案 R） | 已接受（已实现）；`N` 被 0013 收紧，再被 0014 限为仅 `bundle/<名>` |
| [0005](0005-secrets-machine-handlers.md) | Secrets Machine Handlers：点类型目录（`.gcm` / `.sshkey` / `.env`） | 已接受 |
| [0006](0006-retire-pkg-for-internal.md) | 源码布局：废除 `pkg/`，统一到 `internal/` | 已接受（已实现） |
| [0007](0007-machine-secrets-root.md) | 机器级 bundle secrets 根 + 项目覆盖层 | 已接受；覆盖层被 0009 取代 |
| [0008](0008-service-facade-split.md) | Dec 服务 / 门面拆分 | 已接受（规划中） |
| [0009](0009-bundle-binary-scope.md) | Bundle 二元 scope（user \| project） | 已被 0016 取代 |
| [0010](0010-pull-orphan-and-ops.md) | Pull 孤儿收敛、删除收敛与运维面修订 | 已接受（已实现） |
| [0011](0011-private-repo-gcm-bootstrap.md) | 私仓 GCM Bootstrap：Bitwarden 作为启动信任根 | 已接受（已实现）；0013 补候选归属提示 |
| [0012](0012-user-bundle-single-entry.md) | 用户平面 bundle 启用收拢到 Bundles 页 | 已接受（已实现）；「仅 secrets」文案被 0013 修订 |
| [0013](0013-secrets-belong-to-declared-target.md) | Secrets 必须归属已声明 SyncTarget：写入接口类型级收口 | 已被 0016 取代 |
| [0014](0014-bundle-sole-writable-aggregate.md) | Bundle 是唯一可写聚合根 | 已被 0016 取代 |
| [0015](0015-project-config-boundary.md) | 项目配置的边界：用户平面没有 project，`.dec/` 不得落在 Dec 根目录 | 已接受（已实现）；全局配置可带 version，见 0017 |
| [0016](0016-p-four-quadrant-model.md) | 顶层项目与公开/私有 × 用户/项目四象限 | 已接受；平面名改为 global/local，见 0017 |
| [0017](0017-local-layout-version.md) | 本机配置 kind/version 与 layout_version | 已接受 |
| [0018](0018-instance-lock-and-console.md) | 实例锁定与管理客户端 | 已接受（已实现） |
| [0019](0019-remote-provisioning.md) | 远端设备自动置备（SSH provisioning） | 已接受（已实现；真机与发布验收未完） |
| [0020](0020-retire-tui.md) | 卸下 TUI，Console 为人机入口 | 已接受（已实现） |
| [0021](0021-console-owned-runtime.md) | Console 独占用户分发与目标运行时 | 已接受（连接与发布协议已实现；发布基础设施待接入） |
| [0022](0022-console-bitwarden-unlock.md) | Console 统一承载 Bitwarden 人工解锁 | 已接受；部分取代 0008、0018 的旧解锁叙事 |
| [0023](0023-facade-capability-parity.md) | 门面能力口径：Console 覆盖人面能力，MCP 扩展需登记 | 已接受（已实现） |
| [0024](0024-vault-delete-to-trash.md) | 远端删除移入保险库回收站，不永久删除 | 已接受（已实现） |
| [0025](0025-mcp-console-gateway.md) | MCP 经唯一 Console 网关代理，不再直连 dec-server | 已被 0032 取代 |
| [0026](0026-project-provides-and-sync-worktree.md) | 项目作者源映射与可恢复同步工作副本 | 发版 / 本机推柜段已被 [0028](0028-official-registry-and-install.md) 取代；`provides` 作者源映射仍有效 |
| [0027](0027-relkit-owned-update-contract.md) | 更新契约只由 relkit 声明 | 已接受（已实现） |
| [0028](0028-official-registry-and-install.md) | 官方注册表、安装器与个人私仓 | 已接受 |
| [0029](0029-single-consumer-requires.md) | 消费声明唯一化：`requires` + 提供方 `depends_on` | 已接受 |
| [0030](0030-agent-tools-thin-shell.md) | Agent 工具清单与编排下移，dec-mcp 零业务知识 | 清单与 Plan 仍有效；壳与网关已被 0032 取代 |
| [0031](0031-control-plane-and-upstream-contribution.md) | 个人私仓是纯控制面，提供方一律走上游贡献 | 已接受 |
| [0032](0032-mcp-streamable-http.md) | MCP 改为 dec-server 上的 Streamable HTTP | 已接受 |
| [0033](0033-requires-registry-only.md) | 订阅只从官方注册表安装，废除 `vault` pin | 已接受 |
| [0034](0034-bw-belonging-annotation.md) | BW 归属标注：folder 保持平铺，registry 快照作为仓归属 SSOT | 已接受（草案） |
| [0035](0035-product-secrets-plane-declaration.md) | 产品密钥平面声明：secrets_plane 进产品定义，路径降级为校验对象 | 已接受（草案） |
