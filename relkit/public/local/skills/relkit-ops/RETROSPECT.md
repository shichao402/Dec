# 完成后自省

自省分两段，缺一不可：

1. 按 `SKILL.md` 的「完成后」重读本次对话与人工验收，处理用户纠正、成功命令之后
   才暴露的问题，以及 Agent 对范围的误解。
2. 运行 `python scripts/host/relkit_host.py retrospect`；退出码为 0 后再运行
   `python scripts/host/relkit_host.py status`，确认 `ops.retrospect` 为 `verified`。

机械命令只看文件和 `.relkit/cache/ops-journal.jsonl`，看不到对话；命令通过不能替代
第一段。反过来，只做会话总结也不能替代机械命令。本文件只提供入口，不复制检查清单。
