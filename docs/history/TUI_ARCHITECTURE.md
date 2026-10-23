# TUI 已卸下

终端 TUI（`internal/tui/`）已按 [ADR 0020](decisions/0020-retire-tui.md) 删除。

人机入口是桌面 Console：见 [client/README.md](../client/README.md) 与 [.cursor/rules/console-first.mdc](../.cursor/rules/console-first.mdc)。

异步任务跟 Shell、不跟当前页的约定仍有效，实现在 Console 的 action registry（ADR 0018），不再有 Bubble Tea 对照实现。
