# `.agent/pull-prune-safety/` 实施记录

> 对应需求文档：`.agent/pull-prune-safety/requirements.md`
> 行为变更完成时点：2026-10-20

## 这条需求解决什么问题

`dec_pull` 之前会直接把「远端已不存在」的本地密钥文件永久删除。本地镜像没有回收站，
用户手工维护的内容会在 pull 后静默消失（issue #45/#47/#24）。

本实施把 pull 本地剪枝从「永久删除」改为「可预知、可恢复、可追溯」的三道安全边界。

## 核心行为变化

1. **默认不再永久删除**：远端确认无的本地孤儿条目，全部移入隔离区
   `~/.dec/pruned/`（可配置 `DEC_PRUNE_QUARANTINE_DIR`），而非 `os.Remove`。
   - 单条移入是原子操作（rename）。
   - 隔离区不可用时**中止剪枝并报错**，绝不退化为硬删（`mustPruneQuarantineStore`）。
2. **只删「可重建」条目**：判定依据是路径形态是否属于远端派生的 note 落盘
   （`.env` 注入源、`.gcm` 等 handler 托管、普通 plain note）。`files/` 等
   非 note 类型的散件与未知点目录默认保留，防误删手工内容。
3. **隔离区可恢复**：`dec_restore_quarantine`（单条/整批）恢复到原路径；目标
   已存在则不覆盖并报告冲突；超过保留期（默认 `DEC_PRUNE_RETENTION`）不可恢复；
   超过容量上限（`DEC_PRUNE_MAX_BYTES`）按最早剪枝优先清理。
4. **删前可预知**：`dec_preview_pull` 只读返回本次将隔离的逐条路径、理由、
   「是否可从远端重新拉回」，无副作用；无待删时给出明确「无待删条目」结论。
   `dec_list_quarantine` / `dec_summarize_quarantine` 供回看与状态查询。
5. **严格一致模式**（`dec_pull strict=true`）：只影响显式要求的用户，把默认保护
   的条目也移入隔离区，令本地成为严格镜像；仍走软删，不行使任何硬删。

## 关键文件

| 文件 | 职责 |
| --- | --- |
| `internal/prune/quarantine.go` | 隔离区基础库：Move/List/Restore/RestoreAll/PurgeExpired/EnforceCapacity，自带 manifest 持久化 |
| `internal/app/prune_decision.go` | 纯函数式决策器 `DecideTargetPrune`（只读），`PruneScope` 支持范围与 strict |
| `internal/app/prune_executor.go` | 执行器：逐条移入隔离区，失败不影响其余，ctx 取消时标识中止 |
| `internal/app/pull_reconcile.go` | pull 收敛流程：隔离区可用性保护、handler 惰性条目先 revoke 再进隔离区 |
| `internal/app/pull_preview.go` | `dec_preview_pull` 只读预览 |
| `internal/app/restore_quarantine.go` | 隔离区清单/状态/恢复查询 |
| `internal/servicehost/dispatch.go` | 新操作接入（preview_pull / list_quarantine / restore_quarantine / summarize_quarantine）；pull 解析 strict |
| `internal/agenttools/tools.go` | 新 MCP 工具注册（dec_preview_pull / dec_list_quarantine / dec_restore_quarantine / dec_summarize_quarantine） |

## 需求满足情况速览

| 需求 | 交付 |
| --- | --- |
| 1 删前可预知 | `dec_preview_pull`（明细+理由+可拉回标注；无副作用；范围过滤；执行阶段重校验） |
| 2 可重建才可删 | note 落盘类型可删；files/散件与未知来源保留并列出理由 |
| 3 可恢复 | 隔离区软删 + `dec_restore_quarantine`（冲突不覆盖、保留期、容量清理） |
| 4 可追溯 | 全部事件保留、Agent 返回逐条明细、操作 history 可回看、文案不写密钥值 |
| 5 中断并发 | 单条 rename 原子、ctx 中止标识、同一项目并发拒绝、失败不影响其余 |
| 6 收敛入口复用 | 聚合动作复用同一决策/隔离机制，不绕过语义判定 |
| 7 命名约定 | dec_preview_pull / dec_restore_quarantine 沿用 dec_* 动词与分层返回 |
| 8 兼容行为 | 既有 dec_pull 默认宽松复用；新增 strict 显式参数；结果/状态体现隔离区 |
| 9 可验证 | 测试覆盖免删场景（TestDecideNoteKeepsLocalOnlyContent）、超时无丢失（TestPruneExecutorHonorsContextCancellation）、隔离/恢复/状态全链路 |

## 安全边界（禁止退化）

- **任何降级路径都不得退化为硬删除**：隔离区不可写/容量超上限/恢复冲突，均显式报错并保留原文件。
- **strict 不是硬删的通道**：strict 只是把「默认保留」的条目也纳入隔离软删，仍可恢复。
