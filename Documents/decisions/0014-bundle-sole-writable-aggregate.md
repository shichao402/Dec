# 0014 — Bundle 是唯一可写聚合根

- **状态**：已接受（已实现）
- **日期**：2026-08-17
- **关联**：[0002](0002-secrets-synctarget-root.md)（取消 project 级可写归属）、[0004](0004-remote-page.md)（`N` 只接受 `bundle/<名>`）、[0009](0009-bundle-binary-scope.md)、[0011](0011-private-repo-gcm-bootstrap.md)、[0013](0013-secrets-belong-to-declared-target.md)（归属收成一类）
- **影响范围**：`internal/app/bundle_writer.go`、Remote 登记、secrets pull/push plan、`SyncKindProject` 退役为兼容层、存量 `.secrets/project` 迁移

## 问题

0013 把「必须归属已声明 SyncTarget」立成硬规矩，但仍保留 **两类** 可写归属（bundle + 裸 project folder）。业务层因此继续出现「写 SyncTarget / 写 project folder / 写 enabled_bundles」等多条入口，与用户心智冲突：

> 要写的东西只有一个，就是 Bundle。

Project 级 secrets（folder = 裸 project 名，落地 `.secrets/project/`）制造了第二种可写对象，使 Remote `N`、pull plan、AddSecret 候选都要理解两套规则。旁路一多，就会复发「手搓 target」「静默改 scope」「凭据进不了 bundle」类问题。

## 决策

**Bundle 是 Dec 唯一可写聚合根。** 公开半边在 Git vault `bundles/<name>/`，私密半边（可选）在 Bitwarden `bundle/<name>/`。Project 与 User 不写内容，只声明/启用引用了哪些 Bundle。

```text
Bundle
├─ identity：name / scope（user | project）
├─ public → Git vault bundles/<name>/
└─ private（可选）→ Bitwarden bundle/<name>/

Project = projects/<name>.yaml 的 bundles 列表（只引用）
User    = ~/.dec/config.yaml 的 enabled_bundles（只启用）
```

### 1. 可写归属只剩一类

| 归属 | Bitwarden folder | 声明来源 | 落地 |
|------|------------------|----------|------|
| bundle | `bundle/<name>` | vault `bundles/<name>/bundle.yaml` | `.secrets/bundles/<name>/` 或 `~/.dec/secrets/bundles/<name>/` |

**取消** 0002 / 0013 的 project 级可写归属（裸 project 名 → `.secrets/project/`）。项目专属密文件改为某个 `scope: project` 的 Bundle（常见做法：与项目同名的 `bundle/<project_name>`），由 `projects/<name>.yaml` 引用。

`SyncTarget` 降为 Bundle 私密半边的**内部**落盘细节；业务 API 不再以 SyncTarget / project folder 为写入主语。

### 2. 唯一写入口：BundleWriter

TUI / MCP / `dec-server` dispatch 对权威状态的写入只经 `BundleWriter`（或等价 facade）：

- 生命周期：Create / Delete / SetScope（显式；启用流程禁止附带改 scope）
- 公开半边：增删改 vault 资产
- 私密半边：增删改/Rename Note 与 SSH（内部再调 `secrets.Client` + Declared target）
- 启用：Enable / Disable on plane（先确保 manifest 存在且 scope 匹配，再写 `enabled_bundles`）

派生副作用（GCM Apply、SSH landing）不单独暴露：随 Bundle secrets 的 pull/登记契约触发。

**CleanupUnmanaged**：删除非托管裸 folder 内容（只删 BW，不创建 Bundle），与 BundleWriter 并列，文案钉死「非模型内写入」。

### 3. Remote / pull / bootstrap

- Remote `N`：**只**接受 `bundle/<名>`（0013 允许的「已声明 project 名」取消）。
- pull/push plan：只解析当前平面 `enabled_bundles` 对应的 bundle SyncTarget；不再追加 project SyncTarget。
- [0011](0011-private-repo-gcm-bootstrap.md) bootstrap：**保留**破环查找 + Apply；不算 Bundle 写入；非 `bundle/*` 继续标 Unmanaged 并引导迁入 Bundle。

### 4. 存量迁移

`.secrets/project/**` 与 BW 裸 project folder → `bundle/<project_name>/`（`scope: project`），本地同步根迁到 `.secrets/bundles/<name>/`，`projects/*.yaml` 补引用后删源。

## 理由

- **一个对象**：用户与代码都只需理解 Bundle；少一套 project secrets 规则。
- **入口可守**：旁路无法再以「这是 project folder」为名绕过 Bundle 约束。
- **0013 升级而非推翻**：Declared SyncTarget 仍在，只是能声明的种类收成 bundle 一种。

## 被否方案

| 方案 | 否决理由 |
|------|----------|
| A. 继续保留 project 级 SyncTarget（0013 §1a） | 第二种可写对象；与「只写 Bundle」冲突 |
| B. 取消 Bundle，只保留 SyncTarget 为领域对象 | 公开资产无聚合根；与 vault `bundles/` 同构破裂 |
| C. Project 直接拥有 secrets 树、不经 Bundle | 启用/pull/Remote 又要分叉两套路径 |
| D. bootstrap 也必须先建 Bundle 再连私仓 | 环依赖回归 |

## 后果

- 实现：`internal/app/bundle_writer.go`；Remote / secrets plan / AddSecret 去 project 写入；迁移 API 复用 `remote_migrate.go`
- 文档：`BUNDLE-SECRETS-MODEL.md` 一句话改为「可写对象 = Bundle」；0002 / 0004 / 0013 顶部标注被本决策修订
- 测试：Remote `N` 拒 project 裸名；plan 无 project target；手搓 SyncTarget 写入仍拒
