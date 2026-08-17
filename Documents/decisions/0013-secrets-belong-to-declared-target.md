# 0013 — Secrets 必须归属已声明 SyncTarget：写入接口类型级收口

- **状态**：已接受（已实现）；**§1 / §1a 两类归属已被 [0014](0014-bundle-sole-writable-aggregate.md) 收成仅 bundle**
- **日期**：2026-08-17
- **关联**：[0002](0002-secrets-synctarget-root.md)（本决策把其 folder 约定升级为强制归属）、[0004](0004-remote-page.md)（修订第 6 条 `N` 的 folder 输入）、[0005](0005-secrets-machine-handlers.md)、[0009](0009-bundle-binary-scope.md)、[0011](0011-private-repo-gcm-bootstrap.md)（补一条候选归属提示）、[0012](0012-user-bundle-single-entry.md)（修订「仅 secrets」文案）、[0014](0014-bundle-sole-writable-aggregate.md)
- **影响范围**：`internal/secrets/synctarget.go` 类型可见性、`secrets.Client` 全部写方法、`internal/app/remote_register.go`、`internal/app/secrets_pull.go` 的 `remoteInventoryTarget`、`ResolveTarget`、Remote 页 `N` 表单、Bundles 页文案

## 问题

Dec 的模型是 **bundle 为一等公民**：一个 bundle 由两部分组成——Git 私仓里的那部分（`bundles/<name>/`，公开可共享）与 Bitwarden 上的那部分（`bundle/<name>` folder，私密、可选）。两者通过 0002 的 folder 约定关联。`BUNDLE-SECRETS-MODEL.md` 也明写「secrets bundle 仍与 Dec bundle 一一绑定」。

但这条关联从未被立成硬规矩，代码里也没有任何一层强制它，于是出现了「不属于任何 bundle 的游散 secret」：

```text
Bitwarden
├── bundle/vikunja/.env/vikunja.env     ← 有归属，pull/push/Apply 全链路可维护
└── Dec/.gcm/cnb.yaml                   ← 裸 folder，无 SyncTarget，无 LocalRoot
```

游散 secret 的具体代价（以实际发生的 CNB GCM 凭据为例）：

1. **正常 pull 永远看不到它**。pull 只解析 enabled bundle（folder `bundle/<name>`）与当前 project folder，裸 folder 不在其中。
2. 因此 0005 定义的「同步根 → Handler Apply → 写入 GCM」链路**永不触发**。凭据轮换后正常 pull 不会更新 GCM，只能等下一次认证失败再走一遍 0011 的 bootstrap。
3. 0005 的 Revoke（删 Note 前 `credential reject`）同样没有正常触发点。
4. 它不会出现在 Bundles 页——因为它确实不属于任何 bundle。用户看到「找到了 cnb 凭据，但 bundle 列表里没有 cnb」，会合理地怀疑是 bug。

### 为什么现有约束没拦住

`SyncTarget` 有三个把关构造函数（`NewBundleSyncTarget` / `NewMachineBundleSyncTarget` / `NewProjectSyncTarget`），经它们产生的 folder 必然是 `bundle/<name>` 或已声明的 project 名。但两处旁路让把关形同虚设：

- `CommitRemoteRegister` 用结构体字面量手搓 target：`secrets.SyncTarget{Folder: folder, Name: folder, Kind: SyncKindProject}`。Remote 页 `N` 手输的任意 folder 名由此直达 Writer，`Kind` 被无条件写成 project，`LocalRoot` 留空。
- `ResolveTarget` 的 `explicit.LocalRoot != "" && explicit.Folder != ""` 分支直接返回调用方给的 target，不校验来源。

`secrets.Client` 的 7 个写方法（`PushBundle` / `CreateSSHKey` / `DeleteSecureNote` / `DeleteSSHKey` / `UpdateSSHKeyHosts` / `RenameSecureNote` / `RenameSSHKey`）**形状**已经统一成「吃 SyncTarget」，但因为这个类型可被随手构造，统一形状没换来任何约束。

## 决策

**每个由 Dec 写入 Bitwarden 的对象都必须归属一个已声明的 SyncTarget；归属由类型系统在编译期保证，不靠调用方自觉、不靠用户记规则。**

### 1. 归属公理（合法归属只有一类：bundle）

> **修订（[0014](0014-bundle-sole-writable-aggregate.md)）**：原「bundle + 裸 project」两类已收成仅 `bundle/<name>`。新写入路径以 0014 为准；`NewProjectSyncTarget` 降为迁移/只读兼容。

