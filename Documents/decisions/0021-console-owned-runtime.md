# 0021 — Console 独占用户分发与目标运行时

- **状态**：已接受（连接与发布协议已实施；原生 GUI runner、relkit-serve 人页过滤待接入）
- **日期**：2026-09-01
- **关联**：[0018](0018-instance-lock-and-console.md)、[0019](0019-remote-provisioning.md)、[0020](0020-retire-tui.md)
- **影响范围**：Console 连接、版本门闩、目标端四件套、RUP artifact audience、人类发布页

## 决策

终端用户只下载 Dec Console。`dec`、`dec-server`、`dec-mcp`、`dec-exec` 是目标设备运行时，不再作为人类发布页上的独立选择；它们仍保留在签名 RUP manifest 中，供 Console 按目标 `os/arch` 初始化。

同一次发布的 Console 与四件套使用同一 SemVer。连接只允许版本相等：

- Console 低于服务：服务在 `Ping` 之外拒绝控制 RPC，Console 同时拒绝进入会话；
- Console 高于本机服务：用本机 listen token 停旧服务，按 Console 版本安装完整四件套，再拉起并复验；
- Console 高于 SSH 目标：探测后自动置备完整四件套；已安装设备升级无需重复确认，首次注入仍须用户显式确认；
- TLS 直连没有安装通道，版本不等时拒绝并引导改用 SSH。

`Ping` 保持可读，用于取得服务版本并给出明确升级方向，不视为获得控制权。

## 版本与完整性

Console 调安装脚本时传 `DEC_VERSION=vMAJOR.MINOR.PATCH` 和 `DEC_NONINTERACTIVE=1`。发布为每个版本写出 `dec-runtime-manifest.json`，安装脚本据此取得该版本四件套及 sha256，不读取可能已前移的主干 `version.json` 来猜版本。

运行时 artifact 使用 `audience=runtime`；Console 原生包使用 `audience=user`。签名协议保留两类 artifact。unsigned browse 的目标规则是：一旦存在 `audience=user`，只展示用户包；旧 release 没有 audience 时继续展示原清单，避免空页。该过滤须随 relkit-serve 发布端升级后才会在公网生效，不能靠 Dec 本地 stage 单方面完成。

## 构建边界

`scripts/build-console.py` 在原生 Windows/macOS/Linux 节点构建并把产物归一化为 `dist/dec-console-<os>-<arch>.<ext>`。CNB 发布阶段会自动把这些文件加入同一签名 release。

当前 CNB Docker 节点只能构建 Go 跨平台运行时，不能可靠交叉构建 Tauri 的 Windows/macOS 安装包。原生 runner 与跨节点 `dist/` 汇聚、relkit-serve 对 audience 的人页过滤属于发布基础设施前置条件；未汇入 GUI artifact 的旧式发布仍可用于开发 channel，但不满足正式用户发布标准。

## 被否方案

**把四件套塞进每个 Console 安装包。** 否决：跨平台远端会迫使 Windows Console 携带所有 Linux/macOS 架构，包体巨大且仍会过期。

**允许版本区间兼容。** 首版否决：RPC 与 UI 同步演进，严格相等让失败发生在连接边界，避免半可用会话。未来若引入独立协议版本，再重新评估兼容窗口。

**让 `dec-mcp` 自行升级。** 否决：MCP 是 Agent 门面，不应在无用户确认时改变安装；运行时所有权归 Console。
