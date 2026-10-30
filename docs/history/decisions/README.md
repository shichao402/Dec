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
| [0021](0021-console-owned-runtime.md) | Console 独占用户分发与目标运行时 | 已被 vNext 断代取代（Console 已移除） |
| [0022](0022-console-bitwarden-unlock.md) | Console 统一承载 Bitwarden 人工解锁 | 已接受；部分取代 0008、0018 的旧解锁叙事 |
| [0023](0023-facade-capability-parity.md) | 门面能力口径：Console 覆盖人面能力，MCP 扩展需登记 | 已接受（已实现） |
| [0024](0024-vault-delete-to-trash.md) | 远端删除移入保险库回收站，不永久删除 | 已接受（已实现） |
| [0025](0025-mcp-console-gateway.md) | MCP 经唯一 Console 网关代理，不再直连 dec-server | 已被 0032 取代 |
| [0026](0026-project-provides-and-sync-worktree.md) | 项目作者源映射与可恢复同步工作副本 | 发版 / 本机推柜段已被 [0028](0028-official-registry-and-install.md) 取代；`provides` 作者源映射仍有效 |
| [0028](0028-official-registry-and-install.md) | 官方注册表、安装器与个人私仓 | 已接受 |
| [0029](0029-single-consumer-requires.md) | 消费声明唯一化：`requires` + 提供方 `depends_on` | 已接受 |
| [0030](0030-agent-tools-thin-shell.md) | Agent 工具清单与编排下移，dec-mcp 零业务知识 | 清单与 Plan 仍有效；壳与网关已被 0032 取代 |
| [0031](0031-control-plane-and-upstream-contribution.md) | 个人私仓是纯控制面，提供方一律走上游贡献 | 已接受 |
| [0032](0032-mcp-streamable-http.md) | MCP 改为 dec-server 上的 Streamable HTTP | 已接受 |
| [0033](0033-requires-registry-only.md) | 订阅只从官方注册表安装，废除 `vault` pin | 已接受 |
| [0034](0034-bw-belonging-annotation.md) | BW 归属标注：folder 保持平铺，registry 快照作为仓归属 SSOT | 已接受（草案） |
| [0035](0035-product-secrets-plane-declaration.md) | 产品密钥平面声明：secrets_plane 进产品定义，路径降级为校验对象 | 已接受（草案） |
| [0036](0036-ssh-credential-lifecycle.md) | SSH 凭证生命周期与凭据请求通道 | 已接受（草案） |
| [0037](0037-dec-push-upload-split.md) | `dec_push` 语义拆分与 `dec_upload` 引入 | 已接受 |
| [0038](0038-deploy-optional-args.md) | 部署单元的可选参数走 EnvironmentFile | 已接受（已实现） |
| [0039](0039-audit-commit-coalescing.md) | 审计提交按时间窗合并，冷备推送前冲写 | 已接受（已实现） |
| [0040](0040-audit-push-cadence.md) | 审计有自己的推送节奏，不再等"有人改内容" | 已接受（已实现） |
| [0041](0041-notice-rendered-by-server.md) | 说明行由服务端渲染时写入，代理只落字节 | 已接受（已实现） |
| [0042](0042-sync-audit-dedup.md) | sync 审计按内容指纹降噪，密钥下发不降噪 | 已被 0043 取代 |
| [0043](0043-audit-tiers.md) | 审计按事件级别分级，访问类事件有界聚合 | 已接受（已实现） |
| [0044](0044-proxy-protocol-real-ip.md) | 服务端解析 PROXY protocol，恢复真实来源地址 | 已接受（服务端已实现，nginx 待改） |
| [0045](0045-public-repo-not-private.md) | 代码仓库保持公开，改为守住「仓库里不能有真东西」 | 已接受（需求 22 已按此改写） |
| [0046](0046-deploy-secret-in-environment.md) | 部署凭据收进 production environment，作用域由平台裁决 | 已接受（已实现并实测） |
| [0047](0047-multi-path-hash-compare.md) | 多落点哈希必须全量比对，不看最后一份 | 已接受（v2.1.21 已发版，真机验证通过） |
| [0048](0048-empty-hash-still-records-override.md) | 空哈希也必须记一条覆盖事件 | 已接受（v2.1.22～v2.1.24 逐层修完，真机验证通过） |
| [0049](0049-uninstall-must-not-delete-user-files.md) | 卸载不得删用户自己的文件，兜底扫描只认说明行 | 已接受（待发版） |
| [0050](0050-mcp-entry-must-nest-in-mcpservers.md) | MCP 条目必须落进 mcpServers 容器，写在顶层等于静默失效 | 已接受（v2.1.27 已发版，真机验证通过） |
| [0051](0051-json-key-hash-canonical-form.md) | json_key 条目按规范化语义算哈希，两侧不各算各的落盘字节 | 已接受（待发版） |
| [0052](0052-server-ca-in-bitwarden.md) | 服务端 CA 私钥只在本机与 Bitwarden，不进 git，缺失时不静默重建 | 已接受（实现中） |
| [0053](0053-edit-asset-via-fixed-tool.md) | 改资产内容走固化工具 dec_edit_asset_file，说明行写明 bundle 与 path | 已接受（v2.1.33 已发版，v2.1.34 补 revision，真机验证通过） |
| [0054](0054-with-in-startup-host-sync.md) | 代理启动登记 dec 条目覆盖 With，更新地址时保留用户的 disabled 开关 | 已接受（v2.1.35 已发版，真机验证通过） |
| [0055](0055-mcp-entry-renamed-dec-vault.md) | 内置 MCP 入口改名 dec_vault，避开项目名与 dec- 托管前缀，自动迁移旧条目 | 已接受（v2.1.36 已发版，真机验证通过） |
| [0056](0056-codebuddy-user-mcp-path.md) | CodeBuddy 用户级 MCP 落点改为 ~/.codebuddy/mcp.json，并纳入启动登记 | 已接受（v2.1.37 已发版，真机验证通过） |
| [0057](0057-claude-mcp-path.md) | Claude 的 MCP 落点改为 ~/.claude.json（用户级）与项目根 .mcp.json（项目级） | 已接受（本机真机验证通过，待发版） |
| [0058](0058-agent-auto-update.md) | 代理静默自动更新：ed25519 签名清单、防降级、两段式替换（推翻需求 19.3） | 已接受（待发版） |
| [0059](0059-update-interval-from-server.md) | 自动更新检查间隔由服务端 SSOT 决定，代理不落盘 | 已接受（待发版） |
