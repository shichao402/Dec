# 0010 — Pull 孤儿收敛、删除收敛与运维面修订

- **状态**：已接受（已实现）
- **日期**：2026-08-16
- **关联**：[0002](0002-secrets-synctarget-root.md)、[0003](0003-user-enabled-secret-bundles.md)、[0004](0004-remote-page.md)、[0008](0008-service-facade-split.md)、[0009](0009-bundle-binary-scope.md)
- **影响范围**：pull reconcile、Remote/delete、Settings 本机 vars、vault 资产目录清单、`dec-server` 版本探测与重启

## 问题

方案 R 与并行修复暴露出几处文档/行为缺口：

1. 本地 secrets / SSH / cache 孤儿在远端已删后仍残留，或反过来「无法确认远端」时误删本地
2. 删除远端后 `projects/*.yaml` / `known_secret_bundles` / `enabled_bundles` 残留导致 push「幽灵复活」
3. Remote 浏览误写 `known_secret_bundles`
4. Settings 本机用户级 vars 缺少与 Project vars 一致的外部编辑路径
5. skill/command/rule/mcp 目录清单在多处手写，易分裂
6. 门面与 `dec-server` 二进制版本不一致时缺少明确引导

## 决策

### 1. Pull 孤儿 reconcile

- **只对**本次启用且 Bitwarden / vault **远端对照成功**的集合自动清理本地孤儿 Note/SSH（及 Git cache / IDE 安装产物按目标集清理）
- 停用包、解锁失败、列表失败等**无法确认**远端权威时：**只报告不删**
- 落点：`internal/app/pull_reconcile.go`，由 pull 编排调用

### 2. 删除收敛

远端删除 bundle / secrets 时尽量：

- 摘 vault `projects/*.yaml` 中残留 bundle 声明
- 清本机 `known_secret_bundles` 与对应平面 `enabled_bundles`
- 按删除 Mode 清本地 secrets 同步根 / cache（远端-only 不默认清本地盘，本地分区才清）

Remote **浏览**库存**不得** `RememberSecretBundles`（与 pull 发现写入 known 区分）。

### 3. Settings 本机 vars

- 用户级 vars（`~/.dec/local/vars.yaml`）在 Settings 按 `e` 走外部编辑器，语义对齐 Project 页 `.dec/vars.yaml`
- 仍禁止 TUI 内嵌多行编辑

### 4. VaultAssetKinds 共用真相源

- `internal/bundle.VaultAssetKinds` 为 skill / command / rule / mcp 目录与类型的唯一清单
- vault 扫描、Bundles、Remote、cache 清理均引用该表，禁止再复制目录名切片

### 5. 服务版本 mismatch + 重启

- 门面探测 `dec-server` 版本；不一致时 Settings / 状态栏提示
- Settings 可确认后重启本机 `dec-server`（普通停止后由下次门面拉起）；测试环境不得自动拉起 Console，非交互 Agent 缺认证时返回结构化错误（[0022](0022-console-bitwarden-unlock.md)）
- 重启会清空进程内 Bitwarden session——属预期

### 6. secrets 全量可见口径

- `ListAllFolderNames` / Remote inventory 使用同一「全量 folder」口径；Remote 登记归属直接取自该库存渲染出的树（光标所在 folder），不再单独枚举候选
- 与 Bundles/pull 的平面过滤分离（见 [0004](0004-remote-page.md)、[0009](0009-bundle-binary-scope.md)）

## 被否方案

| 方案 | 否决理由 |
|------|----------|
| 无法确认远端时仍 prune 本地 | 误删不可恢复的本机密钥 |
| 浏览 Remote 也 Remember known | 把「看见」变成「登记」，污染启用候选 |
| TUI 内嵌编辑本机 vars | 与 Project vars / Remote 编辑决策冲突 |
| 各模块各自维护资产目录列表 | 漏扫 commands 等已发生过的分裂 |

## 后果

- 文档：`BUNDLE-SECRETS-MODEL.md` pull 段、`TUI_ARCHITECTURE.md` Settings/Remote、`.cursor/rules/bundle-secrets-mirror.mdc` 与实现对齐
- 测试：`pull_reconcile_test.go`、delete 收敛相关、`vault_assets_test.go`、server restart TUI 测试
