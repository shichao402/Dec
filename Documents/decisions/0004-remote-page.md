# 0004 — Remote 页：上下文无关的完整远端编辑器

- **状态**：已接受（已实现；2026-08-16 修订为方案 R；同日补齐 A / typed confirm / 无文件夹；2026-08-17 登记改为同级 Processor）
- **第 6 条 `N` 的 folder 输入已被 [0013](0013-secrets-belong-to-declared-target.md) 收紧，再被 [0014](0014-bundle-sole-writable-aggregate.md) 限为仅 `bundle/<名>`**：已声明 project 名不再是合法写入归属
- **日期**：2026-08-10；**修订**：2026-08-16、2026-08-17
- **关联**：[0002-secrets-synctarget-root.md](0002-secrets-synctarget-root.md)、[0003-user-enabled-secret-bundles.md](0003-user-enabled-secret-bundles.md)、[0009-bundle-binary-scope.md](0009-bundle-binary-scope.md)
- **影响范围**：TUI Remote 页；`ListRemoteInventory` / 删除 Mode；Bitwarden Secure Note 与 SSH Hosts 写回；与当前 project / `--user` 解耦的可见性

## 问题

用户需要一个统一入口去浏览与管理**全部**远端平面上的东西，而不是「当前工作区能看见的子集」：

- Dec Git vault：全量 bundles（不分 scope）
- Bitwarden：全部 `bundle/*` + 全部裸 folder（relkit / MyQuant / Dec 等）
- 删除若把「改远端」与「清本地」绑成同一事务，容易误伤本机或误以为只清了本地

旧 **Delete** 页只能做「列 + 删」；编辑 Secure Note / SSH Hosts、以及新增空 Note，都不适合挤在 Confirm 流程里。继续新增独立 Vault 总览页会碎片化（用户已反对）。

## 决策

**侧栏 Delete 改名为 Remote；Remote = 上下文无关的完整远端浏览器/编辑器；删除拆成远端-only / 本地-only；修改走临时文件外部编辑器。**

约定细则：

1. **页名 Remote，不是 Hosts / Secrets / Vault 总览**  
   树抽象的是「远端可控对象」+ 本机清理分区，在现有 Remote 内解决，不新增侧栏页。

2. **Bundles ≠ Remote**  
   Bundles：当前平面启用哪些包。Remote：全量远端内容的浏览/增删改。启用勾选不进 Remote。  
   `bundle.yaml` 的 `scope` 在 Remote 中降级为**分组标签/元数据**，不是可见性开关。

3. **可见性放开（与当前 project / `--user` 解耦）**  
   - Git vault：全量 bundles  
   - Bitwarden：`ListAllFolderNames` 枚举全部 `bundle/*` + 全部裸 folder  
   - 「无文件夹」：只读折叠区，标注「非 Dec 管理」；不可勾选删除；默认折叠；展开仅元数据（名称/类型），不读正文；处理请到 Bitwarden Web  
   - 平面隔离仍保留在 Bundles / pull / push / env / 启用列表（见 [0009](0009-bundle-binary-scope.md) 修订）

4. **删除拆语义**  
   - 默认 `d`（远端分区）：**只删远端**（Bitwarden Note/SSH、Git vault 资产），不碰本地同步根 / cache  
   - 本地分区：文案钉死「只清本机，不写 Bitwarden / 不写 vault」  
   - API：`DeleteRemoteOnly` / `CleanupLocal`（或 `DeleteProjectItems` + `Mode`）  
   - 跨上下文删除：摘要标明 folder / scope；**typed confirm**——用户必须真正输入风险 folder 名或 `DELETE`（可输入 UI，不是仅文案提示）；同上下文/本地清理保持原有 `y` 确认流

5. **修改 = 外部编辑器 + 临时文件**  
   Secure Note / SSH Hosts：temp file → 编辑 → 直接写回远端；**不自动种/更新本地同步根**（用户需 pull）。  
   **禁止**在 TUI 内嵌密码式输入或多行编辑器。

