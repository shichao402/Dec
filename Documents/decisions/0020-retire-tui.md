# 0020 — 卸下 TUI，Console 为人机入口

- **状态**：已接受（已实施）
- **日期**：2026-09-01
- **关联**：[0008](0008-service-facade-split.md)、[0018](0018-instance-lock-and-console.md)、[0019](0019-remote-provisioning.md)
- **影响范围**：删除 `internal/tui/` 与根 `dec` CLI；`.cursor/rules/console-first.mdc` 取代 `tui-first.mdc`

## 决策

终端 TUI 和根 `dec` CLI 均不再存在。人只使用 Dec Console（`client/`）。每个运行时程序自行支持 `--version`；目标机的一次性配置由单用途 `dec-host-setup` 承担，Agent 与 env 注入仍分别使用 `dec-mcp`、`dec-exec`。

连接本机或远端时，由目标侧检查并初始化运行时套件与服务；Console 不内嵌 `internal/app`。版本门闩与「只发 GUI zip」可在后续发布决策中补齐，不阻塞本次删除。

## 被否方案

**A. 保留 TUI 作为并行入口。** 否决：两套交互面必然漂移。

**B. 无参 `dec` 仍进 TUI，仅文档改口。** 否决：入口必须与决策一致。

**C. 保留一个看似通用的 `dec` 只做版本探测和内部命令。** 否决：每个程序应自报版本；置备配置应由名称和职责明确的单用途工具完成。
