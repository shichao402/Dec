---
状态: 已接受
日期: 2026-10-28
关联: [0050](0050-mcp-entry-must-nest-in-mcpservers.md)、[0054](0054-with-in-startup-host-sync.md)、[0055](0055-mcp-entry-renamed-dec-vault.md)
影响范围: 资产落点表（internal/agent/host.go）、代理启动登记（internal/agent/hosts.go）
---

# CodeBuddy 用户级 MCP 落点改为 ~/.codebuddy/mcp.json，并纳入启动登记

## 背景

代理里有两张宿主表。资产落点表 host.go 给 codebuddy 写了 `userMCP: ".mcp.json"`，
它相对用户目录解析，下发的 MCP 资产落到了 `~/.mcp.json`；启动登记表 hosts.go
则按 ADR 0054 的「本机没有实物可核对」没有收 CodeBuddy。

2026-10-28 用户确认 CodeBuddy 用户级 MCP 读的是 `~/.codebuddy/mcp.json`（该文件
已有一条 `transportType` + `url` 写法的条目）。本机 `~/.mcp.json` 里只有 Dec 下发的
`dec-gongfeng-mcp`，CodeBuddy 界面看不到它：又是 ADR 0050 那种不报错、不漂移、
只是「少了一条」的落点错误。`.mcp.json` 是项目级落点（项目根），被误用到了用户级。

## 决策

1. host.go 的 codebuddy 去掉 `userMCP`，用户级走默认 `<userDir>/mcp.json`，即
   `~/.codebuddy/mcp.json`；项目级仍是项目根 `.mcp.json`。
2. `DefaultHosts` 加入 codebuddy：目录 `~/.codebuddy`，文件 `mcp.json`，JSON 格式，
   条目写 `transportType: "streamable-http"`，与它现有条目一致。目录不存在照旧跳过。
3. 旧落点 `~/.mcp.json` 上已下发的托管条目不会被代理自动清理（删除按新落点解析），
   升级后在本机手工移除一次。

## 验证

单测锁定：用户级/项目级 MCP 落点解析、启动登记写入 `~/.codebuddy/mcp.json`、
其它条目原样保留、不在家目录生成 `.mcp.json`。

真机（2026-10-28，v2.1.37）：本机代理升级重启后 `dec-agent mcp-config` 显示 codebuddy
「已写入」`~/.codebuddy/mcp.json`；同文件里出现下发的 `dec-gongfeng-mcp`，原有
`devops-prod-pipeline-streamable` 不变。旧 `~/.mcp.json` 只剩 Dec 写的那一条，已手工
移除，对账后未被重建。
