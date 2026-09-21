# 0031 — 个人私仓是纯控制面，提供方一律走上游贡献

- **状态**：已接受
- **日期**：2026-09-21
- **修订**：初版曾引入 personal `access: direct | propose` 与跨仓直写落点，当日废弃。见「被否方案」。
- **关联**：[0026](0026-project-provides-and-sync-worktree.md)、[0028](0028-official-registry-and-install.md)、[0029](0029-single-consumer-requires.md)
- **影响范围**：个人私仓项目 stub、registry 快照 `provider.yaml`、`create_local_asset` 落点、沉淀 Skill

## 问题

0026 把作者源定为提供方仓 `provides` / `DecAssets/`。0028 规定官方字节只经 CI 写入 registry，本机不得把 `provides` 项目 `dec_push` 进个人私仓。0029 用 `requires` 的 pin 区分 registry 与私仓。

个人知识仓（如 `agent-dev-playbook`）与 `relkit` 同为提供方源仓，差别只是维护者对前者有 Git 写权限。这让人想给它开一条捷径：在 personal 里标记「我能直写这个提供方」，Agent 据此改源仓 `DecAssets/`、跳过 PR。

捷径不成立。写权限是「这台机器上这个人此刻」的偶然属性：同一个提供方，有 clone 的机器能直写，没 clone 的机器只能提 PR，于是同一条沉淀指令在不同机器上落到不同地方。更糟的是落点降级只能静默——标记说 direct、本机却没有源仓时，写入会掉回 `~/.dec/cache`，正是 0028 要禁的位置。

## 决策

三层互不顶替，**没有第四条捷径**。

1. **控制面（personal / `repo_url`）**：只记录订阅哪些提供方，以及真正的个人 Git 资产与密钥归属。项目 stub 是 `<name>/dec.yaml`，只有 `name` / `title` / `tags` / `depends_on` 一类配置。**不存放提供方资产正文，也不记录贡献方式。**
2. **分发面（registry）**：只读快照。`latest` / `v*` pin 只从这里安装。
3. **提供方源仓**：彼此平级（`relkit`、`AgentDevPlaybook`、…）。作者源是 `DecAssets/` + `provides`，CI `publish-provides` 写入 registry。

提供方身份写在 registry 快照 `<project>/provider.yaml` 的 `origin_repo`，发布时从源仓 git remote 或显式字段取。消费方只读，用它定位上游仓。

消费声明不变，仍只有 `requires`。所有提供方都 pin `latest` 或精确 `v*`，没有「我有写权限所以特殊」的项目：

```yaml
requires:
  agent-dev-playbook: latest
  relkit: latest
```

**贡献口径只有一条**：改动进 `.dec/overrides/`，再由 `dec_propose_upstream` 提 Issue / PR，连同脱敏后的来源经验一并提交，由提供方仓统一整理合入。维护者自己有写权限时，在提供方仓里正常开 PR 或直接改——那是 Git 的事，Dec 不为此增加标记或落点分支。

`create_local_asset` 因此只有三个落点：当前工作区就是提供方家项目时写 `DecAssets/` 并登记 `provides`；当前工作区按官方 pin 订阅该项目时写 contribute 草稿；其余写 cache（仅真正的个人资产）。`officialGitPushBlocked`（有 `provides` 则禁止 cache push）保留。

## 被否方案

- **personal 标记 `access: direct` + 跨仓直写落点**：把「这台机器有没有 clone」变成语义的一部分；缺源仓时只能静默降级到 cache；personal 从配置面退化成半个工作流引擎。同一天引入并撤回。
- **`repos:` 平表 / `vault:<仓名>`**：消费声明再次带拓扑；每台机器重配；撞名规则复杂。
- **playbook 挂在 personal 之下当子仓**：personal 变成第二个资产仓，与「控制面」冲突。
- **direct 等于可以 cache push 进 personal**：正式格与个人笔记再次混写。
- **继续把 playbook pin 成 `vault`**：安装物不进 registry，跨工作区版本无法钉定，也与其它提供方不同级。
