# 需求文档：dec_push 语义拆分与 dec_upload 引入（一步到位）

## 引言

承接 GitHub issue #48（Dec 同步的 push 与 git push 同名导致密钥/代码两流混淆，#45 事故的命名根因）。当前 `dec_push` 同时承担「个人 Git 资产推回私仓」与「secrets 上传 Bitwarden」两个语义不同的上行流，而资产代码上行又叫 git push，「我已经 push 过了」在 Dec 语境天然歧义。

本需求按一步到位策略实施拆分：单一 release 内完成工具面改名与语义收窄，不留 alias 过渡期。拆分后的工具语义：

| 流 | 上行 | 下行 |
|---|---|---|
| 密钥（Bitwarden） | `dec_upload` | `dec_pull` |
| 个人 Git 资产 | `dec_push` | `dec_pull` |
| 资产代码（源仓发布） | git push → CI publish → registry | `dec_pull` |

push 一词在 Dec 工具面只剩 Git 语义（与 git push 对齐），secrets 上行统一叫 upload。

**为什么不采用「两个 release + alias」**（相对前次评估结论的调整理由）：

1. alias 期间 `dec_push` 的行为语义模糊（旧混合行为还是新收窄行为），歧义没有真正消除，只是延后一个 release
2. dec skill 为 embedded 随二进制分发，SKILL.md 与工具同 release 更新，不存在「旧文档配新二进制」的漂移窗口
3. Agent 通过 MCP 工具列表动态发现工具，无硬编码调用方，改名即生效
4. 省去后续移除 alias 的二次清理 release

实现层成本可控：`PushWorkspacePersonalAssets`（仅个人 Git）与 `PushWorkspaceSecretsOnly`（仅 Bitwarden）在 `internal/app/push.go` 已是两个独立函数，拆分只动 `internal/agenttools` 注册层（tools.go 定义 + plan.go dispatch），不重写实现。

## 需求

### 需求 1：新增 `dec_upload` 工具承接 secrets 上行

**用户故事：** 作为 Agent 使用者，我希望有一个语义唯一的 secrets 上传工具，以便「上传密钥到 Bitwarden」与「推送 Git 资产」不再共用一个动词。

#### 验收标准

1. WHEN Agent 调用 `dec_upload`（plane=local|global|both） THEN 系统 SHALL 仅将本地 secrets 落地文件上传到 Bitwarden（复用 `PushWorkspaceSecretsOnly` 实现），不读写个人 Git 资产
2. WHEN 上传完成 THEN 返回信息 SHALL 列出 created / updated / skipped 明细计数
3. IF 目标为官方资产路径（provides 声明的工作区） THEN 系统 SHALL 沿用现有禁止规则拒绝写入
4. WHEN Agent 查看工具列表 THEN `dec_upload` 的 description SHALL 包含「上传 secrets 到 Bitwarden（Dec 同步），不是 git push，不发布任何资产到 registry」

### 需求 2：`dec_push` 语义收窄为仅个人 Git

**用户故事：** 作为 Agent 使用者，我希望 `dec_push` 只做 Git 资产推回，以便 push 在 Dec 语境与 git push 同义。

#### 验收标准

1. WHEN Agent 调用 `dec_push` THEN 系统 SHALL 仅推送个人 Git 资产进私仓（复用 `PushWorkspacePersonalAssets` 实现），不访问 Bitwarden
2. WHEN push 完成 THEN 返回信息 SHALL 显式包含「仅推送个人 Git 资产 N 项；secrets 未包含，请用 dec_upload」（防止旧习惯调用者静默漏推 secrets）
3. WHEN Agent 查看工具列表 THEN `dec_push` 的 description SHALL 明确「仅推个人 Git 资产进私仓；secrets 用 dec_upload；官方资产走 dec_propose_upstream」

### 需求 3：`dec_preview_push` 同步拆分

**用户故事：** 作为 Agent 使用者，我希望预览工具与执行工具一一对应，以便预览结果准确反映即将执行的动作范围。

#### 验收标准

1. WHEN Agent 调用 `dec_preview_push` THEN 系统 SHALL 仅预览个人 Git 半的变更范围
2. WHEN Agent 调用新增的 `dec_preview_upload` THEN 系统 SHALL 仅预览 secrets 上行变更（不写远端）
3. WHEN 任一 preview 被调用 THEN 其平面参数（plane）行为 SHALL 与对应执行工具保持一致

### 需求 4：文档与 UI 同 release 全量同步

**用户故事：** 作为 Dec 使用者，我希望升级后所有文档与界面用语一致，以便不再依赖旧表述猜测语义。

#### 验收标准

1. WHEN 发布包含本变更的 release THEN SKILL.md（dec 与 dec-extract-asset 两份）、README、ARCHITECTURE.md、相关 ADR SHALL 已全部替换为新工具名与新语义，不残留「dec_push 推 secrets」表述
2. WHEN 阅读 SKILL.md THEN 其 SHALL 包含上文「三个流」对照表
3. WHEN 查看 Console 写回页 THEN 两个动作按钮 SHALL 与 MCP 工具一一对应（个人资产写回 ↔ `dec_push`；密钥提交 ↔ `dec_upload`），文案不出现裸 push
4. WHEN 阅读 #45 修复引入的生命周期条款 THEN 其表述 SHALL 从「先 push 到 Bitwarden」更新为「先 `dec_upload` 上传」

### 需求 5：破坏性变更的显式管理（不留 alias）

**用户故事：** 作为存量用户，我希望旧行为落空时有显式提示，以便及时发现并修正调用习惯。

#### 验收标准

1. WHEN 旧习惯调用者经 `dec_push` 期望 secrets 被推送 THEN 需求 2.2 的返回提示 SHALL 使该落空可被发现
2. WHEN release 发布 THEN release notes SHALL 标注 breaking change 并给出迁移指引（「用 dec_push 推 secrets」→「改用 dec_upload」）
3. IF 存在直接依赖工具名清单的测试或集成点 THEN 它们 SHALL 在同一变更中同步更新并通过测试

## 边界情况

- `plane=both` 参数在两个工具中语义保持一致（local + global 双平面）
- SSH Key 相关工具（`dec_generate_sshkey` / `dec_import_sshkey` / `dec_install_ssh_key`）不改动，它们本就单语义
- Console 写回页内部操作标识（`push_personal` / `push_secrets`）不改动，仅核对展示文案
- TUI Run 页的兼容提示文案（`internal/app/operations.go` 中「禁止 Run 页 push / dec_push」）同步核对更新

## 成功标准

- 「我已经 push 过了」在 Dec 语境中唯一指向 Git 流；secrets 上行不再使用 push 一词（#48 核心诉求达成）
- 全仓检索无残留旧语义表述（文档、工具描述、UI 文案）
- 现有 secrets / Git 推送相关测试全部通过（实现层复用既有函数，改动集中于注册层）
