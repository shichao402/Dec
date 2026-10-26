---
状态: 已接受
日期: 2026-10-26
关联: [0047](0047-multi-path-hash-compare.md)、[0009](0009-local-copy-readonly.md)
影响范围: 对账时的覆盖检测、覆盖审计记录、多落点条目
---

# 空哈希也必须记一条覆盖事件

## 背景

[0047](0047-multi-path-hash-compare.md) 修掉了多落点哈希只取最后一份的漏检缺陷：
一个条目同时落在 `.claude/`、`.codebuddy/`、`.codex/`、`.cursor/`、`.with/`
五个目录时，只要有一份与其它不同，代理就上报空哈希，请求云端完整补发。

修完当天发 v2.1.21 上线，随后做需求 5（离线降级）实测时踩到了连带缺陷：
本地改 `.cursor` 那份（路径列表第 4 个），40 秒内文件确实被覆盖回云端内容，
但服务端 `state/<device>.json` 的 `overrides` 里一条记录都没有。

## 问题

覆盖检测的判断是这样写的：

```go
if seen && cur.Hash != "" {
    if prev, ok := state.Files[f.File.ID]; ok && prev.Hash != cur.Hash {
        overrides = append(overrides, ...)
    }
}
```

`cur.Hash != ""` 这个前置条件来自「空哈希只可能是删除回执」的年代。但 0047
之后空哈希多了一个含义：这个条目有多份副本，代理保证不了它们的状态。两个
含义撞在一起，后果是：

- 覆盖照常发生（`files = append(files, f.File)` 在判断之外），文件被静默改回。
- 审计里没有任何痕迹。

这比完全不检测更糟。完全不检测至少还能看出「没生效」；现在是生效了但查不到
是谁、什么时候、改了哪一份，出问题时无从追溯。

## 决定

去掉 `cur.Hash != ""` 前置条件，只保留下面的 `prev.Hash != cur.Hash`：

```go
if seen {
    if prev, ok := state.Files[f.File.ID]; ok && prev.Hash != cur.Hash {
        overrides = append(overrides, ...)
    }
}
```

`prev.Hash` 是上一次落地回执记录的哈希，一定非空（空哈希的回执走的是删除分支，
会把条目从 `state.Files` 里删掉）。所以首次下发时 `prev` 不存在，不会误记；
之后任何形式的「与上次落地不一致」都会落一条记录，包括空哈希。

## 后果

- 覆盖记录从「只在代理报出具体哈希时才有」变成「只要发生覆盖就有」。
- `Override.Hash` 可能为空。管理面只展示条目、落点、时间，不展示哈希，
  所以展示层不受影响；将来若要按哈希去重或回溯，需要按「空 = 多副本不一致」
  理解。
- `record Override` 的去重键仍是 `ID + Hash`，同一份不一致的副本重复上报
  仍只留一条，不会因为哈希为空就每次都新增。

## 备选方案

**让代理上报「哪一份不同」而不是空哈希。** 更精确，但要在协议里加字段、
代理要逐个落点算哈希再挑出差异项，改动面比这次修复大得多，且会破坏 0047
定下的「空哈希 = 保证不了」这条简单约定。当前需求只要求「知道被改过」，
不需要「知道改的是第几份」，不划算。

**在代理侧先自己覆盖，再上报新哈希。** 违反需求 6.1（代理不保存下发清单、
不做双向同步），也违反需求 5.2（本地落点只是只读缓存）。不做。

## 第二处根因：去重时的 map 零值陷阱

只去掉 `cur.Hash != ""` 还不够。改完发 v2.1.22 上线后重测，记录仍然不出现。
真正把记录吞掉的是 `AddOverrides` 的去重：

```go
last := make(map[string]string, len(st.Overrides))
for _, o := range st.Overrides {
    last[o.ID] = o.Hash
}
for _, o := range overrides {
    if last[o.ID] == o.Hash {   // ← 这里
        continue
    }
    ...
}
```

`last` 用 `o.ID` 建索引，取不存在的键返回零值 `""`。空哈希也正好是 `""`。
所以「这个条目从来没被记录过」和「这个条目记过一条空哈希」在比较时完全等价，
第一条空哈希覆盖必然被当成重复丢掉。非空哈希没有这个问题，因为零值 `""`
不可能等于一个真实的 sha256 值。

改成用 `ok` 判断存在性：

```go
if prev, ok := last[o.ID]; ok && prev == o.Hash {
    continue
}
```

## 两个根因为什么同时存在

`cur.Hash != ""` 和 `last[o.ID] == o.Hash` 都写在「空哈希只可能是删除回执」
的年代。删除回执的空哈希根本不会走到这两处：删除的条目在 `RecordAck` 里
就被从 `state.Files` 移除了，`want.Files` 里也不会再有它。所以这两个空哈希
判断在当时是死代码，写的时候不用想清楚它的语义。0047 给空哈希赋予新含义后，
死代码变成了活的地雷，而且两处都踩了。

## 验证

- `TestSyncRecordsOverrideWhenHashUnreported`：空哈希补发必须有覆盖记录，
  且记录带上代理上报的全部落点。旧实现下失败。
- `TestSyncFirstTimeEmptyHashIsNotOverride`：首次下发就报空哈希不该有记录，
  防止修复过头。
- `TestAddOverridesKeepsEmptyHash`：首次空哈希必须落库、重复的同样空哈希
  才去重、哈希变化要新增。实测把去重改回旧写法，这条立刻报
  「空哈希的首次覆盖必须记下来，实际 0 条」。
- 真机实测（2026-10-26，v2.1.23）：五落点条目改 `.claude` 那份，
  服务端 `state/<device>.json` 的 `overrides` 新增一条。

