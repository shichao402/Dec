# 0051：json_key 条目按规范化语义算哈希，不按任何一侧的落盘字节

- **状态**：已接受（待发版）
- **日期**：2026-10-26
- **影响范围**：`internal/proto/jsonnorm.go`（新增）、`internal/cloud/render/render.go`、`internal/agent/apply.go`，所有走 `json_key` 格式下发的 MCP 条目
- **相关**：[0050](0050-mcp-entry-must-nest-in-mcpservers.md)、[0047](0047-multi-path-hash-compare.md)、[0041](0041-notice-rendered-by-server.md)

## 背景

MCP 条目走 `json_key` 格式下发：不整文件覆盖，只往目标宿主的 `mcpServers` 容器里写一个键。

服务端渲染这种条目时，`pf.Content` 是「条目在源文件里的原始字节」。云端源文件是人手写的，通常是缩进两行的形状：

```json
{
  "url": "https://mcpgw.knoa.com/gongfeng"
}
```

代理落盘时要把条目塞进容器、整文件重排，落盘后容器里的条目是紧凑的：

```json
{"url":"https://mcpgw.knoa.com/gongfeng"}
```

两侧算哈希用的是同一套 SHA-256，但喂进去的字节不同。

## 问题

服务端算 `ssot.SumHex([]byte(pf.Content))`，即原始带缩进的字节；代理算 `hashOf(managedOnly(...))`，即落盘后紧凑化的字节。**两者永不可能相等**，于是：

- `Drift` 每条 json_key 条目恒 +1，设备列表长期显示 `drift: 1`
- 每轮对账都判定为「落点与期望不一致」，同一条 MCP 被反复下发
- 覆盖事件的去重逻辑被持续触发

这个缺陷比 0050（写错位置）更难发现。0050 至少还有外部症状——条目从配置文件里消失了，人一眼能看见。这个缺陷完全没有外部症状：文件内容一直是对的，宿主导入正常，唯一的表现就是那个永远不为零的 drift 数字，而它很容易被当成「噪声」忽略。

排查时先怀疑了「用户自己的条目混进哈希」和「条目名前缀」，都排除了。最后是把真实落点的字节取出来，用两侧的函数各算一遍才定位到：同一个 `.cursor/mcp.json`，服务端口径算出 `52acb639…`，代理口径算出 `71dc090e…`。

## 决定

1. **新增 `proto.NormalizeJSONEntry`**，作为两侧唯一的规范化口径：解析成 `any`（数字走 `json.Number`，不经过 `float64`），再紧凑重排。不依赖任何一侧的落盘格式。
2. **服务端**渲染 json_key 条目时，用 `NormalizeJSONEntry(pf.Content)` 的结果算哈希；whole 格式不变，仍算下发字节。
3. **代理**`HashFile` 对 json_key 条目只取目标键的原始字节，规范化后再算哈希。原来算「容器内全部 dec- 前缀条目」的逻辑（`managedOnly` / `marshalSorted`）删除——它不只是口径不一致，本身也是错的：多个托管条目会让每个条目的哈希都被别的条目牵动。
4. **代理回执哈希改为直接调用 `HashFile`**，保证「写下的哈希」与「下一轮读回来上报的哈希」同源，不会各自实现一遍。
5. **规范化失败时服务端报警告并跳过该条目**，不给空哈希。空哈希会被下一轮当成漂移无限重发，把一个问题变成一场循环。

## 后果

- 存量设备需要重新下发一次，把服务端记录的状态哈希刷成新口径。这一次下发之后 drift 归零。
- 源文件排版变化（缩进、键序）不再触发全设备重写，只有语义变化才触发。
- `managedOnly` 与 `marshalSorted` 被删除，`noticeKey` 在哈希路径上不再有意义（说明行对 json_key 本来就不适用，见 `internal/notice`）。
- 空哈希的语义收窄为「条目在文件里不存在」，不再表示「容器里的集合变了」。

## 备选方案

**让代理按下发内容算哈希，不读文件。** 否掉：代理不保存下发清单是需求 5.2 的硬约束，它必须靠读文件才能发现自己被外部改动。改成信下发内容，本地改动就永远检测不到。

**让服务端只算「紧凑化后的条目字节」但仍保留 `managedOnly`。** 否掉：这只能修好单条目场景。同一个文件里落两个 MCP 条目时，条目 A 的哈希会被条目 B 的变化带动，仍然误报。

**统一在协议层下发期望哈希，代理只比对文件字节。** 否掉：这等于让代理保存下发清单，同上。

## 验证

`internal/agent/hash_agree_test.go`：

- `TestServerAndAgentHashAgreeOnJSONEntry` —— 两侧对同一条目算出的哈希必须逐字节相等（核心断言）
- `TestAgentHashIgnoresWhitespaceInSource` —— 排版变化不改变哈希
- `TestAgentHashDetectsEntryChange` —— 条目内容真变了必须被发现
- `TestAgentHashIgnoresOtherServers` —— 用户自己的条目与顶层字段不影响托管哈希
- `TestAgentHashMissingKeyIsEmpty` —— 条目被删返回空哈希，让云端补发

`internal/cloud/render/agent_e2e_mcp_test.go`：

- `TestSyncConvergesWithRealAgentOnMCP` —— 真实 Writer 落盘、真实 HashFile 读回、真实 Sync 对账，第二轮不再下发且 `Drift` 为 0

反向验证：把服务端改回 `ssot.SumHex([]byte(pf.Content))`，`TestSyncConvergesWithRealAgentOnMCP` 立刻失败并报「仍要下发 1 个」，与线上症状一致；把 `NormalizeJSONEntry` 改成原样返回，`TestServerAndAgentHashAgreeOnJSONEntry` 立刻失败。两条测试都确实咬住了这个缺陷。

## 留下的教训

`agent_e2e_test.go` 那条「真实链路」测试本来是专门为捕获这类缺陷写的（它的注释里记着 2026-10-23 那次写满磁盘的事故），但它用的三个条目全是 `whole` 格式，一条 `json_key` 都没有。测试写得再真，只要漏了一整类格式，缺陷就能安然藏在里面。

以后加下发格式，第一件事是在真实链路测试里补一条该格式的用例，而不是补独立单元测试。
