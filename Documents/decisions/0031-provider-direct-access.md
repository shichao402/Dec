# 0031 — 提供方直写权限与控制面

- **状态**：已接受
- **日期**：2026-09-21
- **关联**：[0026](0026-project-provides-and-sync-worktree.md)、[0028](0028-official-registry-and-install.md)、[0029](0029-single-consumer-requires.md)
- **影响范围**：个人私仓项目声明 `access`、registry 快照 `provider.yaml`、`create_local_asset` 落点、订阅面板、沉淀 Skill

## 问题

0026 把作者源定为提供方仓 `provides` / `DecAssets/`。0028 规定官方字节只经 CI 写入 registry，本机不得把 `provides` 项目 `dec_push` 进个人私仓。0029 用 `requires` 的 pin 区分 registry 与私仓。

三层叠起来缺一格：个人知识仓（如 `agent-dev-playbook`）与 `relkit` 同为提供方源仓，但维护者对前者有 Git 写权限。现有路径只有：

1. 当官方仓：改 `DecAssets` + CI 发 registry（正确），却没有「我对这个提供方可以直接改源仓」的标记，Agent 容易改 cache 再 `dec_push`。
2. 当私仓正文：资产写进 `dec-source-private`，控制面与分发面混在一起。

「再开一层可写 Git 仓」或 `vault:<仓名>` 会把拓扑泄漏进消费声明，也把 personal 误当成资产正文仓。

## 决策

三层互不顶替。

1. **控制面（personal / `repo_url`）**：记录「要哪些」和「我对这个提供方怎么贡献」。项目 stub 是 `<name>/dec.yaml`。**不存放提供方资产正文。**
2. **分发面（registry）**：只读快照。`latest` / `v*` pin 只从这里安装。
3. **提供方源仓**：彼此平级（`relkit`、`AgentDevPlaybook`、…）。作者源仍是 `DecAssets/` + `provides`。CI `publish-provides` 写入 registry。

提供方身份（客观）与贡献方式（主观）分开：

- **客观**：registry 快照 `<project>/provider.yaml` 的 `origin_repo`（发布时从源仓 git remote 或显式字段写入）。
- **主观**：personal 里 `<project>/dec.yaml` 的 `access: direct | propose`。未写视为 `propose`。标记只选择工作流，不能绕过 Git/GCM/SSH。

消费声明不变，仍只有 `requires`：

```yaml
requires:
  agent-dev-playbook: latest
  relkit: latest
```

有写权限的提供方仍然 pin `latest`，不是 `vault`。

工作流：

- `access=direct` 且本机 `managed_projects` 有该源仓：改 `DecAssets/`、登记 `provides`，提交源仓，由 CI 发 registry。禁止把正文 `dec_push` 进 personal。`officialGitPushBlocked`（有 `provides` 则禁止 cache push）保留。
- 否则：`.dec/overrides/` + `dec_propose_upstream`（PR / Issue）。

`create_local_asset`：家项目或 direct 受管源仓写入 `DecAssets/`；官方且非 direct 写入 contribute 草稿；其余写入 cache（仅真正的个人 Git 资产）。

## 被否方案

- **`repos:` 平表 / `vault:<仓名>`**：消费声明再次带拓扑；每台机器重配；撞名规则复杂。
- **playbook 挂在 personal 之下当子仓**：personal 变成第二个资产仓，与「控制面」冲突。
- **把 `access: direct` 写进提供方源仓**：写权限因用户而异，不是提供方的客观属性。
- **direct 等于可以 cache push 进 personal**：正式格与个人笔记再次混写。
- **继续把 playbook pin 成 `vault`**：安装物不进 registry，跨工作区版本无法钉定，也与官方提供方不同级。