| 归属 | Bitwarden folder | 声明来源 | 落地 |
|------|------------------|----------|------|
| bundle | `bundle/<name>` | vault `bundles/<name>/bundle.yaml`（含由启用/登记按需补的占位） | `.secrets/bundles/<name>/` 或 `~/.dec/secrets/bundles/<name>/` |

**「未在 Git vault 声明过的裸 folder」不是合法归属**，Dec 不向其写入任何内容（浏览与 CleanupUnmanaged 除外）。

范围是**全部** secrets，不限于 `.gcm` / `.env` / `.sshkey` 点类型目录——普通 Secure Note 同样受约束。点类型目录只决定「落地后怎么处理」（0005），不决定「归不归属」。

### 1a. ~~bundle 级还是 project 级~~（已被 0014 废止）

原「跨项目复用进 bundle / 单仓进 project」判据**不再成立为可写归属分叉**。项目专属密文件改为某个 `scope: project` 的 Bundle（常见：与项目同名），由 `projects/<name>.yaml` 引用。详见 [0014](0014-bundle-sole-writable-aggregate.md)。

### 2. 写入接口类型级收口

`SyncTarget` 改为**不可从包外手工构造**（未导出字段 + 只读访问器，或等价手段）。能产生**可写** target 的只有：

- `NewBundleSyncTarget` / `NewMachineBundleSyncTarget`（要求 bundle 在 vault 有 manifest，或本次调用负责补占位）

`NewProjectSyncTarget` 仅保留给存量迁移与只读兼容，**禁止**进入新的写入路径。

`Client` 的写方法只接受 Declared target。业务层经 `BundleWriter`（0014）统一入口。

### 3. 浏览与写入分型

Remote 页仍需看见全量 folder（0004 第 3 条不变，包括裸 folder 与孤儿 folder），因此保留一个**只读远端节点**类型（承接现在 `remoteInventoryTarget` 为裸 folder 造的无 `LocalRoot` target）。该类型**不能**被传入任何写方法，由类型系统拒绝，而不是靠调用方记得别传。

删除是唯一例外：裸 folder 内容必须可删（否则存量无法清理）。删除接口接受只读节点，但语义固定为「只删远端」，不涉及 LocalRoot。

### 4. Remote 页 `N` 的 folder 输入约束（修订 0004 第 6 条）

`N` 手输 folder 只接受两种形态：

- `bundle/<名>`：视为新 bundle。提交时同步在 Git vault 补一份最小 `bundle.yaml` 占位（复用 0003 第 4 条既有机制），使这一半立刻有归属。
- 已在 vault `projects/*.yaml` 声明的 project 名。

其它裸名**拒绝提交**，提示改用 `bundle/<名>`。0004 原文「或尚未出现在树上的空 folder」这一条被本决策收紧。

### 5. 存量裸 folder = 非托管

裸 folder 里的既有内容保持可见、可读、可删（0004 能力不减），但在 Remote 页标记为**非 Dec 托管**，并明示它不参与 pull / push / Handler Apply。提供显式迁移动作（把 Note 搬进 `bundle/<名>`），迁移必须由用户确认，不自动进行。

### 6. 0011 Bootstrap 候选提示（补充约束）

Bootstrap 仍枚举**全部** folder 按 host 匹配——这是打破环依赖的前提，不改。但候选若位于非托管裸 folder：

- Apply 仍然允许（救急路径不能断）
- TUI **必须**明示：该 Note 不属于任何 bundle，pull 不会维护它，凭据轮换后需要重新 bootstrap
- 同时给出迁移建议

### 7. 跨平面 scope 不得静默改写（已实现）

归属既然是硬约束，改变归属就必须是显式动作。用户平面勾选保存时（`ensureVaultBundlesForUserEnable`）：

| manifest 状态 | 处理 |
|---------------|------|
| 不存在 | 创建 `scope: user` 占位（0003 第 4 条不变） |
| `scope: user` | 无操作 |
| **显式** `scope: project` | **拒绝**，不改 manifest，不进 `enabled_bundles`，向用户说明 |
| 缺省 scope 且无 project 引用 | 按 0009 迁移期推断写回 `scope: user` |
| 缺省 scope 但被任一 `projects/*.yaml` 引用 | **拒绝**，理由中点明引用它的 project |

被拒绝的名字**不得**留在 `enabled_bundles`：那会是一个「勾了但平面隔离永远看不见」的条目。因此保存顺序也随之调整为「先校验/修复共享 vault，再写本机启用列表」。