6. **`n` / `N` 登记（归属由光标决定；Processor 同级）**  
   - Remote 页 `n`：归属 = **光标所在 folder**（树上 folder 分组节点是其所有子孙节点 ID 的前缀，直接按前缀反推）；表单内**不再选归属**，归属不对就 `Esc` 退出、移动光标重按 `n`  
   - 光标停在点类型目录（`.env` / `.gcm` / `.sshkey` 等）之下时，类型阶段默认选中该 Processor  
   - 光标落在分区根 / Dec vault / 「无文件夹」等归属不唯一的位置：**不开表单**，只提示移动光标或按 `N`  
   - Remote 页 `N`：手输 folder 名，用于新 bundle（`bundle/<名>`）、新 project folder、或尚未出现在树上的空 folder  
   - 归属不再需要枚举远端 folder，`n` / `N` 不触发 Bitwarden 解锁；登记表单在 Remote 页整页渲染，任何阶段 `Esc` 可退出  
   - Processor 同级：`note` / `.env` / `.gcm` / `.sshkey`；各自声明名称规则、来源控件与 Bitwarden Writer（Secure Note 或 SSH Key Item）  
   - 内容来源由 Processor 声明：Note 类为外部编辑器 / 路径 / 系统选文件；`.sshkey` 为本机生成 / 路径 / 系统选文件  
   - **不强制**落本地同步根（Note）；SSH Key 创建成功后按现有契约尝试落地 `~/.ssh`（已存在则不覆盖）  
   - `N` 指定的 folder 允许尚不存在：提交时按需创建 Bitwarden folder（Secure Note 与 SSH Key Item 两条 Writer 同此契约）  
   - Remote 页 `a`/`A` = 全选/全不选；Project 页 `A` 仍为「本地同步根已有文件 → 登记到对应 SyncTarget」，归属仍从 `SuggestSecretTargets` 轮转选择

7. **进入即尝试含远端列表**  
   Remote 进入时默认 `includeRemote=true`；`r` 强制刷新。切页不打断在飞 IO（见 TUI §5.5）。浏览孤儿 **不** 写回 `known_secret_bundles`。

8. **删除收敛（与幽灵复活相关）**  
   远端删除 bundle / secrets 时尽量：摘 `projects/*.yaml` 残留声明、清 `known_secret_bundles` / 平面 `enabled_bundles`、清本地 secrets 同步根（按 Mode）；避免 push 再把已删包推回。

9. **退役条件（已完成）**  
   页签字符串不再出现 `Delete`；包内标识符可短期保留 `DeleteCandidate` 等。

## 被否方案

| 方案 | 否决理由 |
|------|----------|
| Delete 与 Remote 双页并存 | 能力重叠、导航噪声 |
| 新增 Vault 总览侧栏页 | 用户反对；能力应在 Remote 内完成 |
| Hosts 编辑放在 Delete / Settings | 与「删」或「连接配置」语义冲突 |
| TUI 内嵌编辑 notes | 与 Project vars 决策冲突 |
| 远端删 + 本地清默认同一事务 | 误删风险；与用户心智不一致 |
| 编辑顺带种本地盘 | 其它项目裸 folder 无可靠 LocalRoot；与「pull 才更新本地」不一致 |
| 仅文案提示跨上下文风险 | 不足以防误删；必须真正输入确认 |
| 登记表单内轮转选择归属 | 候选几十个、tab 轮转难定位，且要为此枚举远端 folder（可能触发解锁）；光标本就停在目标 folder 上 |
| `.sshkey` 从类型列表剔除、只提示去 Bitwarden Web 手建 | 同一入口出现「有的类型能建、有的不能」的断层；SSH Key 与 note / `.env` / `.gcm` 是同级 Processor，只是 Writer 不同 |
| 为 `.sshkey` 做一条专用登记旁路 | 会长出第二套状态机；来源差异（本机生成）应由 Processor 声明，而不是分叉流程 |
| 登记前要求用户先在远端建好 folder | `N` 的语义就是新 folder；建 folder 是登记的一部分 |

## 后果

- 用户话术：删/改**远端** → Remote 远端分区；清**本机残留** → Remote 本地分区；启停包 → Bundles / Settings；登记到已有 folder → 光标停到该 folder 后 Remote `n`；登记到新 folder → Remote `N`。
- 快照与集成测试页名从 `Delete` 改为 `Remote`。
- 实现落点：`internal/app/remote_inventory.go`、`remote_edit.go`、`remote_register.go`、`delete_typed_confirm.go`；`internal/tui/delete_page.go`、`delete_tree.go`、`add_secret.go`、`file_picker.go`；`internal/secrets` `processor.go`、`sshkey_material.go`、`ListAllFolderNames` / `ListUnfiledItems` / `CreateSSHKey`。
