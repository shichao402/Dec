# 完成后自省

唯一权威闸门：

`python scripts/host/relkit_host.py retrospect`

退出码为 0 后，再运行 `python scripts/host/relkit_host.py status`，确认
`ops.retrospect` 为 `verified`。本文件只提供入口，不承载可跳过的检查清单。

`retrospect` 会读 `.relkit/cache/ops-journal.jsonl`：本次遇到 / 已修进脚本或 skill / 未消化。未消化不为空则退出码非 0。
