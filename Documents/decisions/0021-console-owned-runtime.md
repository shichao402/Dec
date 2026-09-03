# 0021 — Console 独占用户分发与目标运行时

- **状态**：已接受（连接与发布协议已实施；GitHub Actions 原生 GUI runner 已接入；同平台四件套内置 + SSH 推送已接入。relkit-serve 人页按 audience 过滤仍待发布端升级）
- **日期**：2026-09-01
- **关联**：[0018](0018-instance-lock-and-console.md)、[0019](0019-remote-provisioning.md)、[0020](0020-retire-tui.md)
- **影响范围**：Console 连接、版本门闩、目标端四件套、RUP artifact audience、人类发布页

## 决策

终端用户只下载 Dec Console。每个 Console 只内置与自身 `os/arch` 相同的 `dec`、`dec-server`、`dec-mcp`、`dec-exec`；跨平台运行时仍保留在签名 RUP manifest 中，供发起端按 SSH 目标的 `os/arch` 下载。

同一次发布的 Console 与四件套使用同一 SemVer。连接只允许版本相等：

- Console 低于服务：服务在 `Ping` 之外拒绝控制 RPC，Console 同时拒绝进入会话；
- Console 高于本机服务：用本机 listen token 停旧服务，按 Console 版本安装完整四件套，再拉起并复验；
- Console 高于 SSH 目标：探测后自动置备完整四件套；已安装设备升级无需重复确认，首次注入仍须用户显式确认；
- TLS 直连没有安装通道，版本不等时拒绝并引导改用 SSH。

`Ping` 保持可读，用于取得服务版本并给出明确升级方向，不视为获得控制权。

## 版本与完整性

每个平台的 Console 安装包内置**同 os/arch** 四件套（`resources/runtime/<os>-<arch>/` + `runtime-manifest.json` sha256）。本机首次连接或版本升级时从 `AppHandle.path().resource_dir()` 定位资源，校验摘要后以临时文件 + rename 原子释放到 `~/.dec/bin/`；Windows 先 rename-aside 并在失败时回滚。不再跑联网 `install.sh` / `install.ps1`，且拒绝从较新运行时降级的门闩保持不变。

SSH 置备由**发起端**解析目标 `os/arch`：

1. 查 `~/.dec/runtime-cache/<ver>/<os>-<arch>/` 并重验清单摘要
2. 缺失或校验失败时由发起端经 RUP 下载到临时目录，再 rename 发布缓存
3. 经系统 SSH 把四个二进制流式推到目标临时目录，chmod 后 rename 到 `~/.dec/bin`

目标机只需系统 SSH 服务端与 POSIX `sh`/核心文件工具，不需要 curl、bash 或公网访问。发起端异平台缓存未命中且无法访问 RUP 时，置备明确失败。跨平台四件套仍保留在签名 RUP manifest（`audience=runtime`），供发起端按需拉取。人类 browse 页一旦存在 `audience=user` 只展示 Console；旧 release 没有 audience 时保持原清单以免空页。该过滤须随 relkit-serve 发布端升级后才会在公网生效。

当前 relkit SDK 选择最高可达版本，不支持任意历史版本直取。缓存未命中时，只有 RUP 解析结果与 Console 钉死版本一致才下载；渠道已有更高版本则要求先更新 Console，或预先准备旧版本缓存。SSH 传输后优先用目标端 `sha256sum` / `shasum -a 256` 核对四件套，无 hash 工具时明确降级为逐组件 `--version`；激活过程中保留旧文件并在失败时 best-effort 回滚。

## 构建边界

`scripts/build-console.py` 在原生 Windows/macOS/Linux 节点：

1. 为当前平台编译同 os/arch 四件套并写入 Tauri resources，同时生成摘要清单
2. 构建 Console 安装包，归一化为 `dist/dec-console-<os>-<arch>.<ext>`
3. 构建结束清理生成资源；二进制不纳入仓库

GitHub Actions（`.github/workflows/release.yml`）每个 Console job 都准备 Go 与 relkit SDK，并用 `ubuntu-latest` / `windows-latest` / `macos-latest` / `macos-15-intel` 原生编四套 Console（Intel 那一列不能用 `macos-13`：该镜像 2025-12-04 已退役，job 只会一直排队；`macos-15-intel` 是最后一个 x86_64 macOS 镜像，2027-08 后需转 arm64）；另用 Ubuntu 交叉编全平台 runtime artifact，publish job 汇进同一次 `relkit stage`。

Tauri 安装包不能从 Linux 交叉出 NSIS/DMG。relkit-serve 人页 audience 过滤仍是发布端前置条件。

## 被否方案

**把所有平台四件套塞进每个 Console 安装包。** 否决：跨平台远端会迫使 Windows Console 携带所有 Linux/macOS 架构，包体巨大且仍会过期。同平台内置 + 发起端缓存推送已覆盖离线目标场景。

**允许版本区间兼容。** 首版否决：RPC 与 UI 同步演进，严格相等让失败发生在连接边界，避免半可用会话。未来若引入独立协议版本，再重新评估兼容窗口。

**让 `dec-mcp` 自行升级。** 否决：MCP 是 Agent 门面，不应在无用户确认时改变安装；运行时所有权归 Console。