原实现把「缺省」与「显式 project」都归一成 project 后统一改写为 user，等于用一次勾选静默把 bundle 搬到另一平面，所有引用它的 project 会突然拉不到资产。0009 迁移期只授权推断**缺省**的那种。

Bundles 页对应地把这类条目标为 `OtherPlane`，展示「属于项目平面」、复选框画成 `[-]`，并拒绝 `space` 勾选。

### 7a. 项目平面同样校验（只校验，不修复）

项目平面的保存路径起初直接写盘，于是已从 vault 删除的 bundle 名能一直留在 `enabled_bundles` 里，pull 每次只回一句「引用的 bundle 找不到声明，已忽略」。校验现在两平面对称：本平面 vault 里看不见的名字不进 `enabled_bundles`，并区分两种理由——「仓库里没有这个 bundle（可能已被删除）」与「属于用户平面（`scope: user`）」。

与用户平面不同，项目平面**只校验不修复**：不创建占位（project bundle 由 vault 显式维护），更不改写别人的 scope——那正是第 7 条禁止的动作。隐式 bundle（目录有资产但无 manifest）算可见；仓库未连接时无从校验，放行以免离线存不了。

被拒条目必须回传给用户（`SaveBundleSelectionResult.RejectedBundles`）并在 TUI 显示。此前该字段没有任何渲染，用户只会看到「勾了却没生效」——静默丢勾选和静默改 scope 一样不可接受。

### 8. 文案对齐（修订 0012 第 3 条）

`AssetBundleOption.SecretsOnly` 的展示文案不再用「仅 secrets」——0003 的 2026-08-09 修订已明确交代过不用该词，因为它暗示存在一种「不是 bundle 的 secrets 实体」，与一等公民模型冲突。改用「仓库未登记」：它仍是 bundle，只是 Git 那一半尚未落 manifest。

同时该标记不得再覆盖「vault 已有 manifest、只是属于另一平面」的条目——那种情况说「vault 尚无 manifest」在事实层面就是假的。两类分别用 `SecretsOnly` 与 `OtherPlane` 表达。

## 理由

- **不让用户记规则**：归属规则写在类型里，写错编译不过；用户不需要在登记表单前回忆 folder 命名约定。
- **范围取全集**：只约束点类型目录会留下「普通 Note 可以游散」的缺口，规则又退回靠人记。
- **保住 0004 的可见性**：靠「只读节点 / 可写 target」分型，而不是砍掉裸 folder 的可见性来实现约束。
- **保住 0011 的救急能力**：约束加在「写入」与「提示」上，不加在 bootstrap 的查找范围上，环依赖的解法不受影响。

## 被否方案

| 方案 | 否决理由 |
|------|----------|
| A. 只约束 `.gcm` / `.env` / `.sshkey` 点类型目录 | 普通 Secure Note 仍可游散；规则退化为「部分类型有约束」，又要靠人记 |
| B. 在每个写方法开头运行时 if 校验 folder 前缀 | 7 处重复校验，新增 Writer 必漏；约束应在类型层一次到位 |
| C. 承认裸 folder 为第三类合法归属 | 它没有可靠 LocalRoot（0004 已就此否决过「编辑顺带种本地盘」），pull / push / Apply 均无处落地，等于长期维护一类瘸腿 secret |
| D. Remote 页不再显示裸 folder | 推翻 0004 第 3 条，且会拿掉清理存量裸 folder 内容的唯一入口 |
| E. 自动把裸 folder 里的 Dec 语义 Note 迁进 `bundle/<名>` | 未经确认改远端；与 0003 第 4 条「不在发现时自动建包」同源 |
| F. 限制 bootstrap 只扫 `bundle/*` | 会让「凭据在裸 folder」的存量用户彻底连不上仓库，环依赖回归 |

## 后果

- 实现落点：`internal/secrets/synctarget.go`（类型收口 + `ResolveTarget` 去后门）、`internal/secrets/client.go`（写方法签名）、`internal/app/remote_register.go`（去掉手搓 target）、`internal/app/secrets_pull.go`（`remoteInventoryTarget` 拆出只读类型）、`internal/tui/` Remote `N` 表单校验与非托管标记、Bundles 页文案。
- 存量迁移：现有 CNB GCM 凭据需从裸 folder 迁入 `bundle/<名>`，迁移后正常 pull 即可维护 GCM。
- 文档需改平：`BUNDLE-SECRETS-MODEL.md`（归属公理与非托管概念）、`0004` 第 6 条、`0011` 候选提示、`0012` 文案。
- 测试：`N` 拒绝裸名、只读节点不可写（编译期或等价测试）、bootstrap 非托管候选提示、迁移动作。
