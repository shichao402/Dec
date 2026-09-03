# 0012 — 用户平面 bundle 启用收拢到 Bundles 页

- **状态**：已接受（已实现）
- **日期**：2026-08-17
- **关联**：[0003](0003-user-enabled-secret-bundles.md)（取代其第 3 条与被否方案 D）、[0009](0009-bundle-binary-scope.md)
- **第 3 条「仅 secrets」文案已被 [0013](0013-secrets-belong-to-declared-target.md) 修订**：该标记语义不变，但展示文案改为「仓库未登记」一类，不再暗示存在非 bundle 的 secrets 实体
- **影响范围**：Settings 页、`dec --user` Bundles 页、`SaveGlobalSettingsInput.EnabledBundles`

## 问题

`GlobalConfig.EnabledBundles` 出现了两个写入口：

- Settings 页的「用户 bundles」勾选区（`SaveGlobalSettings` 的 `EnabledBundles` 字段）
- `dec --user` 的 Bundles 页（`SaveWorkspaceEnabledBundles`，user 平面直接写 `GlobalConfig`）

两者的勾选状态各自基于页面加载时的快照，因此在 Bundles 页启用一个 bundle 后再去 Settings 按 `s` 保存，会用旧快照覆盖掉刚才的改动。0003 当初把入口定在 Settings 并明确否决「Bundles 页钉选到本机」，理由是「Bundles 是项目上下文，全局能力在 Settings」——但 0009 引入用户平面后，`dec --user` 的 Bundles 页本身就是用户平面上下文，该前提已不成立。

同时两个入口的候选集合并不相同：Settings 候选是 vault ∪ `known_secret_bundles` ∪ Bitwarden folder 枚举 ∪ 已启用，而 Bundles 页只扫 vault 中已有 manifest 的 bundle。因此 Settings 实际上还承担了「把仅存在于 Bitwarden 的 bundle 首次提升为 `scope: user`」的职责，不能直接删除了事。

## 决策

**用户平面 bundle 启用的唯一写入口是 `dec --user` 的 Bundles 页。**

1. Settings 页移除可勾选的「用户 bundles」区块，仅保留一行只读计数并指向 `dec --user`；对应的光标行、`space` 切换与 dirty 判定一并移除。
2. Settings 保存时 `SaveGlobalSettingsInput.EnabledBundles` 恒为 `nil`（= 不修改）。该字段保留 nil / 非 nil 的既有语义，非 nil（含空切片）仍表示按「写回」覆盖。
3. 用户平面 Bundles 页的候选补齐 Settings 原有来源：已启用 ∪ `known_secret_bundles` ∪ Bitwarden folder 枚举。vault 中尚无 manifest 的条目标记为 `SecretsOnly`，展示为「仅 secrets」。
4. 勾选保存后仍由 `ensureVaultBundlesForUserEnable` 补 `scope: user` 的 bundle 声明（0003 第 4 条不变），此后该条目变为普通 vault bundle，标记消失。
5. 列候选不得为此触发 Console Authenticate：无 session 时退化为 known ∪ 已启用。

## 理由

- 单一写入口消除快照覆盖，这是本次改动的直接动因。
- 启用列表的语义属于「平面配置」，与 `dec --user` 的 Bundles 页同上下文；Settings 保留全局连接 / IDE / 本机 vars 等真正的机器级设置。
- 把 Bitwarden 候选并入 Bundles 页，使「提升 secrets-only bundle」与「启用普通 bundle」成为同一个动作，无需用户理解两处候选集合为何不同。

## 被否方案

### A. 直接删除 Settings 区块，不动 Bundles 页候选

否决：会丢掉唯一的 secrets-only 提升入口，只存在于 Bitwarden 的 bundle 将无法在 TUI 中启用。

### B. Settings 区块改为只读列表（逐个列出但不可勾选）

否决：只读列出全部候选占屏且无动作价值；用户真正需要的是「在哪改」，一行计数加指引即可。

### C. 保留双入口，改为保存前重新加载并做三方合并

否决：为一个不该存在的第二入口引入合并语义与冲突提示，复杂度换不到能力。

### D. 让 Settings 与 Bundles 页共享同一份内存选择状态

否决：跨页共享可变选择状态会把两个页面的生命周期绑死，且 `dec` 与 `dec --user` 是不同进程/平面，本就无法共享。
