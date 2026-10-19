# ADR 0037：dec_push 语义拆分与 dec_upload 引入

日期：2026-10-19
状态：已接受

## 背景

issue #48（承接 #45 事故复盘）指出：`dec_push` 同时承担「个人 Git 资产推回私仓」与「secrets 上传 Bitwarden」两个语义不同的上行流，而资产代码上行又叫 git push。下行却统一叫 `dec_pull`。这种不对称使「我已经 push 过了」在 Dec 语境天然歧义：#45 作者以为 push 过密钥，实际只推了代码，密钥流没动，pull 时密钥被当孤儿 prune 删除。

## 决策

1. **`dec_push` 语义收窄为仅个人 Git**：只推个人 Git 资产进私仓（实现复用 `PushWorkspacePersonalAssets`），不访问 Bitwarden。push 一词在 Dec 工具面只剩 Git 语义，与 git push 对齐。
2. **新增 `dec_upload` 承接 secrets 上行**：只上传本地 secrets 到 Bitwarden（实现复用 `PushWorkspaceSecretsOnly` / `push_secrets` 路由），不读写个人 Git 资产。上行本质是「上传到远端权威存储」，upload 不与 VCS 冲突。
3. **预览一一对应**：新增 `dec_preview_upload` 预览 secrets 上行（不写远端）；`dec_preview_push` 收窄为预览个人 Git 半。
4. **不留 alias，单一 release 一步到位**：理由见下。旧习惯调用者的落空由 `dec_push` 返回中的显式提示（`SecretsSkippedReason` = 仅推送个人 Git 资产；secrets 未包含，请用 dec_upload）兜底。
5. **下行保持 `dec_pull`**：下载方向没有歧义，不拆。

| 流 | 上行 | 下行 |
|---|---|---|
| 密钥（Bitwarden） | `dec_upload` | `dec_pull` |
| 个人 Git 资产 | `dec_push` | `dec_pull` |
| 资产代码（源仓发布） | git push → CI publish → registry | `dec_pull` |

## 为什么不留 alias 过渡

- alias 期间 `dec_push` 的行为语义模糊（旧混合行为还是新收窄行为），歧义没有真正消除，只是延后一个 release
- dec skill 为 embedded 随二进制分发，SKILL.md 与工具同 release 更新，不存在「旧文档配新二进制」的漂移窗口
- Agent 通过 MCP 工具列表动态发现工具，无硬编码调用方，改名即生效
- 省去后续移除 alias 的二次清理 release

## 影响

- Console 写回页操作标识（`push_personal` / `push_secrets`）不变，展示文案已分离（「写回个人资产」/「提交密钥」），与 MCP 工具一一对应
- `dec_push` 收窄后，历史上同时推两半的调用方需拆成两次调用（`dec_push` + `dec_upload`）；返回信息中的显式提示使该变更可被发现
- 文档全量同步：SKILL.md（dec / dec-extract-asset）、README、ARCHITECTURE.md、渲染头部注释
- #45 修复引入的生命周期条款同步改写为「先 `dec_upload` 上传」

## 后续

- issue #48 方案 B 按本 ADR 形态落地（拆分而非单纯改名）；方案 C（`dec_push_secrets`）不再需要
- release notes 需标注 breaking change 并给出迁移指引
