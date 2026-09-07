# 0023 — 门面能力口径：Console 覆盖人面能力，MCP 扩展需登记

- **状态**：已接受（已实现）
- **日期**：2026-09-07
- **关联**：[0008](0008-service-facade-split.md)（服务 / 门面拆分）、[0020](0020-retire-tui.md)（Console 为人机入口）、[0022](0022-console-bitwarden-unlock.md)（认证只在 Console）
- **影响范围**：`client/src/pages/`（同步、删除页）、`internal/mcp/`、`internal/app/list_secrets.go`、`.cursor/rules/console-first.mdc`

## 问题

Console-first 只规定「新功能先设计 Console 页，Console 未覆盖就留 `TODO(console)`」。它约束的是
**新功能从哪一端开始设计**，没有回答一个已经发生的问题：MCP 有 Console 没有的能力，算不算违规？

一次门面能力审计列出八处差异，并把它们统一判成「MCP 超出 Console」。但这个判断把性质完全不同的
东西混在一起：既有「人真的需要、Console 却没有」的缺口（看不到有哪些密钥文件、删不了远端资产、
推不了本机平面），也有「只为免交互自动化存在」的参数（一次调用跑两个平面、置备时直接指定分支和
标签）。按同一条规则处理，前者会被拖着不修，后者会被逼着在 Console 加人根本不需要的开关。

缺口方向还是反的：删除是破坏性操作，此前只有 Agent 门面能做，人要删自己的密钥得让 Agent 代劳。

## 决策

### 1. 判定标准是使用者，不是门面先后

一项能力该在哪个门面出现，取决于**谁在用它**：

- **人会用的能力**：必须在 Console 有入口。MCP 可以有同名工具，但 Console 缺失即为缺口，
  不因「MCP 已经能做」而免除。
- **只为免交互自动化存在的参数**：允许 MCP 独有，不要求 Console 补对应控件。

「哪个门面先实现」不构成合法性依据。

### 2. Console 必须覆盖的能力

以下三项在本决策中补齐：

| 能力 | Console 入口 | 此前状况 |
|------|--------------|----------|
| secrets 元数据只读清单 | 同步页「密钥清单」 | 只有 `dec_list_secrets`；「我的 token 在哪」在 Console 无法回答 |
| 删除远端与本机库存 | 删除页 | 只有 `dec_list_delete_candidates` / `dec_delete`；破坏性操作只在 Agent 门面 |
| Global 平面预览与推送 | 同步页推送目标选 Global | 只能推项目；本机凭据改完没有界面推 |

### 3. 允许的 MCP 独有扩展（需登记）

| 扩展 | 为什么人不需要 |
|------|----------------|
| `plane=both` | 省一次往返。人在界面上分两次操作，反而更清楚当前在动哪个平面 |
| `dec_provision_remote` 的 `branch` / `tags` | 免交互置备要一次传全；Console 有人在场，可逐步确认 |
| `dec_init_project` 的 `apply_vault_project` | 同上；Console 的初始化流已有显式绑定步骤 |

新增 Console 没有的 MCP 工具或参数时，必须在本表加一行并写明人为何不需要。**没登记就算缺口**，
而不是默认合法。

### 4. 认证方向的不对称是设计

MCP 永不承载人工认证输入，这不是缺口，见 [0022](0022-console-bitwarden-unlock.md)。
反方向同理：Console 不提供 `plane=both` 式的批量开关。

### 5. 平面参数按 0015 传递

Console 补本机平面能力时，`projectRoot` 必须为空、`workspacePlane` 为 `global`。
服务端不得把「Root 为空」一律当成「缺项目根」报错——那会让本机平面永远调不通
（`ListWorkspaceSecretsMetadata` 此前就是如此）。只有项目平面要求非空 Root。

## 理由

- 按使用者划分，缺口和扩展有了各自的处置方式，不用在「补 Console」和「删 MCP」之间二选一。
- 破坏性能力回到人手里：删除有了 Console 入口，人不必通过 Agent 删自己的密钥。
- 登记义务让差异保持显式。差异本身不是问题，无人知道的差异才是。

## 被否方案

**A. 严格子集：MCP 不许有 Console 没有的东西，超出的要么补 Console 要么删掉。**
否决：会把 `plane=both` 这类纯往返优化也判成违规，逼 Console 加一个「同时操作两个平面」的开关。
那是人不需要、且会让人看不清正在动哪个平面的危险 UI。

**B. 不作约束，两个门面各按自己的使用者演进，只要求文档如实记录。**
否决：删除这类破坏性能力会长期只存在于 Agent 门面。文档记下来也不改变「人得让 Agent 删自己的
密钥」这个事实。

**C. 以门面先后判定合法性：谁先实现谁是基准。**
否决：与使用者是否需要无关。同一项能力先落在 MCP 还是 Console，往往只是当时手边在改哪个文件。

**D. 把 MCP 的破坏性能力（`dec_delete`）收掉，只留 Console。**
否决：Agent 清理自己产生的资产是正当需求。破坏性的正确答案是可恢复语义
（见 [0024](0024-vault-delete-to-trash.md)）与显式确认，不是收走能力。

## 实施结果

- Console 同步页：推送目标扩展为「Global（本机）+ 各项目」，新增密钥清单面板
- Console 删除页：列库存、按分区勾选（远端 / 本机互斥）、确认词门控、结果区渲染残留项
- `ListWorkspaceSecretsMetadata` 放宽用户平面的空 Root 检查，项目平面仍拒绝
- 布局测试覆盖两个平面各一次推送预览，以及删除页的分区锁定与确认门控

## 参考

- `.cursor/rules/console-first.mdc` — Console 页与能力映射
- `internal/assets/dec/SKILL.md` — Agent MCP 快速参考
