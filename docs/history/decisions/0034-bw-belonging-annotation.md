# 0034 — BW 归属标注：folder 保持平铺，registry 快照作为仓归属 SSOT

- **状态**：已接受（草案）
- **日期**：2026-09-25
- **关联**：[0016](0016-p-four-quadrant-model.md)、[0026](0026-project-provides-and-sync-worktree.md)、[0028](0028-official-registry-and-install.md)、[0031](0031-control-plane-and-upstream-contribution.md)
- **影响范围**：`internal/app/list_secrets.go`、`internal/registry/snapshot.go`、`internal/agenttools`（`dec_list_secrets` 输出）、Console 密钥清单页

## 问题

多产品仓（`products:` 声明多个产品，一个 Git 仓对应多个 BW folder）让 BW 侧丢失了仓归属：folder 名 = 产品名，`cnb` / `woa` / `tencent-cloud` 同属 DecPersonalDevKit 这件事，只存在于产品仓配置和 registry 快照里，BW 与 Console 的密钥清单都读不出来。folder 列表越积越多，越像一个无主名字的堆，人类分不清哪些 folder 同源、哪些是身份型产品、哪些是孤儿。

同时存在两类判读需求：

1. 人类侧：密钥清单只有平铺的文件行，无仓分组、无身份型标注、无孤儿判读。
2. Agent 侧：`dec_list_secrets` 只回路径，回答不了「`cnb` 这个 folder 是谁家的」「哪些 folder 是孤儿」，只能全量枚举后自己翻 registry。

## 决策

**BW 地址模型一行不动**（folder = 产品名，Note 名 = `private/<plane>/<rel>`，见 0016/0017）。仓归属不进 BW，registry 快照（每产品一份 `provider.yaml` + 资产名单）是归属 SSOT：`folder 名 = 产品名 = registry 目录名`，读取路径上做一次 join。

归属信息只增强展示，不改变任何写路径：不写回、不迁移、不产生隐式删除。

### 判读规则

1. **正常产品**：registry 目录存在且资产名单非空。folder 行标注落地状态（现状已有）。
2. **身份型产品**：registry 目录存在、资产名单为空（`provides` 为空，只发身份，密钥留在 BW 同名 folder）。行内 badge 标「仅密钥」，提示删 folder 前先确认 `products` 里是否还声明。
3. **孤儿 folder**：registry 查无此产品。折叠到「疑似孤儿」区域；`list_delete_candidates` 复用同一孤儿判定，把「registry 查无 + 无 requires 消费」的 folder 标为高置信删除候选。

### 人类侧（Console 密钥清单）

密钥清单从平铺文件列表改为两级结构：顶层是来源仓分组（副标题显示 `origin_repo`），组内是产品 → note 文件。每行加归属 badge（正常 / 仅密钥 / 未归属）。数据源是 `discoverRemoteSecretTargets` 已经枚举出的全量 folder，只是多一层 registry join，数据量和现在一样多。

### Agent 侧（`dec_list_secrets`）

`SecretFileMetadata` 增加三个仅元数据字段（红线不变：绝不返回 Note 正文 / 密钥内容）：

```json
{
  "secrets_bundle": "cnb",
  "project_rel_path": ".env/app.env",
  "origin_repo": "https://.../DecPersonalDevKit.git",
  "identity_only": false,
  "orphan": false
}
```

Agent 由此可直接回答归属与孤儿问题，不用再自己翻 registry。

### 平面边界

registry 快照属于 global 平面，`list_secrets` 按任意 workspace 触发。归属是全局事实，与触发平面无关：项目平面调用同样读 registry 快照做 join，不把归属信息限制在 Global 资产页。

## 被否方案

- **方向 B：把仓归属编码进 BW（folder 前缀或 note 名加仓段）**：破坏 0016/0017 的地址唯一性，需要远端迁移；产品名本身已是全局唯一订阅名，再编码一份归属就是项目里反复否决的「第二套规则」。
- **只读「本机已知」名单（known bundles / requires），不查 registry**：known 是订阅快照，漏掉未订阅但已发布的产品；孤儿判读会把「未订阅」误判成「孤儿」，置信度不足。
- **归属信息写进 BW（folder 描述字段 / 假 note）**：把展示信息写进权威存储，与「浏览不写回」的原则冲突；BW 变成第二份归属 SSOT，与 registry 冲突时无解。
- **`dec_list_secrets` 加 `include_origin` 开关，默认关**：归属是元数据不是正文，没有泄露面；开关徒增调用复杂度，Agent 还得先知道要传它。

## 实现要点

改动集中在三处：`mergeRemoteSecretMetadataForWorkspace` 里 join registry 快照（`ReadProjects` 已有现成读取）、`SecretFileMetadata` 加字段、Console 分组渲染。浏览路径维持「不写 known」的原则（ADR 0004 修订）。
