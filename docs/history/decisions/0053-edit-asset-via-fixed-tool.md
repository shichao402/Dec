---
状态: 已接受
日期: 2026-10-27
关联: [0041](0041-notice-rendered-by-server.md)、[0049](0049-uninstall-must-not-delete-user-files.md)、[0009](0009-local-copy-readonly.md)
影响范围: MCP 工具清单、资产写入链路、落点说明行
---

# 改资产内容走固化工具，说明行写明怎么改

## 背景

用户要求能让 AI 修改云端的 skill，并且要做硬：AI 必须调用固化好的工具，
而不是自己组织读写步骤，更不能去改本地副本。

改造前能真正改到云端源的入口有四个：管理面资产页、`dec_read_asset` 加
`dec_write_asset`、代理本机 `/api/call` 直调同一对方法，以及删除类工具。
人走管理面是安全的，因为前端代码固化了「读全量、回传原 manifest、整包写回」。
AI 只能走后两条，它们都要求模型自己组织一次整体替换，有三个坑：

1. 整体替换：`items` 少传一个文件，那个文件就被删除。
2. 编码不对称：`dec_read_asset` 返回的 manifest 与文件内容是 `[]byte`，JSON 里是
   base64；`dec_write_asset` 的 manifest 参数却收明文。工具描述又说「原样回传」，
   照做就会写进一串 base64，`planes`/`host` 声明随之丢失。
3. 不回传 manifest 会触发重新推断，下发范围被悄悄改掉。

另外，AI 读到的通常是落点副本（如 `~/.claude/skills/x/SKILL.md`）或某个看起来
像源的旧目录。原说明行只说「要改请改云端」，没说云端是哪个 bundle、哪个文件、
用什么工具，AI 仍需自己推断。

## 决定

1. 新增协议方法 `edit_asset_file` 与 MCP 工具 `dec_edit_asset_file`。参数只有
   `bundle`、`path`、`old_string`、`new_string`、`base_revision`、`message`。服务端在
   一个事务内读出文件、做唯一匹配替换、保留可执行位后写回；其余文件与
   manifest 不经过调用方，原样保留。
2. 拒绝而不是猜测：`old_string` 出现 0 次或多次、为空、与新文本相同，文件不存在
   或不属于任何条目，目标是 `manifest.yaml`，都返回错误且不产生提交。新增文件、
   改 manifest 这类结构性改动仍走 `dec_write_asset`。内容校验（明文密钥扫描、
   密钥引用完整性）与整体写入相同。
3. 说明行带定位：服务端渲染时把条目的 bundle 与源文件路径写进说明行，形如
   `由 Dec 管理，本地修改会被覆盖；要改请改云端。修改方法：调用 MCP 工具
   dec_edit_asset_file（bundle=relkit，path=skills/relkit-ops/SKILL.md），或在 Dec
   管理面的资产页编辑；不要改这份副本。` AI 照抄参数即可，不需要推断源在哪里。
4. `notice.Text` 保持不变，定位信息只追加在它之后。卸载扫描（0049）与 `Apply` 的
   幂等判断都按「包含 Text」认领，新旧两种说明行都能被识别。
5. `dec_read_asset` 在 MCP 层把 manifest 与文本文件解码成原文返回，二进制保留
   base64 并带 `base64=true`，与 `dec_write_asset` 入参格式对称。
6. MCP 总说明与 `dec_write_asset` 描述改为分流：改已有文件内容一律用
   `dec_edit_asset_file`，`dec_write_asset` 只用于结构性改动。

## 不做的事

- 不限制 AI 的通用文件编辑能力，那不在 Dec 的控制范围内。误改落点副本的兜底
  仍是对账覆盖加覆盖审计（0048），结果是「没生效」而不是「悄悄留下」。
- 不处理不带说明行的旧目录（例如某项目里的 `DecAssets/`）。它们不被 Dec 读取，
  也无法被 Dec 打标记。
- 不提供正则替换、多处替换。

## 影响

- 发版后说明行文案变化，所有带说明行的落点会被重写一次。覆盖判定只比较代理上报
  哈希与上次落地哈希，未被本地改动的落点两者相等，不会误记覆盖事件。
- MCP 工具清单从 15 个变为 16 个，测试与 `dec-agent acceptance` 的期望清单同步更新。
- `read_asset` 的 `revision` 改为仓库 HEAD（读文件之前取），与 `base_revision` 的比较
  对象一致。此前返回的是 manifest.yaml 里的 revision 字段，通常为空，读完拿不到可用的
  base，乐观并发形同虚设。

## 验证（2026-10-27，v2.1.33 生产）

通过代理 `/mcp` 端点按 MCP 协议调用：`dec_read_asset relkit` 返回 manifest 与 SKILL.md
原文；多次匹配、零匹配、改 manifest.yaml 三种请求均被拒且不产生提交；对
`skills/relkit-ops/SKILL.md` 做一处唯一替换后得到 commit 81d7367（source=mcp），
claude、codebuddy、codex、cursor、With 五个落点全部更新，说明行带
`bundle=relkit，path=skills/relkit-ops/SKILL.md`，设备 drift 为 0，未新增覆盖记录。
