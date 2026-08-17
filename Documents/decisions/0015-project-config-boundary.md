# 0015 — 项目配置的边界：用户平面没有 project，`.dec/` 不得落在 Dec 根目录

- **状态**：已接受（已实现）
- **日期**：2026-08-17
- **关联**：[0009](0009-bundle-binary-scope.md)（本决策是其平面隔离的直接推论）、[0012](0012-user-bundle-single-entry.md)（用户平面启用列表的唯一入口）
- **影响范围**：`internal/config/project.go`（`ProjectConfigManager` 全部读写入口）、`internal/app/vault_project.go`、`internal/tui/model.go`（overview 后的推断）

## 问题

`dec --user` 的 `Workspace.Root` 是空串——用户平面本就没有工作区。但 `ProjectConfigManager` 直接 `filepath.Join(projectRoot, ".dec")`，空 root 会退化成**相对路径** `.dec`，实际写到哪里取决于 `dec-server` 的 cwd。而服务是被第一个门面拉起的，cwd 继承自那次 `dec` 的运行目录。

当 cwd 恰好是 Dec 根目录的父目录（默认即 `~`）时，`.dec/config.yaml` 和全局配置 `~/.dec/config.yaml` **是同一个文件**。全局配置没有 `version` 字段，被当作项目配置读取时命中 v1→v2 升级分支并就地回写，于是 `repo_url` 与 `enabled_bundles` 被丢掉，只留下两边同名的 `ides`。

真实事故链：用户平面 Bundles 页保存成功写入 `enabled_bundles` → overview 加载完成后 TUI **无条件**发起 vault project 推断 → `NeedsVaultProjectAutoApply("")` 读「项目配置」→ 全局配置被改写 → Run 页 pull 读到空启用列表，报「未启用 bundle」。用户看到的现象是「明明勾了并保存了，pull 却说没选」。

根因不是那次推断调用本身，而是**空项目根被静默当成 cwd 处理**：任何一处漏掉平面分流，都会以「覆盖全局配置」的形式爆炸，且没有任何报错。

## 决策

### 1. 用户平面没有项目配置

不读、不写、不推断。用户平面的启用列表只在 `~/.dec/config.yaml` 的 `enabled_bundles`（0009 第 6 条、0012），`project_name` / `.dec/vars.yaml` / vault project 归属等概念在该平面**不存在**。

具体地，`InferVaultProject` 与 `NeedsVaultProjectAutoApply` 对空 `projectRoot` 直接报错而非返回「需要推断」；TUI 在用户平面不再发起推断命令。

### 2. 项目配置要求真实的项目根

`ProjectConfigManager` 的读写入口（`LoadProjectConfig` / `SaveProjectConfig` / `EnsureVarsConfigTemplate` / `Exists`）一律先校验项目根，两类直接拒绝：

| 情况 | 处理 |
|------|------|
| `projectRoot` 为空 | 返回 `ErrProjectRootRequired`；`Exists()` 返回 false |
| `.dec/` 解析后等于 Dec 根目录（`DEC_HOME`，默认 `~/.dec`） | 报错并点明「项目配置会覆盖全局配置」 |

第二条同时挡住了在 `~` 下当项目根跑 `dec` 的情况——那和空 root 撞的是同一个文件。

### 3. 越界必须显式失败

不做「猜一个合理默认」的降级。调用方传空 root 是**调用方的 bug**，报错才能让它在开发期暴露；静默落到 cwd 只会把 bug 变成数据损坏。

## 被否方案

**`dec-server` 启动时 chdir 到固定目录。** 只是把事故挪个位置：cwd 换成 Dec 根目录后 `.dec/config.yaml` 变成 `~/.dec/.dec/config.yaml`，不再覆盖全局配置，但相对路径依赖与「空 root 被静默接受」两个问题都还在，下一个漏掉平面分流的调用点照样写出一份没人读的幽灵配置。

**空 root 回退到 cwd（现状）。** 这正是事故本身。它让「忘记按平面分流」这类错误不产生任何可观测信号。

**给全局配置补 `version` 字段，避免被误认成 v1 项目配置。** 治标。它只堵住 v1→v2 升级这一条误判路径，`SaveProjectConfig` 仍然可以往全局配置的位置直接写；而且要求两种配置的版本字段语义永久保持兼容，是个无谓的耦合。

**只在 TUI 修，不加 config 层守卫。** 门面不止 TUI（还有 `dec-mcp`），调用点也会继续增加。守卫放在唯一的写入实现里才有覆盖率，放在调用方等于每新增一处就赌一次。

## 参考

- 事故现场：`~/.dec/config.yaml` 被写成带 `version: v2` 的项目配置格式，`repo_url` / `enabled_bundles` 丢失
- 回归测试：`internal/config/project_root_guard_test.go`、`internal/app/user_plane_global_config_test.go`
