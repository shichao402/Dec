# 0003 — Secrets bundle 的用户级启用（机器平面）

- **状态**：已接受（已实现）
- **日期**：2026-08-09
- **关联**：[0002-secrets-synctarget-root.md](0002-secrets-synctarget-root.md)
- **影响范围**：`~/.dec/secrets/config.yaml` 的 `user_enabled_bundles`、`pkg/secrets` sync plan、`pkg/app` pull / Settings、TUI Settings；**不**改 Bitwarden folder 命名协议

## 问题

某些 Bitwarden secrets bundle（例如 `bundle/woa`）**只有 SSH Key**，没有对应的 Git 公开资产，也不绑定某一项目。

按 0002 现状，这类资产仍要：

1. 伪装成 Dec bundle（哪怕 vault 里没有 `bundles/woa/`）
2. 写入各 project 的 `enabled_bundles` 才能进入 pull

两点都不成立：

- **操作麻烦**：跨项目重复 enable
- **语义错误**：SSH Key 落地本就是机器级 `~/.ssh/`（0001/0002 已确认），却用「项目启用列表」控制发现/拉取

## 决策

**不新增 `user/` Bitwarden 命名空间。继续使用 `bundle/<name>` SyncTarget；在用户/本机平面增加「用户级启用」列表。**

```text
启用平面（并集）：
  Project.enabled_bundles     → 当前项目 pull 时同步这些 bundle SyncTarget
  User.enabled_secret_bundles → 本机始终同步这些 bundle SyncTarget（与当前项目无关）

Bitwarden folder 仍为：bundle/<name>
SSH Key → ~/.ssh/（不变）
Secure Note → .secrets/bundles/<name>/（若有；仅当本次 pull 落在某 project 工作区时写入该工作区）
```

约定细则：

1. **User 启用 ≠ 必须有 vault `bundles/<name>/`**  
   允许 secrets-only bundle（纯 SSH / 纯 Note 包）。
2. **Project 启用与 User 启用是并集，幂等**  
   同一名字两边都启用不冲突；plan 去重。
3. **TUI 入口在 Settings（全局）**  
   不塞进某项目 Bundles 页；Bundles 页仍只管当前 project 的公开资产与 project 级 enable。
4. **Pull**：每次项目 pull（或 Settings 显式「同步用户 secrets」）把 `User.enabled_secret_bundles` 并入 secrets plan；至少保证 SSH 写入 `~/.ssh/`。
5. **配置落点**：`~/.dec/secrets/config.yaml` 字段 `user_enabled_bundles`（逻辑 bundle 名列表），**不是** `.dec/config.yaml`。

## 理由

- **对齐落地平面**：SSH 已是机器级；发现/拉取也应可全局启用。
- **复用 0002**：不新增 SyncTarget Kind、不强迫改 folder 名（`bundle/woa` 可保留）。
- **减轻跨项目噪音**：像公司跳板钥这类身份资产配置一次即可。
- **边界清晰**：真正跟某 Dec 产品包同生共死的机密，仍走 project `enabled_bundles`；跨项目的才进 User 列表。

## 被否方案

**A. 新建 Bitwarden `user/<name>`（或 `machine/<name>`）第三种 folder 协议。**  
否决：对当前「纯 SSH bundle」过度；需要迁移既有 folder；与已落地的 `bundle/` 协议重复。若未来用户级 Note 需要独立本机树且不想与 Dec bundle 概念混用，再开后续 ADR。

**B. 继续要求各 project `enabled_bundles` 勾选纯 SSH bundle。**  
否决：操作与语义双重错误。

**C. 在 Bundles 页做「钉选到本机」。**  
否决：Bundles 页是项目上下文；全局能力应在 Settings，避免「装成项目功能」。

## 非目标（本 ADR）

- 不改变 Secure Note / SSH 的落地路径约定（仍遵 0002）
- 不实现 MCP / `dec exec` 新语义
- 不自动把某 project 的 `enabled_bundles` 提升为 User 启用（须用户在 Settings 显式操作）

## 参考

- `Documents/BUNDLE-SECRETS-MODEL.md`（SSH 机器级落地）
- [0002](0002-secrets-synctarget-root.md)
