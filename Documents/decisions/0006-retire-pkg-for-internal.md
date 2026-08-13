# 0006 — 源码布局：废除 `pkg/`，统一到 `internal/`

- **状态**：已接受（已实现）
- **日期**：2026-08-13
- **影响范围**：原 `pkg/*` → `internal/*`；`cmd/`、文档、`.cursor/rules/` 全部 import 与路径引用

## 问题

Dec 是应用，不是对外 Go 库。继续把业务代码放在 `pkg/` 会暗示「可被外部 import」，与实际边界不符；且已有 `internal/tui`、`internal/mcp`、`internal/secrets`，目录分裂。

## 决策

**删除 `pkg/` 根目录；全部库代码迁入 `internal/`。**

| 原路径 | 新路径 |
|--------|--------|
| `pkg/app` | `internal/app` |
| `pkg/assets` | `internal/assets` |
| `pkg/bundle` | `internal/bundle` |
| `pkg/compat` | `internal/compat` |
| `pkg/config` | `internal/config` |
| `pkg/diag` | `internal/diag` |
| `pkg/editor` | `internal/editor` |
| `pkg/freshness` | `internal/freshness` |
| `pkg/ide` | `internal/ide` |
| `pkg/repo` | `internal/repo` |
| `pkg/secrets` | `internal/secrets`（含 `handler/`、`unlock/`） |
| `pkg/types` | `internal/types` |
| `pkg/update` | `internal/update` |
| `pkg/vars` | `internal/vars` |
| `pkg/version` | `internal/version` |

导入路径统一为 `github.com/shichao402/Dec/internal/...`。  
对外入口仍是 `main.go` + `cmd/`；schema 仍在 `schema/`。

## 理由

- Go 的 `internal/` 明确禁止仓外引用，符合「个人 CLI / TUI 应用」定位
- 与已存在的 `internal/tui`、`internal/mcp` 对齐，减少双根
- 新功能（如 secrets machine handlers）不再落到过时的 `pkg/`

## 被否方案

**A. 仅迁 secrets，其余留在 `pkg/`。**  
否决：半吊子布局；下次每个新包都会再问一次。

**B. 改成仓根一级包（`/app`、`/config`）。**  
否决：与 `cmd/`、`schema/`、`Documents/` 混排；`internal/` 更能表达不可导出。

**C. 保留 `pkg/` 作兼容 re-export stub。**  
否决：无外部消费者，stub 只会拖长死亡。

## 参考

- [ARCHITECTURE.md](../ARCHITECTURE.md) 模块划分
- [0005 Machine Handlers](0005-secrets-machine-handlers.md)
