# Production escape 收尾入口

按 `SKILL.md` 的「收尾」处理本次现有自动化未提前发现的问题。

- 可跨宿主复现的 core/wrapper/ops 缺口进入 Relkit 中央
  `conformance/production-escapes`，并关联真实可执行回归。
- 产品业务专属问题与测试留在宿主仓。
- Agent 误判修正 Skill；若同时暴露产品缺口，仍按前两类分别落地。

不得再用 `.relkit/cache/ops-journal.jsonl`、`DIGESTED_ISSUE_CODES` 或
`ops.retrospect=verified` 作为 vNext 完成证据。中央记录、候选认证和 stable release
门禁均由 `relkit-ops` 执行；本文件不复制 schema 或判定逻辑。
