# 0035 — 产品密钥平面声明：secrets_plane 进产品定义，路径降级为校验对象

- **状态**：已接受（草案）
- **日期**：2026-09-25
- **关联**：[0016](0016-p-four-quadrant-model.md)、[0026](0026-project-provides-and-sync-worktree.md)、[0028](0028-official-registry-and-install.md)、[0031](0031-control-plane-and-upstream-contribution.md)、[0034](0034-bw-belonging-annotation.md)
- **影响范围**：`internal/types/pack.go`、`internal/config/provides.go`、`internal/registry`、`internal/publish`、`internal/app`（规划 / 推送 / 密钥清单）、Console 密钥清单页

## 问题

BW 条目名里的 `private/<plane>/` 段是平面推导的投影，不是平面声明：平面来自订阅者
workspace（`Workspace.SecretsPlane()`），产品从未声明自己的密钥该落在哪个平面。现状全落
`private/global` 只是「当前都从 global 平面操作」的巧合。后果：

1. 项目平面的密钥清单里混着机器平面资产，人类分不清「这个项目有哪些关联私密资产」。
2. 一旦某产品从项目平面订阅出现，同一 folder 会同时长出 `private/local` 与
   `private/global` 两套路径，没有基准可判哪个对。
3. 孤儿判读同理：路径推导的平面无法回答「这个 folder 的密钥本来就该在这里吗」。

## 决策

密钥平面的唯一声明在产品定义上：`secrets_plane: global | local`（多产品仓写在每个
`products.<name>` 下，单产品仓写在 `.dec/config.yaml` 顶层；`products` 存在时顶层声明被
忽略，与 `tags` 同规则）。声明随发布进 registry 快照（`provider.yaml`），与 `origin_repo`
/ `tags` 同链路。订阅者 workspace 的平面推导从「归属依据」降级为「校验对象」：

- **pull / 密钥清单（fail-open 约束）**：声明平面的产品只出现在对应平面的 plan 与密钥
  清单里；项目平面清单不再显示声明为 `global` 的产品，用户平面清单不再显示声明为
  `local` 的产品。未声明（迁移期）与 registry 不可达时沿用现状推导，绝不因断网阻断同步。
- **push（fail-closed 校验）**：声明平面与当前平面不符的 target 不进 plan；若其本地
  同步根仍有文件，push 直接报错——写回路径与声明不符就该报错，而不是静默丢弃。
- **声明语义**：`global` = 密钥落在机器根 `~/.dec/secrets/<p>/`（`private/global` 条目）；
  `local` = 落在项目 `.secrets/<p>/`（`private/local` 条目）。

### 平面判读规则

| 场景 | 行为 |
|------|------|
| 产品声明 `global` | 项目平面 plan / 清单跳过该产品；用户平面正常 |
| 产品声明 `local` | 用户平面 plan / 清单跳过该产品；项目平面正常 |
| 未声明（迁移期） | 两侧都沿用平面推导，行为与 0035 之前完全一致 |
| registry 不可达 | 声明查本机 cache 已装回落；查不到按未声明处理 |
| 存量 folder（非产品名） | 不参与平面判定，维持 Unmanaged 语义 |
| push 声明不符且本地有文件 | 报错：提示迁移文件或修正声明 |

`dec_list_secrets` 每条元数据增加 `declared_plane`（`global` | `local` | 空），Agent 可
直接回答「产品 X 的密钥平面是什么」。

### Remote / 删除页不过滤

Remote 是上下文无关的完整远端浏览器（ADR 0004 修订）：删除页与 Remote 仍枚举全部
folder 与两个平面的条目——清理语义不受声明约束。声明只收紧 pull / push / 密钥清单的
「归属」路径。

## 被否方案

- **把 secret 塞进 provides 声明平面**：`ProjectProvideTarget` 明确拒绝 `type: secret`
  （ADR 0026：`.secrets/` 整树由 SyncTarget 规则同步，再声明一遍就是第二套规则）。平面
  必须是产品级新字段，不能伪装成一条 provide。
- **沿用 `tags: [global]` 当平面声明**：tags 只用于订阅页推荐筛选（0031 已定），语义过载
  会让「推荐勾选」与「密钥落点」互相绑架。
- **继续由订阅者平面推导（现状）**：推导是果不是因——项目平面订阅出现后同一产品双路径
  并存无基准；密钥清单无法回答「这个项目有哪些关联私密资产」。
- **未声明即 fail-closed（项目平面直接跳过所有 global 推导 target）**：存量产品仓都没
  声明，先收紧会把大量在用同步路径一次性打断；迁移期必须 fail-open，等声明补齐后收紧
  自然生效。
- **把平面写进 BW（folder 描述 / 条目名加段）**：与 0034 被否方案同理，BW 是投影不是
  声明源，写回去就成了第二套规则。

## 实现要点

四层贯穿：`ProductDecl.SecretsPlane` / `ProjectConfig.SecretsPlane`（声明）→
`AuthorProduct`（发布）→ `ProviderMeta.SecretsPlane` / `ProjectSnapshot.SecretsPlane`
（registry 携带）→ `secretsBelongingResolver`（消费侧判定，含 cache 回落）。规划过滤挂
在 `planWorkspaceSecretsSync`，push 校验挂在 `PushWorkspaceSecretsBundles`（对
`SkippedByPlane` 扫同步根，非空即报错），清单行过滤挂在
`mergeRemoteSecretMetadataForWorkspace`。
