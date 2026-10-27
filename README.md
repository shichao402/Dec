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

版本由服务端规定。代理不对齐时会被拦住，并提示从服务端下载要求版本。

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
