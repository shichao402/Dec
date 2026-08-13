# 0003 — 用户级 Bundle 启用（机器平面）

- **状态**：已接受（已实现，2026-08-09 修订）
- **日期**：2026-08-09
- **修订**：2026-08-09 — 纠正首版实现偏差：用户级对象是 **Dec bundle**，不是「仅 secrets」；secrets-only 在**勾选启用**时才写入 vault 占位
- **关联**：[0002-secrets-synctarget-root.md](0002-secrets-synctarget-root.md)
- **影响范围**：`~/.dec/secrets/config.yaml` 的 `user_enabled_bundles`（语义：本机启用的 Dec bundle 短名）、`internal/app` pull / Settings、vault `bundles/<name>/` 按需创建；**不**改 Bitwarden `bundle/<name>` folder 协议

## 问题

某些能力（例如 `bundle/woa` 纯 SSH、或要在多项目复用的公开包）应在**本机**配置一次，而不是塞进每个 project 的 `enabled_bundles`。

首版实现把 Settings 收成「只勾选 secrets」，与用户心智不符：用户级配置的对象应是 **bundle**（公开资产 ∪ 对应 secrets），secrets 只是该包协议的一侧。

另外：secrets-only 发现后若永远不在 Dec vault 露面，Settings/刷新很难形成稳定清单。需要在合适时机补齐 vault 侧的 bundle 身份。

## 决策

**不新增 `user/` Bitwarden 命名空间。用户/本机平面启用的是 Dec bundle 短名；启用集合与 project `enabled_bundles` 做并集。**

```text
启用平面（并集）：
  Project.enabled_bundles  → 当前项目启用的 Dec bundle
  User.enabled_bundles     → 本机始终启用的 Dec bundle（Settings 配置）

一次 project pull：
  目标包 = Project ∪ User
  → Git 公开资产（vault bundles/<name>/）按并集解析安装
  → secrets SyncTarget 按并集 pull（0002）；SSH → ~/.ssh/

Bitwarden folder 仍为：bundle/<name>
```

约定细则：

1. **用户级对象是 Dec bundle**  
   Settings 展示/勾选的是 bundle 短名（候选 = vault 已有 ∪ `known_secret_bundles` ∪ 已启用）。文案用「本机启用的 bundles」，不是「仅 secrets」。

2. **Project 与 User 并集、幂等**  
   同名两边都启用不冲突；pull 资产与 secrets 共用合并后的名单。

3. **TUI 入口在 Settings（全局）**  
   Bundles 页仍只管**当前项目**的 enable；不在此「钉到本机」。

4. **secrets-only → 启用时再建 vault 占位**  
   - 仅枚举 / pull 发现：写入本机 `known_secret_bundles`，**不**自动改 Git vault。  
   - 用户在 Settings **勾选启用并保存**：若 vault 尚无 `bundles/<name>/`，则创建最小 `bundle.yaml`（`members: []`）并 commit+push。  
   - 不在「发现时」自动建包，避免污染共享 vault、避免未确认就改远端。

5. **配置落点（现阶段）**  
   字段名仍为 `user_enabled_bundles`，落在 `~/.dec/secrets/config.yaml`（历史路径）；**语义是本机启用的 Dec bundle 短名**，不是「仅 secrets 列表」。后续若迁到 `~/.dec/config.yaml` 另开修订，不改本决策语义。

6. **仅 User 启用、项目未勾选时仍可 pull**  
   项目 `enabled_bundles` 为空但 User 非空时，pull 不得整单跳过；无公开成员时仍应同步 secrets（如 SSH）。

## 理由

- **对齐用户心智**：本机配置的是「包」，secrets / 公开资产都是包的面。
- **复用 0002**：仍用 `bundle/<name>` SyncTarget；不引入 `user/` folder。
- **启用时建 vault**：给 secrets-only 以 Dec 身份，刷新可见；又避免「发现即写仓库」。
- **边界清晰**：跟某项目绑定的仍走 project Bundles 页；跨项目的进 User。

## 被否方案

**A. 新建 Bitwarden `user/<name>`。**  
否决：与 0002 `bundle/` 重复；迁移成本高。

**B. Settings 只管理 secrets，不管公开 bundle。**  
否决：首版实现即此偏差；用户无法在本机统一启用整包。

**C. 枚举/pull 发现 secrets-only 就自动创建并 push vault。**  
否决：未经确认改共享仓库；泄漏「存在某 secrets 包」的时机过早。

**D. Bundles 页「钉选到本机」。**  
否决：Bundles 是项目上下文；全局能力在 Settings。

**E. 允许长期 secrets-only 且永不进 vault。**  
否决（作为默认路径）：刷新与清单不稳定；改为「启用时补占位」。未启用前仍可仅 known 缓存。

## 非目标（本 ADR）

- 不改变 Secure Note / SSH 落地路径（仍遵 0002）
- 不实现 MCP / `dec exec` 新语义
- 不自动把 project `enabled_bundles` 提升为 User 启用
- 不在本轮把字段迁出 `~/.dec/secrets/config.yaml`（仅澄清语义）

## 参考

- `Documents/BUNDLE-SECRETS-MODEL.md`
- [0002](0002-secrets-synctarget-root.md)
