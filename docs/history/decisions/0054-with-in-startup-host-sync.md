---
状态: 已接受
日期: 2026-10-27
关联: [0050](0050-mcp-entry-must-nest-in-mcpservers.md)、[0053](0053-edit-asset-via-fixed-tool.md)
影响范围: 代理启动时的宿主 MCP 自动登记（internal/agent/hosts.go）
---

# 代理启动登记 dec 条目覆盖 With

## 背景

代理每次启动把自己写成名为 `dec` 的 MCP 条目（需求 2），但 `DefaultHosts`
只列了 Claude、Codex、Cursor。With 的条目是早先手工写的，代理换端口后它仍指向
旧地址：With 连不上 dec，拿到的工具清单停在旧会话，验证 ADR 0053 时就看不到
新增的 `dec_edit_asset_file`。资产落点表（host.go）早已包含 With，两张表不一致。

## 决策

1. `DefaultHosts` 加入 With：目录 `~/.bg-agent/config-with-app`，文件
   `mcp_config.json`，JSON 格式。目录不存在照旧跳过，不替用户创建。
2. With 条目写 `transportType: "streamable-http"`，与它现有条目的写法一致，
   代理的 `/mcp` 就是 streamable HTTP。
3. 更新地址时保留原条目里的 `disabled` 值。代理只负责把地址改对；用户在宿主里
   关掉了 dec，就不能借更新之机重新打开。原条目没有 `disabled` 时不凭空加上。
   这条对所有 JSON 宿主一视同仁。

## 不做

CodeBuddy 的用户级 MCP 位置本机没有实物可核对，这次不加，避免凭猜测写进别人的
配置文件。
