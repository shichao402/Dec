# 0028 — 官方注册表、安装器与个人私仓

- **状态**：已接受；消费声明段被 [0029](0029-single-consumer-requires.md) 细化（`requires` 同时承载官方 pin 与私仓 `vault` pin，是唯一消费声明）
- **日期**：2026-09-16
- **关联**：[0016](0016-p-four-quadrant-model.md)、[0023](0023-facade-capability-parity.md)、[0026](0026-project-provides-and-sync-worktree.md)（发版与本机推柜段被本决策取代）、[0027](0027-relkit-owned-update-contract.md)
- **影响范围**：`internal/registry`、`internal/publish`、`internal/install`、`internal/contribute`、`cmd/dec-registry`、消费仓 `.dec/config.yaml` 的 `requires`

## 问题

官方投影曾写入用户私仓，本机 Console / cache push 也能改「正式格」。这让消费端版本无法按工作区拆开，也让 CI 与人工写同一份远端。

## 决策

三套存储互不顶替：

1. **官方注册表**：Dec 仓库 orphan 分支 `registry`，tag `registry/<项目>/<提供方版本>`。
2. **个人私仓**：`repo_url`。只放个人 Git 资产。
3. **密钥**：Bitwarden。不进任何 Git。

消费声明只有一张 `requires` 表（项目 → `latest` 或精确 `v*`），写在 `.dec/config.yaml` 或本机 `~/.dec/config.yaml`。没有 pin 字段，没有 semver range，不再接受 `requires: [relkit]` 列表。

`install` 是纯函数：按 `requires` + 个人启用列表重画 IDE 目录。`.dec/cache` 只按 tag 下载，改它无效。已发布 tag 的字节不可改；撤回用 yank / purge，只由提供方 CI 执行。

提供方用非用户面 `dec-registry publish-provides`。Dec 发版把它单独构建到
`dist/ci/dec-registry-<os>-<arch>`，stable GitHub Release 挂载该工具；它不属于
RUP runtime，也不进入 Console resources / `~/.dec/bin`。提供方 CI 使用
`.github/actions/publish-provides`，以 deploy key 或 token 写 registry。

Console / MCP 不写官方注册表。消费方改官方安装物时，从具体项目页进入下级页「本地覆写」，建立
`.dec/overrides/<提供方>/<类型>/<资产>/` 并关联源仓 PR 或 Issue；
票据关闭/合并且 registry 出现新版本后，用户在「更新」页确认安装，新正式版替换并删除已解决覆写。

Console「更新」只做远端到本地：跨 Global / 项目多选官方 `requires`，预览后安装。
个人 Git 与 Bitwarden 的写回位于对应项目页或 Global 资产页，不与更新混为双向同步。

`scripts/relkit.lock.json` 是 relkit 产品锁，Dec 不读它来填 `requires`。跟宿主版本耦合的资产必须 `public/local`，不得放在 global。

## 被否方案

- 官方投影写入用户私仓：消费机无法并存两套官方版本。
- 另开第二个 GitHub 仓当注册表：多一个仓库、两套权限。
- 官方 tag 不带 `registry/` 前缀：与 Dec Console 产品 tag `v*` 冲突。
- 本机 / Console 写注册表：正式格再次变成可写副本。
- 迁移层与双写：硬切，不留兼容入口。
