# 0009 — Bundle 二元 scope（user | project）

- **状态**：已接受
- **日期**：2026-08-14
- **关联**：[0002](0002-secrets-synctarget-root.md)、[0003](0003-user-enabled-secret-bundles.md)、[0005](0005-secrets-machine-handlers.md)、[0007](0007-machine-secrets-root.md)、[0008](0008-service-facade-split.md)
- **影响范围**：`bundle.yaml`、`GlobalConfig`、`internal/secrets` SyncTarget / env、`internal/ide`、`internal/app` pull/push/delete、`internal/tui`、`dec --user`、`Documents/BUNDLE-SECRETS-MODEL.md`
- **取代 / 修订**：[0003](0003-user-enabled-secret-bundles.md) 的并集语义；[0007](0007-machine-secrets-root.md) 的项目覆盖层

## 问题

启用平面与落地平面可自由组合，四种组合里两种是坏的：

1. **machine 落地 + project 启用**：勾选写在 `<project>/.dec/config.yaml`，效果却是整台机器的（`git config --global`、`git credential approve`、`~/.ssh/`）。pull 不做停用清理；`gitgcm` 无撤销路径。声明作用域与实际作用域不一致，且不可逆。
2. **project 落地 + user 启用**：pull 传并集、push 传纯项目列表，同一 bundle 落进 `ResolveSyncTargets` 不同分支，overlay 在 push 时消失，项目内改的 token 推不回远端。

覆盖语义只存在于 env 平面（`LoadEnvForBundle` 三层按 key 合并）；文件直读平面没有任何合并——一个路径只有一份字节。user 级公开资产实际仍装进项目内 IDE 目录，「user」名不符实。

## 决策

### 1. Bundle 声明二元 scope

`bundles/<name>/bundle.yaml` 增加必填字段：

```yaml
name: tencent-cloud
scope: user          # user | project
description: ...
members: [...]
```

判据是**凭据归属**：属于「我这个人的身份」→ `user`（git credential、SSH、个人云账号）；属于「这个项目的身份」→ `project`。不按自建 / 第三方划分。

横跨两边的能力拆成两个包（如 `deploy-ssh` user + `deploy-api` project），不在 Dec 层做 overlay。

### 2. 两平面落地完全对称

| | user bundle | project bundle |
|--|-------------|----------------|
| skill / rule / command | `~/.cursor/skills/` 等 | `<project>/.cursor/skills/` |
| MCP | `~/.cursor/mcp.json` | `<project>/.cursor/mcp.json` |
| secrets | `~/.dec/secrets/bundles/<name>/` | `<project>/.secrets/bundles/<name>/` |
| 启用列表 | `~/.dec/config.yaml` 的 `enabled_bundles` | `<project>/.dec/config.yaml` 的 `enabled_bundles` |

`ResolveSyncTargets` 的 `both` 分支改为**报错**。删除 `NewProjectBundleOverlayTarget`、`NoteNamePrefix`、`NoteNameExcludePrefixes` 与项目覆盖层协议。

多账号不由 Dec overlay 承担：凭据多份放机器层，选择权由 MCP 在运行时读项目内非敏感配置（`${workspaceFolder}` + cwd）。

### 3. 平面隔离

用户级上下文（`dec --user`）只访问用户平面；项目级上下文只访问项目平面——**适用于 Bundles / pull / push / env / 启用列表**。

- 删除 `mergeProjectAndUserEnabledBundles` 与并集语义；project pull 只处理 `scope: project` 的包。
- `LoadEnvForBundle` 降为按平面单层：user bundle 只读机器层；project bundle 只读项目层。删除「`.secrets/project/env` 覆盖 user bundle」逃生口。
- ~~Bundles / Remote / delete 候选按上下文过滤。~~ **修订（2026-08-16，方案 R）**：Remote 库存**不再**按上下文过滤；`scope` 在 Remote 中仅为分组标签。平面隔离保留在 Bundles / pull / push / env / 启用列表。见 [0004](0004-remote-page.md) 修订。
- RPC 显式携带 scope，不用哨兵 `projectRoot` 伪装。

**不隔离**：Bitwarden session（`dec-server` 进程内存）与 `~/.dec/secrets/device.json` 设备信任继续共享——它们是认证，不是资产可见性。

### 4. `dec --user`

把 TUI 工作空间切到用户平面，复用同一套 Bundles 勾选、pull、push、Remote、cleanup。启用与停用在同一处，用户级 IDE 资产清理自然覆盖。

写操作与项目平面对称，均按平面解析落点：

