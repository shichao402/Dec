# 收尾入口

按 [`SKILL.md`](SKILL.md) 的「收尾」处理本次没有被现有自动化提前发现的问题：

- core、ops、agent 或引擎的缺口：在 relkit 仓修复，并补一个会在下次同类问题出现前
  失败的回归测试。
- 产品业务专属问题：修复与测试留在宿主仓。
- Skill 把人带错路：直接修 Skill；若同时暴露产品缺口，仍按前两条分别落地。

不在 CI 里加守卫脚本或登记表来“证明已消化”。上一代的
`conformance/production-escapes`、`relkit-ops escape audit` 与 ops 账本都已删除。
