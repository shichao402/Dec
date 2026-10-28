---
状态: 已接受
日期: 2026-10-28
关联: [0054](0054-with-in-startup-host-sync.md)、[0050](0050-mcp-entry-must-nest-in-mcpservers.md)
影响范围: 代理写入宿主的内置 MCP 条目名、serverInfo 名称、验收脚本
---

# 内置 MCP 入口改名 dec_vault

## 背景

代理写进各宿主的内置 MCP 条目叫 `dec`，与项目名、仓库名完全相同。自然语言里
「用 dec 改一下」可以指改 Dec 仓库源码、调 MCP 工具改云端资产、或旧系统遗留的
名为 `dec` 的 skill，前两种后果相反（一个改本地文件，一个改云端源）。

## 决策

1. 命名原则：项目叫 Dec，组件一律是 `dec` 加角色名，不再出现裸的 `dec`。
2. 内置入口改名 `dec_vault`，serverInfo 名称同步。vault 同时涵盖资产与密钥。
3. 必须用下划线，不能用 `dec-vault`：`dec-` 是云端下发 MCP 条目在共享配置里的
   托管前缀（`ssot.EntryKey`，代理按前缀识别归属）。落进这个命名空间，某个 bundle
   里出现名为 `vault` 的 MCP 条目时会静默覆盖入口；入口地址随端口变化，还会被算进
   托管条目哈希，造成误报漂移。`TestEntryNameOutsideManagedPrefix` 守住这一条。
4. 迁移：写入新名时删除旧名 `dec`（JSON 与 Codex TOML 都处理），新名不存在时继承
   旧条目的 `disabled`；卸载时新旧两个名字都删。
5. 工具名 `dec_*` 不改：挂在 server 下、只给 AI 看，不构成口语歧义，改动成本高。

## 连带清理

With 与 Cursor 的 skills 目录下残留旧系统渲染的 `dec`、`dec-extract-asset` 两个
skill（2026-10-13 生成，不在任何云端 bundle、无说明行，描述的是已下线的 Console、
dec-server、dec_propose_upstream），一并删除，避免 AI 被引回旧做法。

## 验证（2026-10-28，v2.1.36）

本机代理升到 v2.1.36 重启后，Claude、Cursor、With 的 `mcpServers` 里旧 `dec` 已删、
`dec_vault` 已写入，其余条目逐项不变；Codex 的 `config.toml` 只剩
`[mcp_servers.dec_vault]`。With 无需重启即按新名重新连上，`dec_status` 返回
aligned=true，旧名 `dec` 已从 With 的 MCP 列表消失。四个旧 skill 目录已删。