| 操作 | 用户平面读写位置 |
|------|----------------|
| pull / push（Dec 资产） | `~/.dec/cache/<bundle>/` ↔ vault `bundles/<name>/` |
| push（secrets） | `~/.dec/secrets/bundles/<name>/` → Bitwarden `bundle/<name>` |
| Remote 删除 / 编辑 | **全量远端库存**（不按 `scope: user` 过滤）；本地清理走独立「本地」分区（`~/.dec/cache`、`~/.dec/secrets`、`~/.ssh` 等） |
| 启用列表变更 | `~/.dec/config.yaml` 的 `enabled_bundles` |

**Project 页不在用户平面开放**：它编辑的是 `.dec/vars.yaml` 项目变量，用户平面没有对应概念。

IDE 列表跟全局配置走，不走项目优先的 `ResolveEffectiveIDEs`。

不能简单把 `homeDir` 当 `projectRoot`：`claude-internal` / `codex-internal` 的 `dirKey` 与 `userDirKey` 不同名。IDE 接口增加显式平面概念。

映射约束：

- 用户平面启用列表落在已有 `~/.dec/config.yaml`（`GlobalConfig.EnabledBundles`），不新建配置文件，也不再放 `~/.dec/secrets/config.yaml`。
- 用户平面 secrets 映射到 `~/.dec/secrets/`，不是 `~/.secrets/`。
- 用户平面跳过 `EnsureSecretsGitignore` 与 `validateNotGitTracked`。

### 5. MCP 占位符

两平面都保留 `${workspaceFolder}`，由 IDE 展开。删除 `WrapMCPServerWithExec` 里无条件替换为绝对路径的逻辑。`--project-root` 同样写占位符。

### 6. 启用列表归位

`user_enabled_bundles` 从 `~/.dec/secrets/config.yaml` 迁到 `GlobalConfig.EnabledBundles`（yaml tag `enabled_bundles`），与 `ProjectConfig` 字段同名。`known_secret_bundles` 与 `Bundles []BundleBinding` 留在 secrets 配置。

迁移照 `loadLegacyLocalIDEs` 模式：新字段为空时读旧位置，保存成功后清理旧字段。

### 7. 既有 vault 迁移

按当时本机 `user_enabled_bundles` / 新 `enabled_bundles` 推断一次并写回 vault：在列表里的写 `scope: user`，其余写 `project`；迁移后 `scope` 必填。避免已在 user 级使用的包（如 tencent-cloud）secrets 从机器层掉回项目层。

## 一并处置的缺陷

- push / pull 传参口径不一致导致 overlay 推不回去（随 overlay 删除消失）。
- `gitgcm` 无撤销路径：停用 / 删除时需 `credential reject` 与清理对应 `git config --global`。
- `delete.go` 对机器平面用 `projectRoot` 拼 `LocalRoot`：改走 `ResolveAbsDir`。
- `installBuiltinMCPs` 对 `claude-internal` / `codex-internal` 写错用户级 MCP 路径：随 IDE 平面概念一并修复。

## 理由

- 启用平面与落地平面由 bundle 内在性质唯一决定，禁止构造生命周期不一致的状态。
- 两平面对称后，「user」名实相符；IDE 用户级安装通路已存在（内置资产），只需开放给 vault bundle。
- 平面隔离取消并集复制，IDE 自己同时看见用户级与项目级目录。
- 删除 overlay 与三层 env 合并，文件直读与 env 注入语义一致，且 push/pull 自然对称。

## 被否方案

**A. 保留 machine + project overlay（0007）。**  
否决：启用 / 落地错位；文件平面无法合并；push 口径 bug 结构性存在。

**B. 仅加 `secrets_scope`，公开资产仍并集装进项目。**  
否决：user 名不符实；平面隔离要求公开资产也分平面落地。

**C. 物化机器层文件到项目内再覆盖。**  
否决：需要 origin manifest，推翻 [0001](0001-secrets-landing-path.md)「不引入 state.json」；副本漂移回潮。

**D. 路径搜索协议（`DEC_SECRETS_DIRS`）代替平面隔离。**  
否决：可作未来增强，但不解决启用 / 落地生命周期错位；第三方不配合则无效。本 ADR 先定二元 scope + 隔离。

**E. 用户平面启用列表单独新建配置文件。**  
否决：与「两平面只是位置不同」冲突；`GlobalConfig` 已是全局启用 / IDE 的自然落点。

## 参考

- `Documents/BUNDLE-SECRETS-MODEL.md`
- `internal/assets/mcp/dec.json`（`${workspaceFolder}` 先例）
- `internal/app/settings.go` `InstallBuiltinAssetsForIDE`（用户级安装先例）
