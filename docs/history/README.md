# 历史文档

这里保存的文档全部描述**已废弃的旧一代架构**，不再代表当前实现。

## 为什么整批归档

Dec 已完成断代升级（见 `.agent/dec-cloud/requirements.md` 已定决策 14）。旧一代是「重客户端 + Console + 本机服务 + Bitwarden 运行时依赖」的架构，新一代是「云端 SSOT + 超薄代理」：

| | 旧一代（本目录所有文档） | 新一代（当前） |
|---|---|---|
| 人机入口 | Tauri Console 桌面应用 | 浏览器 `http://127.0.0.1:8787/admin` |
| 本机服务 | `dec-server` gRPC 47653 + MCP 47654 | `dec-agent` HTTP 8787 |
| 资产组织 | `provides` 作者源映射 + public/private × global/local 四象限 | bundle，无官方与个人之分 |
| 密钥 | Bitwarden runtime 依赖 | 根密钥加密后与资产同在服务端 git |
| 分发 | registry + `publish.firoyang.com` | 服务端自身下载通道 |
| 更新 | Relkit vNext 组件体系 | GitHub CI 统一构建部署 |

旧架构的代码仍在仓库里（`client/`、`internal/app`、`internal/secrets` 等），但不再作为产品方向。`internal/secrets` 中的 Bitwarden API 实现被复用为根密钥冷备，其余部分按决策 17 重写。

## 保留它的理由

`decisions/` 里的 37 份 ADR 记录了大量取舍依据：为什么废除 `pkg/`、为什么 MCP 改 Streamable HTTP、为什么远端删除进回收站、为什么必须归属已声明的 SyncTarget。这些「当初为什么这么选、否掉了什么」在日后重遇同类问题时仍有参考价值，属于决策记录该保留的东西。

其余文档（`ARCHITECTURE.md`、`BUNDLE-SECRETS-MODEL.md`、`ROADMAP.md`、`TUI_ARCHITECTURE.md`、`UPDATE_ARCHITECTURE.md`、`research/`、`examples/`、`_pending/`）保留原文是为了在追溯 ADR 时能对上当时的语境。

## 使用须知

读这里的任何文档，都要先假定它描述的是旧系统。不要按它的路径、端口、命令、配置项去操作当前系统。

需要了解当前系统，看上一级的 [USAGE.md](../USAGE.md)。

## ADR 编号在新一代不续用

`decisions/0001` 到 `0037` 是旧一代的编号。新一代的决策记录在 `.agent/dec-cloud/requirements.md` 的「已定决策」一节，采用扁平编号，与这里不衔接。两份编号体系不要混引。
