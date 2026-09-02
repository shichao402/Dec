# 0020 — 卸下 TUI，Console 为人机入口

- **状态**：已接受（已实施）
- **日期**：2026-09-01
- **关联**：[0008](0008-service-facade-split.md)、[0018](0018-instance-lock-and-console.md)、[0019](0019-remote-provisioning.md)
- **影响范围**：删除 `internal/tui/`；`dec` 无参不再启动 Bubble Tea；`.cursor/rules/console-first.mdc` 取代 `tui-first.mdc`

## 决策

终端 TUI 不再是用户面。人只使用 Dec Console（`client/`）。`dec` 保留 ` --version` 与内部 hidden 命令（`__freshness-check`、`__service-setup`）。`dec-mcp` / `dec-exec` / `dec-server` 职责不变。

连接本机或远端时，由目标侧检查并初始化四件套与服务；Console 不内嵌 `internal/app`。版本门闩与「只发 GUI zip」可在后续发布决策中补齐，不阻塞本次删除。

## 被否方案

**A. 保留 TUI 作为并行入口。** 否决：两套交互面必然漂移。

**B. 无参 `dec` 仍进 TUI，仅文档改口。** 否决：入口必须与决策一致。
