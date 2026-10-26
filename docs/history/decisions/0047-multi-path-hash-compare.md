# 0047 多落点哈希必须全量比对

## 背景

一份资产经常要落到同一台机器的多个宿主目录。当时实测用的那个资产就是五个：
`.claude/`、`.codebuddy/`、`.codex/`、`.cursor/`、`.with/`。协议里
一个 `ManifestEntry` 带一组 `Paths`，五个绝对路径同属一条。

代理每轮对账要上报这些落点的当前哈希，服务端拿它和自己记录的「上次落地哈希」
比对，不一致就判定被本地改过，重新下发覆盖（需求 6.2）。所以上报的哈希必须
如实反映落点的真实状态——这是整个增量体系的地基。

## 问题

`internal/agent/manifest.go` 的 `BuildManifest` 原本这样写：

```go
var hash string
missing := false
for _, p := range e.Paths {
    h, err := HashFile(p, e.Format, e.Key)
    if err != nil { missing = true; break }
    if h == "" { missing = true; break }
    hash = h          // 每轮都覆盖，只留下最后一个
}
```

`hash = h` 在循环里没有 break，最后留下的是**最后一个路径**的哈希。于是：

- 最后一个副本被改 → 上报的哈希变了 → 云端判定不一致 → 全部重发，正确。
- 前面任意副本被改、最后一个没动 → 上报的还是最后那个的哈希 → 和云端记录
  一致 → 判定「没被改过」→ **被改的那一份永远不会被纠正**。

这不是理论风险。2026-10-26 在实测资产的 `.cursor/skills/.../SKILL.md`
尾部追加了一行标记，等 25 秒，文件没被还原；把同样的标记追加到 `.with/`
（路径列表里最后一个），25 秒内被自动覆盖回原内容。同一次实验里，`.cursor`
是漏检的那一份，`.with` 是侥幸生效的那一份。

漏检的概率不低：五个副本里只要被改的不是最后一个，就检测不到。日常编辑
最常动的是 `.cursor`（Cursor 是主力 IDE），而它在路径列表里排第四——几乎是
最容易漏检的位置。

## 决策

多落点条目的哈希必须**全量比对**：读出所有副本的哈希，只要有两份不同，就
上报空哈希。

```go
var hash string
inconsistent := false
for i, p := range e.Paths {
    h, err := HashFile(p, e.Format, e.Key)
    if err != nil { inconsistent = true; break }
    if h == "" { inconsistent = true; break }
    if i == 0 { hash = h; continue }
    if h != hash { inconsistent = true; break }
}
```

上报空哈希等于告诉云端「这个条目的状态我保证不了，请完整补发」，会让所有副本
重写一遍。代价是多写几个文件，收益是被改的副本一定能被纠正。用「宁可多写」
换「一定正确」是划算的：这个链路上漏检的后果是用户改过的内容悄悄留在磁盘上，
而多写的后果只是一次幂等的重复写入。

注意空哈希的语义是复用的：原来「落点缺失」也上报空哈希，现在「内容不一致」
走同一条路径。这两种状态对云端的要求是一样的——重发。不需要新增协议字段。

## 为什么不是别的方案

**取第一个路径的哈希。** 只是把漏检位置从「最后一个之外」挪到「第一个之外」，
问题一模一样，换了个受害者。

**逐路径各上报一条。** 协议层面把一条多落点资产拆成多条单落点条目。可行，
但会改变 `ManifestEntry` 的语义，牵动服务端 `render/state.go` 的记录结构与
`Sync` 的比对逻辑，还要处理「同一资产的多条记录要一起成功」。为一个本可以用
三行代码修掉的比较逻辑，去动协议和两端的状态机，不值得。

**在代理侧直接纠正不一致的副本。** 代理不知道期望内容是什么，它只有路径和
哈希。让它自己去比对副本之间谁对谁错，等于让代理猜测云端意图，违背
「代理不保存下发清单」的设计（需求 5.2）。

## 影响

- `internal/agent/manifest.go`：`BuildManifest` 改为全量比对。
- `internal/agent/sync_test.go`：新增 `TestSyncReportsDivergentCopiesAsEmptyHash`
  与 `TestSyncReportsConsistentCopiesWithHash`，前者在旧实现下会失败。
- 服务端与协议无需改动。
- 已部署的旧代理仍然带这个缺陷，需要发新版本才会修掉。生产上现有的多落点
  资产都是测试期写入的、内容一致，没有实际数据损失。
