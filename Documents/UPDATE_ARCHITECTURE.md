# Dec 更新架构（Relkit vNext）

Dec 是 Relkit vNext 的 S3 样本：同一产品按 `os × arch × component × audience`
选择运行时组件和 Console。宿主只声明能力并生产 ArtifactSet；部署事实、密钥和发布
事务属于 `firoyang-public` Operator。

## 单一控制面

仓库根的 `relkit.project.json` 声明：

- Linux/Windows/macOS 五个目标；
- Go 与 Rust facade；
- `dec` Runtime Profile 自订阅；
- `component`、`audience` 两个额外 selector 维度；
- producer 入口：`scripts/relkit_producer.mjs`（跨平台选择 Python 后执行
  `scripts/relkit_producer.py`）。

`relkitw` 验证签名 Catalog、Operator Profile、Runtime Profile 与 immutable
resolution，再按 digest 物化 CLI、updater、Go SDK 和 Rust SDK。仓库不维护 Relkit
版本、组件 URL、endpoint、公钥或 token。

## 两条运行时链

Console 自更新：

- Tauri 通过 `.relkit/components/sdk/rust` 的 canonical facade 调用 sidecar；
- 构建脚本从 verified Runtime Bootstrap 注入目录入口、信任根、渠道和恢复信息；
- selectors：`component=console,audience=user`。

运行时套件：

- Go facade 从 linker 注入的同一 Runtime Bootstrap 构造 ClientProfile；
- components：`dec-server`、`dec-exec`、`dec-host-setup`；
- selectors：`component=<name>,audience=runtime`。

两条链都只消费 `relkit.runtime-bootstrap/1`，不得手写 endpoint、公钥或宽松 DTO。

## 构建

Operator 先 provision bootstrap 或签名 offline bundle，开发机/CI 随后执行：

```bash
./relkitw doctor
./relkitw artifacts produce \
  --version v1.2.3 \
  --channel dev \
  --target darwin-amd64
```

每个 target 独立生成一个 `relkit.artifact-set/1`：

- 三个运行时 component 各有 installer + payload；
- Windows/macOS 原生 runner 另生成 Console installer；
- Linux 只生成运行时套件。

五个 target 的 ArtifactSet 由 CI 系统传输，汇总 Job 用
`relkitw artifacts merge` 合并。合并结果的 product、version、channel 和
resolution digest 必须完全相同。

Go 构建通过 `.relkit/components/sdk/go/relkit.mod` 使用物化 SDK；Rust 构建通过
path dependency 使用物化 SDK。宿主 `go.mod`、Cargo 配置不负责钉 Relkit 版本。

## 发布边界

产品 CI 只生产和汇总 ArtifactSet。独立 Operator Job 拥有：

- publish endpoint/backend 和上传凭据；
- 产品签名私钥与 Runtime Profile 发布；
- stage → simulate → fake → publish 的事务；
- 多 backend 一致性、partial-commit 修复和 released-state 验证。

`stable/v*` 决定 channel，只保留这一个渠道；每次发布另外创建 GitHub Release，且只展示
Console installer。GitHub Release 不是 RUP 控制面。

## 首次自订阅

产品第一次 vNext 发布前还没有在线 Runtime Profile。Operator 使用产品私钥执行：

```text
relkit-ops runtime-profile create
```

生成首份签名 Profile 并放入 Operator inventory；之后 producer 才能获得 verified
Runtime Bootstrap。真实私钥只存在于本机/Operator 私密配置，不提交仓库。

## 不变量

- Console、运行时套件和 updater 必须引用同一 resolution digest；
- candidate resolution 不能产生 stable ArtifactSet；
- 缺 Runtime Bootstrap、SDK、sidecar 或任一 selector 维度时构建失败；
- 产品仓不能读取发布 token，Operator 仓不能改产品业务选择；
- 发布任一关键步骤失败不得报告成功；
- 用户端验签或协议不兼容时拒绝更新，并展示签名 Profile 中的恢复入口。

历史 consume lock、`relkit.json`、`tools/bin` 与直接 `ci release` 流程已删除；相关事故
只作为历史记录，不是当前操作入口。
