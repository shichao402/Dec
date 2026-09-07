# 0024 — 远端删除移入保险库回收站，不永久删除

- **状态**：已接受（已实现）
- **日期**：2026-09-07
- **关联**：[0004](0004-remote-page.md)（删除拆语义）、[0010](0010-pull-orphan-and-ops.md)（删除收敛）、[0023](0023-facade-capability-parity.md)（Console 删除入口）
- **影响范围**：`internal/secrets/apiclient.go`（`deleteCipher`、`listCiphers`）、Console 删除页文案

## 问题

`DeleteSecureNote` 与 `DeleteSSHKey` 走 Bitwarden 的 `DELETE /ciphers/{id}`，这是**永久删除，不可撤销**。
删错一条 `.env` Note 就没了，Bitwarden 侧没有任何恢复途径。

同一次删除操作里，另外两侧都是可恢复的：私仓资产删除后仍在 Git 历史里，本机落地文件删掉可以
重新 pull。唯独 Bitwarden 侧是单向的，而它恰好是唯一的密钥权威源——最不该丢的那一份反而最脆弱。

[0023](0023-facade-capability-parity.md) 给 Console 加了删除入口，把这个操作从「Agent 偶尔调用」
变成「人点按钮」，风险面随之扩大。

## 决策

### 1. cipher 删除走软删

`deleteCipher` 改用 `PUT /ciphers/{id}/delete`，条目进入保险库回收站。
硬删接口 `DELETE /ciphers/{id}` 不再被 Dec 使用。

### 2. 回收站条目对 Dec 等于不存在

Bitwarden 的 `/ciphers` 列表照常返回回收站里的条目，只是带上 `deletedDate`。
过滤放在 `listCiphers` 这一层——它是 Dec 全部 cipher 读取的唯一入口，单点过滤即覆盖列表、pull
与 push 的同名判重。

不过滤会有三个可见后果：删完还列得出来、同名重建撞「已存在」、pull 把回收站内容拉回本地。

### 3. 恢复不做在 Dec 里

Dec 不提供 restore 入口。误删由官方 Bitwarden 客户端在回收站中恢复。

### 4. folder 删除保持硬删

`DeleteAddress` 继续用 `DELETE /folders/{id}`。Bitwarden 删除 folder 不删其中的条目，条目只是
脱离 folder，没有数据丢失，因此不需要回收站语义。

### 5. 可恢复性写进界面

删除页明确写出三侧的可恢复途径：密钥进 Bitwarden 回收站、私仓资产留在 Git 历史、本机文件可重新
拉取。删除操作的恐惧来自不知道能不能撤销，而不是来自确认按钮的数量。

## 理由

- 唯一权威源不该是唯一不可恢复的一侧。
- 复用 Bitwarden 已有的回收站，Dec 不引入第二套删除生命周期。
- 过滤收在单一读取入口，不需要在每个调用点重复判断，也不会漏。

## 被否方案

**A. 保留硬删，删除前导出一份备份。**
否决：要把明文密钥写到磁盘，与「session、vault key 与密钥正文只在内存」的边界直接冲突
（[0022](0022-console-bitwarden-unlock.md)）。

**B. Dec 自建回收站：删除前把条目复制到某个保留区。**
否决：多一份密文副本和一套过期 / 清理 / 恢复的生命周期要维护，而 Bitwarden 已经提供了。

**C. 软删但不过滤 `deletedDate`。**
否决：删除在 Dec 视角下不生效——列表仍显示、同名重建报「已存在」、pull 把已删内容拉回本地。

**D. 在 Console 里做 restore 入口。**
否决：为低频操作再建一套回收站 UI，且要处理「恢复后与本地落地状态如何对齐」。官方客户端已能
恢复，先不重复。

## 实施结果

- `deleteCipher` 走 `PUT /ciphers/{id}/delete`；测试断言软删路径且不得出现硬删请求
- `listCiphers` 过滤 `deletedDate` 非空的条目；测试覆盖「回收站条目不出现在 ListNotes」
- Console 删除页面板与结果区文案写出可恢复性

## 参考

- `Documents/BUNDLE-SECRETS-MODEL.md` — 删除语义
- `.cursor/rules/bundle-secrets-mirror.mdc` — Push 与删除的硬约束
