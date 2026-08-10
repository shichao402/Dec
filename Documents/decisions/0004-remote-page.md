# 0004 — Remote 页：远端 CRUD 统一入口，Delete 退役

- **状态**：已接受（已实现）
- **日期**：2026-08-10
- **关联**：[0002-secrets-synctarget-root.md](0002-secrets-synctarget-root.md)、[0003-user-enabled-secret-bundles.md](0003-user-enabled-secret-bundles.md)
- **影响范围**：TUI 侧栏页 `Delete` → `Remote`；`pkg/app` 删除/编辑 API；Bitwarden Secure Note 与 SSH Key Notes（Hosts）写回；设计文档与规则中的「Delete 页」表述

## 问题

用户需要一个统一入口去浏览与管理远端平面上的东西：

- Dec Git vault：`bundles/<name>/` 公开资产与 cache
- Bitwarden：`bundle/<name>` Secure Notes + SSH Keys（Hosts 在 SSH Item Notes）
- project 级 secrets（特殊节点，落地在 `.secrets/project`）

旧 **Delete** 页只能做「列 + 删」，心智是负面操作台；编辑 Secure Note / SSH Hosts、以及日后新增空 Note，都不适合挤在 Confirm 流程里。继续新增独立页（如 Hosts 编辑塞进别处）会碎片化。

## 决策

**侧栏 Delete 直接改名为 Remote；删除能力并入 Remote；修改走外部编辑器（与 Project vars 的 `e` 同型）；Bundles 页仍只管项目本地 `enabled_bundles`。**

约定细则：

1. **页名 Remote，不是 Hosts / Secrets**  
   树抽象的是「远端可控对象」，含 Git 与 Bitwarden，不仅是 SSH Hosts。

2. **Bundles ≠ Remote**  
   Bundles：project 启用哪些包。Remote：包内外远端内容的浏览/增删改。启用勾选不进 Remote。

3. **修改 = 外部编辑器**  
   Secure Note：落到本地同步根文件（缺则从远端拉正文）→ `tea.ExecProcess` → 退出后 push。  
   SSH Hosts：临时文本（一行一个 host）→ 编辑 → `UpdateSSHKeyHosts` 写回 Notes，并按需刷新 `~/.ssh/config` managed 段。  
   **禁止**在 TUI 内嵌密码式输入或多行编辑器。

4. **删除仍二次确认**  
   原 Delete 的摘要 / 最终确认流程保留在 Remote（`d`），文案改为 Remote。

5. **进入即尝试含远端列表**  
   Remote 进入时默认 `includeRemote=true`（有 session / 可解锁时补 orphan）；`r` 强制刷新。切页不打断在飞 IO（见 TUI §5.5）。

6. **退役条件（已完成）**  
   页签字符串不再出现 `Delete`；用户文档与规则改指向 Remote。包内标识符可短期保留 `DeleteCandidate` 等，避免无意义大重构。

## 被否方案

| 方案 | 否决理由 |
|------|----------|
| Delete 与 Remote 双页并存 | 能力重叠、导航噪声；用户已选「直接改名」 |
| Hosts 编辑放在 Delete / Settings | 与「删」或「连接配置」语义冲突 |
| TUI 内嵌编辑 notes | 与 Project vars 决策冲突，TTY 与 Bubble Tea 打架 |
| 新增 `dec secrets edit` CLI | 违反 TUI-first |
| Remote 上勾启用 bundle | 与 Bundles / Settings 用户启用平面冲突 |

## 后果

- 用户话术：删 / 改远端 → Remote；启停本机/项目包 → Bundles / Settings。
- 需新增 Bitwarden SSH Item Notes 更新能力（此前只有 pull / delete）。
- 快照与集成测试页名从 `Delete` 改为 `Remote`。
