# 0050 MCP 条目必须落进 mcpServers 容器

- **状态**：已接受（待发版）
- **日期**：2026-10-26
- **影响范围**：`internal/agent/apply.go` 的 `mergeJSONKey` / `removeJSONKey` / `managedOnly`，所有走 `json_key` 格式下发的 MCP 条目

## 背景

新系统的 MCP 资产以 `json_key` 格式下发：云端给出一个单条目对象，代理把它合并进宿主的共享 JSON 配置文件。设计意图是只动带 `dec-` 前缀的键，其余内容原样保留（需求 2.7、6.3）。

`mergeJSONKey` 的实现直接对文件根对象做 `root[key] = value`，也就是把条目写在配置文件的顶层。

## 问题

顶层不是任何宿主读 MCP 配置的位置。

四个 JSON 宿主都只读 `mcpServers` 对象下的键：

```json
{
  "mcpServers": {
    "dec-gongfeng-mcp": { "url": "https://..." }
  }
}
```

写到顶层得到的是：

```json
{
  "dec-gongfeng-mcp": { "url": "https://..." },
  "mcpServers": { "dec": { "url": "http://127.0.0.1:8787/mcp" } }
}
```

这份文件是合法 JSON，写入不报错，哈希对得上，`drift` 归零，代理回报「已写入」。唯一的症状是：那条 MCP 在宿主里永远不会出现。

失败是静默的，这是这个缺陷最难发现的地方。它不产生错误日志、不产生告警、不产生漂移，只在人打开宿主的 MCP 面板时表现为「少了一条」。而同一份文件里，代理自己写的 `dec` 条目是正常的——因为它走的是另一条代码路径（`internal/agent/hosts.go` 的 `upsertJSONEntry`，那里一直是对的）。两条路径对同一个文件用了不同的写入位置，这个不一致本身就是缺陷的证据。

定位过程：迁移 `gongfeng` MCP 条目后核对落点，发现 `.cursor/mcp.json` 与 `.bg-agent/config-with-app/mcp_config.json` 顶层多了一个 `dec-gongfeng-mcp`，而 `mcpServers` 里没有。`.claude/mcp.json` 更直观——整个文件只有一个顶层键，`mcpServers` 根本没被创建。

## 决定

`mergeJSONKey` 改为读写 `mcpServers` 容器：

1. 读出 `mcpServers`（不存在则视为空对象），在其中写入目标键，再放回根对象。
2. `mcpServers` 存在但不是对象时拒绝改写。这不是过度谨慎：覆盖它会连带丢掉用户原有的全部服务器定义。
3. 写入后顺手 `delete(root, key)`，清掉旧版本误写在顶层的同名键。否则那个键会一直留在文件里，人排查时看得见它、宿主却读不到，会误判成「已经写了」。
4. `removeJSONKey` 对称处理：两边都找，两边都删，只在文件确实空了（`mcpServers` 也空）时才删文件。
5. `managedOnly` 只统计 `mcpServers` 里的 `dec-` 键。顶层是用户的地盘，用户在那里放别的工具配置不该被算成漂移。

## 后果

需要重新下发一次 MCP 条目：已经写错位置的条目会在下一次对账时被搬到 `mcpServers` 里。

已经写进顶层的那份残留不会自动消失，因为哈希只看 `mcpServers`，顶层变化不触发重发。这需要一个明确的处理动作：重新写一次同一个条目即可，`mergeJSONKey` 里的 `delete` 会顺手清掉。已迁移的 `gongfeng-mcp` 按此方式处理。

代理自己的 `dec` 条目与云端下发的 `dec-*` 条目现在落在同一位置，`upsertJSONEntry` 与 `mergeJSONKey` 的行为一致。两条路径仍然独立，因为它们处理的是不同的键集与不同的生命周期，但写入位置必须一致，这是约束。

## 备选方案

**加一个「容器路径」字段到协议里**（如 `File.Container`）。否决：容器位置由宿主的配置格式决定，不由云端决定。云端不该知道 cursor 用 `mcpServers`、codex 用 TOML 表——那是代理的白名单知识（决策 10 已经把这个边界划清）。把它放进协议等于把宿主细节泄漏到云端，还要为每个宿主维护一份映射。

**在服务端渲染时就把 `mcpServers` 包好**。否决：`json_key` 的语义是「只写一个键」，包一层容器就变成了「写一个对象的一部分」，与 `FormatWhole` 的边界模糊掉。而且服务端不知道目标宿主是 JSON 还是 TOML。

**只加文档与检查，不改代码**。否决：这条路径上没有任何自动检查能发现写错位置，因为文件始终是合法 JSON。

## 验证

`internal/agent/apply_test.go` 新增三条测试，都做过反向验证（临时把实现改回写顶层，测试立刻失败）：

- `TestApplyJSONKeyKeepsOthers`：托管条目必须出现在 `mcpServers` 里、且不在顶层；用户自己的条目原样保留。
- `TestApplyJSONKeyCreatesContainerWhenMissing`：文件里没有 `mcpServers` 时要自己建，用户的其它字段不能丢。
- `TestApplyJSONKeyMovesLegacyTopLevelKey`：旧版本写在顶层的同名键被搬进容器，代理自己的 `dec` 条目不能被误删。
- `TestRemoveJSONKeyCleansLegacyTopLevel`：卸载时顶层的残留也要清掉。
- `TestRemoveJSONKeyKeepsFileWithUserServers`：还有用户条目时不能删文件。
- `TestHashIgnoresUserKeys`：新增顶层字段不该改变托管哈希。
