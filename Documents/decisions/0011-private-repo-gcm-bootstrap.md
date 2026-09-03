# 0011 — 私仓 GCM Bootstrap：Bitwarden 作为启动信任根

- **状态**：已接受（已实现）
- **日期**：2026-08-17
- **关联**：[0005](0005-secrets-machine-handlers.md)、[0008](0008-service-facade-split.md)、[0009](0009-bundle-binary-scope.md)
- **补充约束**：[0013](0013-secrets-belong-to-declared-target.md) — 查找范围不变（仍扫全部 folder），但候选位于非托管裸 folder 时必须提示「不属于任何 bundle，pull 不维护，轮换需重新 bootstrap」
- **认证补充**：[0022](0022-console-bitwarden-unlock.md) — 人工认证统一由 Console 承载
- **影响范围**：Settings 仓库连接、同步页 pull、Bitwarden 认证、`.gcm` Processor、service progress stream

## 问题

Dec Git Vault 可以是 HTTPS 私仓；其凭证以 Bitwarden Secure Note（`.gcm/*`）管理并由
GCM Processor 写入系统 Git Credential Manager。首次连接新机器时形成环依赖：

```text
拉取 Dec 私仓
  → 需要 GCM 凭证
  → GCM Note 的正常发现依赖仓内 bundle manifest
  → 必须先拉取 Dec 私仓
```

把凭证改成 SSH 并不能消除这个问题：如果 SSH Key 同样依赖该私仓中的声明，环仍存在。

凭证**过期**是同一环的日常形态：仓库早已连接，但 `git fetch` 因旧 token 失败；此时
Settings 不再 probe（URL 未变），若不在 Run 页 pull 失败路径补入口，用户会误以为
「手里有新 GCM 也无法用」，只能手工 `git credential approve`。

## 决策

新增一条 **Repo GCM Bootstrap 特殊编排**。它不引入新凭证类型或第二份 token，只在
明确发生 HTTPS 认证失败后启用。触发入口：

- **Settings**：首次连接 / 更换 Repo URL 时的探测失败。
- **Run 页 pull**：`FetchBare`（`git fetch`）认证失败；成功 Apply 后自动重试 pull。

流程：

1. 用禁交互的 `git ls-remote` / `git fetch` 探测仓库；网络、DNS、地址错误按原错误返回。
2. 仅 HTTPS 认证失败时，错误携带稳定标记 `[dec:repo-auth-required]`（跨 RPC 后仍可判定），
   Console 明确询问用户是否从 Bitwarden 查找 GCM。
3. 用户确认后，由 `dec-server` 复用现有 Bitwarden session；缺 session 时按 0022 进入
   Console Authenticate。
4. 不读取 Git bundle manifest；直接枚举 Bitwarden folder 中名字匹配 `.gcm/*` 的
   Secure Note，逐条只在服务进程内解密，并按正文 `host` 匹配 repo host。
5. Console 仅收到 folder、Note 路径、host、username；**正文/password 永不经 RPC 返回**。
6. 用户选择候选后，服务重新读取 Note、复核 host，调用现有 `GCMHandler.Apply`，
   再次 `git ls-remote` 验证；成功后按来源重试 Settings 保存或 Run pull。

若 Bitwarden 尚无匹配 Note，用户应先到 **Remote 页**登记 `.gcm/*`（登记不依赖私仓
可达），再回到确认步骤。Bootstrap 查询与 Apply 使用现有流式 operation RPC；本机交互
MCP 缺 session 时由认证协调器拉起/聚焦 Console 并等待，远端无桌面或 CI 返回结构化错误。

## 安全与生命周期约束

- Bootstrap 只由用户确认触发，不因任意 Git 错误自动读取 Bitwarden。
- 仅明确的 HTTPS 认证错误可进入流程；SSH、DNS、超时、证书错误不进入。
- 不在 `~/.dec/config.yaml`、环境变量、日志或 RPC 结果中保存/回传 token。
- Bitwarden session 仍只在 `dec-server` 内存；GCM 副作用仍由 0005 的 Processor 管理。
- 仓库连通后回到正常 bundle pull；Bootstrap 不改变 bundle scope、SyncTarget 或 pull 语义。
- 多候选必须由用户选择；零候选只报告，不创建占位 Note。

## 理由

Bitwarden 认证本身不依赖 Dec Git Vault，因此可以作为启动信任根。复用同一份 `.gcm`
Note 与同一个 Handler 能打破环依赖，同时避免维护一套“bootstrap token”配置。

## 被否方案

### A. 要求用户先在系统外手工配置 GCM

可行但体验差，且 Dec 已拥有安全读取和 Apply 同一 Note 的基础能力；保留为故障恢复手段，
不作为主流程。

### B. 在本机配置中保存 repo token

否决：产生第二份秘密、增加持久化攻击面，并违反 secrets 只在 Bitwarden / 进程内的约束。

### C. 建立专用 bootstrap folder / 复制一份凭证

否决：同一 token 双份存储会漂移。按 host 扫描现有 `.gcm/*` 即可。

### D. 任意连接失败都自动解锁 Bitwarden 并 Apply

否决：会把网络/DNS/地址错误误判为认证问题，也会在未经确认时读取秘密并产生机器副作用。

### E. 改用 SSH

否决为通用解法：只改变凭证类型，不消除自举依赖；HTTPS/GCM 仍是 CNB 等服务的实际需求。
