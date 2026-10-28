---
状态: 已接受
日期: 2026-10-28
关联: [0050](0050-mcp-entry-must-nest-in-mcpservers.md)、[0055](0055-mcp-entry-renamed-dec-vault.md)、[0056](0056-codebuddy-user-mcp-path.md)
影响范围: 资产落点表（internal/agent/host.go）
---

# Claude 的用户级 MCP 落点改为 ~/.claude.json，项目级改为项目根 .mcp.json

## 背景

host.go 的 claude 条目只给了 `userDir: ".claude"` 和 `projectDir: ".claude"`，没有写
`userMCP` / `projectMCP`，于是两级 MCP 都按默认规则解析成 `<目录>/mcp.json`：

- 用户级落到 `~/.claude/mcp.json`
- 项目级落到 `<项目根>/.claude/mcp.json`

Claude Code 两者都不读。它的三个作用域里，本地与用户作用域都在 `~/.claude.json`，
项目作用域在项目根的 `.mcp.json`；`~/.claude` 只是数据目录（skills/rules 在那里）。

2026-10-28 在本机核对：`~/.claude.json` 的 `mcpServers` 里只有 `dec_vault`（代理启动时
写的那条），而云端下发的 `dec-gongfeng-mcp` 落在 `~/.claude/mcp.json`。文件合法、
哈希对得上、drift 归零、代理回报「已写入」，唯一症状是那条 MCP 在 Claude 里永远不出现。
这是 ADR 0050（写在顶层而非 mcpServers）和 ADR 0056（CodeBuddy 误用项目级 `.mcp.json`
到用户级）的同一类静默失败。

## 决策

1. host.go 的 claude 补上两级显式落点：`userMCP: ".claude.json"`（相对用户目录）、
   `projectMCP: ".mcp.json"`（相对项目根）。
2. `userDir` / `projectDir` 保持 `.claude` 不变：skills 与 rules 仍落在 `~/.claude/` 与
   项目根的 `.claude/`，这次只改 MCP 那一类文件。
3. `jsonKey` 仍为 true。`~/.claude.json` 是 Claude 的主配置，`mergeJSONKey` 只读写
   `mcpServers` 容器、其它键按原始字节回写，不会破坏 `projects` 等字段；ADR 0050 已把
   合并位置固定在 `mcpServers`。
4. 启动登记表（hosts.go）本来就是 `~/.claude.json`，不需要改。修正后两张表一致，
   代理自己写的 `dec_vault` 与云端下发的 `dec-*` 落在同一文件的同一个容器里（ADR 0050
   第 58 行的预期）。

## 影响

旧落点 `~/.claude/mcp.json` 上已下发的托管条目不会被代理自动清理：删除按新落点解析，
旧文件不在白名单的这条路径上。升级后需在本机手工移除一次，否则会留下一个 Claude
不读、人却看得见的文件，排查时容易被当成「已经写了」。

## 验证

单测 `TestResolveClaudeMCPPath` 锁定四件事：用户级 MCP 解析到 `~/.claude.json`、项目级
解析到 `<项目根>/.mcp.json`、skills 仍落在 `~/.claude/skills/`、以及 claude 仍在白名单内。
