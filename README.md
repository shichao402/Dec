# Dec

个人 AI 资产与密钥的云端仓库。资产和密钥有一个唯一的云端权威副本，各台机器只保留一个超薄的通讯代理，负责把云端内容写到本机落点。

只有一个用户，就是作者本人。不设账户体系，已登记的设备就是「我」。

## 当前状态

已完成断代升级。旧一代的重客户端架构（Console 桌面应用、本机 `dec-server`、registry 分发、Bitwarden 运行时依赖）已废弃，不再维护。

| | 旧一代（已废弃） | 当前 |
|---|---|---|
| 人机入口 | Tauri Console | 托盘窗口，或浏览器 `http://127.0.0.1:43679/admin` |
| 本机服务 | `dec-server` gRPC 47653 + MCP 47654 | `dec-agent` HTTP 43679 |
| 资产组织 | `provides` 映射 + 四象限 | bundle |
| 密钥 | Bitwarden 运行时依赖 | 根密钥加密后与资产同存服务端 git |
| 云端 | 无 | `https://dec.firoyang.com` |

## 快速开始

### 安装代理

任何一台新设备，浏览器直接打开 `https://dec.firoyang.com` 就是安装指引：当前要求版本、每个平台的一行安装命令、登记方式、装完怎么确认。`/download` 是同一个页面。

macOS 与 Linux：

```bash
curl -fsSL https://dec.firoyang.com/install.sh | bash
```

Windows（PowerShell）：

```powershell
iwr -useb https://dec.firoyang.com/install.ps1 | iex
```

两个脚本都由服务端自托管，与二进制同源，装客户端不依赖 GitHub 可达。它们做同样几件事：判定系统与架构、比对本机与服务端要求的版本、下载代理与托盘并校验 sha256、配置服务地址、引导登记、注册服务与自启、最后确认代理在线。装到 `~/.local/bin` 或 `%LOCALAPPDATA%\Programs\dec`，全程不需要 sudo 或管理员权限。

登记需要 Bitwarden 邮箱、主密码和两步验证码，由 `dec-agent` 直接向终端读取，不经过脚本，也不进命令行参数。脚本默认按「已有一台设备」处理；只有当这台是本服务的第一台设备时，先执行 `export DEC_INITIALIZE=1`（Windows 用 `$env:DEC_INITIALIZE=1`）再运行安装命令。

代理装在用户级位置，不需要管理员权限：

| 平台 | 二进制 | 自启方式 |
|---|---|---|
| macOS | `~/.local/bin/dec-agent` | launchd `com.dec.agent.plist` |
| Linux | `~/.local/bin/dec-agent` | systemd 用户单元 `dec-agent.service` |
| Windows | `%LOCALAPPDATA%\Programs\dec\dec-agent.exe` | 计划任务 `DecAgent` |

卸载用 `dec-agent uninstall`。二进制不签名，Windows 首次运行会在 SmartScreen 拦一下，点「更多信息 → 仍要运行」。

第一台设备用 `initialize`（生成根密钥并写入 Bitwarden 冷备），此后新增设备用 `enroll`。服务端已经初始化过，新机器一律走 `enroll`。

### 管理面

```
http://127.0.0.1:43679/admin
```

本机 `dec-agent` 提供页面，每次操作由它带设备证书转发到云端。共有六个页面：概览、设备、资产、密钥、历史、项目。

完整说明见 [docs/USAGE.md](docs/USAGE.md)。

### 确认代理在跑

```bash
curl -s http://127.0.0.1:43679/api/status
```

返回里有 `server_url`、`device_id`、`aligned`，说明连接正常。`aligned` 为 `false` 时收不到下发，需要按提示更新代理。

### 目录

| 路径 | 内容 |
|---|---|
| `~/.dec-agent/` | 代理状态：设备证书、根密钥、本地缓存 |
| `~/Library/LaunchAgents/com.dec.agent.plist` | 代理的 launchd 服务（macOS） |
| `~/.local/bin/dec-agent` | 代理二进制 |

## 架构要点

云端是唯一 SSOT。资产和加密后的密钥存放在服务内部的 bare git 仓库里，由服务自身提交，不对外开放任何 git 协议。

代理不做任何决策。它只负责鉴权、安全通讯，以及把云端内容写到白名单落点。白名单内置在代理二进制里，云端改不了。

所有通道单向。读取链路从云端到设备，本地副本只读，本地改动不回收。写入链路是独立的同步请求，直接进云端，不经过本地文件。

版本由服务端规定。以服务方式安装的代理会自动更新到要求版本（清单经 ed25519 签名校验，失败自动回滚，`DEC_AUTO_UPDATE=0` 可关闭）；前台运行或本地构建的代理不对齐时会被拦住，并提示从服务端下载要求版本。

安全模型：服务端失陷视同所有已登记设备失陷。服务的部署链路安全由使用者自行保证。客户端不签名，不设发布密钥。

需求与已定决策的完整列表见 `.agent/dec-cloud/requirements.md`。

## 代码结构

| 目录 | 职责 |
|---|---|
| `cmd/dec-agent/` | 代理入口 |
| `internal/cloud/` | 云端核心：资产存储、密钥加密、渲染、git 提交 |
| `internal/cloudsvc/` | 云端协议方法实现 |
| `internal/proto/` | 协议定义 |
| `internal/secrets/` | Bitwarden API（复用为根密钥冷备） |
| `internal/vars/` | 变量替换 |
| `scripts/` | 部署、清理脚本 |

`client/` 是旧一代的 Tauri Console，`internal/app/`、`internal/tui/` 等服务于旧架构，均不再作为产品方向。

## 构建与测试

```bash
go build ./...
go vet ./...
go test ./...
```

部署由 GitHub CI 在打 release tag 时完成：服务端和客户端从同一 tag 构建，部署服务端的同时把客户端二进制放到服务端下载通道。期间停服，不做无中断部署。

## 日常维护

### 本机落点被改乱了

云端是权威。设备页的 `drift` 显示本机与云端的差异条数，正常情况下云端会自动补齐。长期不为 0 且 `last_error` 有内容，说明同步有问题。

### 需要重置某台设备

设备页吊销该设备。吊销立即生效，不依赖设备在线。吊销后，曾渲染到该设备的密钥会进入轮换清单，逐个处理完即可。

### 设备全丢

用 Bitwarden 主密码和 2FA 证明身份，进入恢复模式取回根密钥，再重新登记设备。

### 清理旧一代残留

旧一代产物（`~/.dec`、`dec-console.app`、各项目的 `dec-*` 落点）可用脚本清理，默认预演，保护新架构：

```bash
bash scripts/purge-legacy-dec.sh          # macOS 预演
bash scripts/purge-legacy-dec.sh --apply  # macOS 执行
```

Windows 用 `scripts/purge-legacy-dec.ps1`。需要跨机器执行时，`bash scripts/serve-purge.sh` 会起一个临时分发服务并打印可直接粘贴的命令。

## 文档

- [docs/USAGE.md](docs/USAGE.md) — 使用说明：入口、逐页操作、验证方法、清理
- [docs/history/README.md](docs/history/README.md) — 历史归档说明与新旧架构对照
- [docs/history/decisions/](docs/history/decisions/README.md) — 37 份旧一代 ADR，记录取舍依据
- `.agent/dec-cloud/requirements.md` — 当前架构的需求与已定决策

## 平台支持

服务端：Linux `amd64`。
代理：macOS `arm64` / `amd64`、Windows `amd64`、Linux `amd64`。

## 许可证

MIT
