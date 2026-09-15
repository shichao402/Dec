# 0027 — 更新契约只由 relkit 声明

- **状态**：已接受（已实现）
- **日期**：2026-09-15
- **关联**：[0021](0021-console-owned-runtime.md)（谁执行更新）、[0023](0023-facade-capability-parity.md)（MCP 不对称）、relkit ADR 0010 / 0012
- **影响范围**：`client/src-tauri/src/console_update.rs`、`client/src/lib/console-update.ts`、`third_party/relkit/`

## 问题

Console 自更新曾同时维护 Go helper JSON、Tauri serde struct 与前端 TypeScript 类型。空的 `releaseNotesMarkdown` 被中间层省略后，只能在运行时得到 `missing field`。如果 Dec 再声明一份 `console_update.proto`，只是把第二份契约从 struct 换成 proto，仍会漂移。

## 决策

1. 更新 IDL 只有 relkit 的 `proto/updater/v1/updater.proto`。Dec 的 `schema/` 只声明 Dec 配置与资产协议，不声明 updater 消息。
2. 产品仓不执行 updater codegen。`relkit_host.py install` 按 lock 落下 Rust facade、TypeScript bindings 与 sidecar；Dec 只通过 path dependency / import 消费。
3. Tauri 把 Rust facade 的 canonical ProtoJSON 投影交给 WebView。前端用 lock 组件中的 `CheckResultSchema` / `StatusSnapshotSchema` 解析，再按五个 oneof 变体渲染；不手写 `CheckResult`、`UpdateAvailable` 或字段镜像。
4. `CheckPolicy.after_success` / `after_failure`、`StatusSnapshot` 与引擎持久化状态负责节流和上次状态。Dec 不写 `console-update.json`，不计算下次检查时间。
5. `currentVersion` 与 `canAutoInstall` 是本机壳事实，可与 canonical 结果一起返回，但不进入 updater IDL。

## 被否方案

**在 Dec 新增 `schema/app/v1/console_update.proto`。** 否决：relkit IDL 已经声明五变体与字段语义；产品 proto 会成为第二个事实源。

**在产品构建中运行 `buf generate`。** 否决：生成器版本与插件供应链会泄漏到每个宿主，且升 lock 后仍可能忘记生成。生成物必须由 relkit release 产出并由 lock 钉住。

**保留 Go / Rust / TypeScript 三份薄 DTO。** 否决：所谓“薄”仍需逐字段同步，原始 missing-field 故障正来自这条路径。

**缺字段时在消费者使用默认值。** 否决：`mandatory=false` 等默认值会把契约错误洗成合法业务结果；缺键必须在 conformance 或严格解析时失败。

## 结果

- 改 relkit 字段或 oneof 后，Dec 前端在编译期暴露不匹配。
- Rust 与 TypeScript 通过同一组 canonical ProtoJSON fixture 对齐。
- Console 自更新不再依赖 Go helper，也不依赖当前连接的 `dec-server`。
