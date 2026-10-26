# Dec 使用说明

这份文档面向使用者，回答三个问题：入口在哪、每个页面能干什么、怎么确认操作真的生效。

架构、协议、存储结构的实现细节见 `.agent/dec-cloud/requirements.md` 与 [docs/history/ARCHITECTURE.md](history/ARCHITECTURE.md)（后者描述已废弃的旧架构，仅供追溯）。

---

## 一、入口

### 管理面（日常操作都在这）

最省事的是双击托盘图标（下一节）。直接用浏览器的话，有三个地址，打开的是同一个页面：

| 地址 | 什么时候用 |
|---|---|
| `https://local.dec.firoyang.com/admin` | 好记、有正规证书，首选 |
| `http://dec.localhost:8787/admin` | 证书还没下发或过期时的兜底，不依赖任何配置 |
| `http://127.0.0.1:8787/admin` | 最原始的地址，永远可用 |

三个地址都只落在本机回环上，外部访问不到，所以不需要登录、密码或客户端证书。

- `local.dec.firoyang.com` 是一条公网 DNS 记录，在任何设备上都解析到 `127.0.0.1`，也就是"你自己这台机器"。它的证书由云端签发并自动下发到每台设备，过期前会自动续期。如果 443 端口被别的程序占用，地址会变成 `https://local.dec.firoyang.com:8787`，以托盘提示和管理面「概览」上显示的为准。
- `dec.localhost` 由浏览器强制解析到本机（RFC 6761），不经过 DNS。

这个页面由本机的 `dec-agent` 进程提供。浏览器里的每次点击，都由 `dec-agent` 带着设备证书转发给云端 `https://dec.firoyang.com`，真正干活的是云端。

### 托盘图标

`dec-tray` 是一个只负责显示的小程序（macOS 在菜单栏，Windows 在任务栏右下角）。它不保存任何数据，退出它也不影响代理和同步。

| 图标 | 含义 |
|---|---|
| 绿 | 在线且与云端对齐 |
| 黄 | 本机证书需关注（快到期、已过期或签发失败）；`dec.localhost` 地址仍可用 |
| 红 | 同步出错、版本未对齐，或重启后 30 秒内没恢复 |
| 紫 | 云端维护中 |
| 蓝色空心环 | 正在重启代理 |
| 灰 | 代理没在跑 |

同时有多种问题时只显示最要紧的一种，顺序是：维护 > 同步出错 > 版本未对齐 > 证书 > 正常。鼠标悬停可以看到具体原因和当前访问地址。证书问题会额外弹一次系统通知，同一个问题不会反复弹。

操作：

- 双击：打开管理面（自动选当前可用的最佳地址）。
- macOS 单击，或 Windows 右键：弹出菜单。菜单里可以查看状态、查看日志、重启代理（代理没在跑时显示为"启动代理"）、退出托盘。
- macOS 上按住 Control 再单击，可以不等双击判定、立即弹出菜单。

安装：把同平台的 `dec-tray-*` 文件放到 `dec-agent` 所在目录（通常是 `~/.local/bin/`），再执行一次 `dec-agent install`，托盘会随登录自动启动。不想要托盘可以用 `dec-agent install --no-tray`。`dec-agent uninstall` 会把托盘一起卸掉。

托盘自己崩溃时会被系统自动拉起；在菜单里点"退出"则不会被拉起，直到下次登录。

### 根页面

```
http://127.0.0.1:8787/
```

只显示代理自身的连接状态，是排障用的。日常操作走 `/admin`。

### 云端地址

```
https://dec.firoyang.com
```

它监听服务器本机的 `127.0.0.1:8444`，公网 443 由 nginx 按域名原样透传过来（nginx 不解密）。只有三个端点允许不带设备证书访问：设备登记、客户端下载、健康检查。其余接口一律要求设备证书，直接从公网访问会失败。所以你的浏览器不能直接访问云端管理面，必须经过本机代理。

---

## 二、两个 Dec 不要搞混

| | 旧一代（已废弃） | 新一代（当前） |
|---|---|---|
| 人机入口 | Console 桌面应用 | 浏览器打开 `127.0.0.1:8787/admin` |
| 二进制 | `~/.dec/bin/dec-server`、`dec-exec`、`dec-mcp` | `~/.local/bin/dec-agent` |
| 状态目录 | `~/.dec/` | `~/.dec-agent/` |
| 系统服务 | 由 Console 拉起 | `com.dec.agent`（launchd） |
| 落点命名 | `dec-<名字>` | 无前缀，直接用资产名 |

项目根目录的 `./dec` 是旧版 v1.13.146，能跑 `config`、`push`、`pull`、`search`，但它操作的是一个已经不存在的系统。不要用它。

判定当前跑的是不是新一代，看这一条：

```bash
curl -s http://127.0.0.1:8787/api/status
```

有 `server_url`、`device_id`、`aligned` 这些字段，说明新一代在正常工作。

---

## 二点五、MCP：让 Agent 直接读写 Dec

`dec-agent` 在 `http://127.0.0.1:8787/mcp` 上提供标准 MCP（Streamable HTTP）入口。Claude Code、Codex、Cursor 这些 Agent 只要配置了这一个地址，就能自己读写你的资产与密钥元数据，不需要你手动点管理面。

### 自动接入，不用手改配置

代理每次启动都会把一条名为 `dec` 的内置条目写进本机已安装宿主的用户级配置：

| 宿主 | 配置文件 |
|---|---|
| Claude Code | `~/.claude.json` |
| Codex | `~/.codex/config.toml` |
| Cursor | `~/.cursor/mcp.json` |

三条规则，都是为了不弄坏你已有的东西：

- 只覆盖 `dec` 这一条，其它 MCP 服务器原样保留，连键顺序都不动。
- 宿主目录不存在就跳过，不替你创建 `~/.claude` 之类的空目录。
- 端口变了，下一轮启动会把地址更新成新端口。

### 排查命令

```bash
dec-agent mcp-config
```

它会打印当前条目内容、写到哪个文件、以及每个宿主的状态（已写入 / 已跳过 / 失败）和原因。Agent 里看不到 Dec 时先跑这条。

常见情况：

- 显示「配置目录不存在」：对应 IDE 没装，这是预期行为。
- 显示「已跳过：配置里没有 dec 条目」：代理还没 restart 过。重启 `com.dec.agent` 后再看。
- 显示「失败」：多数是配置文件本身不是合法 JSON（手工编辑漏了逗号）。代理不会覆盖坏文件，按提示修好再重启。

### 工具清单

| 工具 | 作用 | 副作用 |
|---|---|---|
| `dec_status` | 服务端版本、要求的代理版本、协议版本、是否维护中、本机是否已对齐 | 无 |
| `dec_list_assets` | 列出所有 bundle 与条目 | 无 |
| `dec_read_asset` | 读一个 bundle 的完整内容 | 无 |
| `dec_write_asset` | 整体替换一个 bundle（少传的条目即删除） | 立即推送 + 进审计 |
| `dec_delete_asset_item` | 按路径删一个条目 | 立即推送 + 进审计 |
| `dec_delete_bundle` | 删除整个 bundle（目录与 manifest 一起删） | 立即推送 + 进审计，不可逆 |
| `dec_list_secrets` | 列出密钥元数据 | 无 |
| `dec_put_secret` | 写入或更新密钥值 | 立即推送 + 进审计 |
| `dec_delete_secret` | 删除密钥 | 立即推送 + 进审计 |
| `dec_history` | 查资产或密钥的改动历史 | 无 |
| `dec_list_projects` | 列出项目登记 | 无 |
| `dec_put_project` | 新增或修改项目登记 | 进审计 |
| `dec_delete_project` | 删除项目登记 | 进审计 |
| `dec_list_subscriptions` | 查看设备订阅了哪些 bundle | 无 |
| `dec_set_subscriptions` | 整体替换设备订阅（只能改本机设备） | 立即推送 |

`dec_status` 返回的是对外可见状态，不含根密钥 ID、初始化入口开关这类服务端内部信息，也不含 revision。要看 revision 用 `dec_list_assets` 或 `dec_read_asset`。

### 限权：本地清单只是第一道限制

以下能力**不提供**给 MCP，而且不是「藏起来」而是服务端直接拒绝：

- 读密钥明文
- 设备管理（登记、吊销、改设备类型）
- 回滚到某个历史版本
- 轮换根密钥
- 修改**其他**设备的订阅

代理里的工具清单只是减少误用的第一道门，真正的边界在服务端：即便有人绕过清单直接发请求，服务端也会按来源 `mcp` 拒绝这些操作。反过来说，写入是允许的——因为写入会进 git 历史、能再写一次覆盖，风险可控。

订阅是唯一一处「设备管理」里放开给 MCP 的部分：订阅列表只决定这台设备自己消费哪些 bundle，不涉及别的设备也不涉及密钥，所以允许设备改自己的订阅（AI 帮你把某个 bundle 加进本机订阅是日常用法）。跨设备调 `dec_set_subscriptions` 会被拒。

---

## 三、管理面逐页说明

### 概览

两张卡片。本地代理显示当前二进制版本、设备 ID、是否与服务端版本对齐、上次同步时间、上次错误。云端服务显示服务端版本、要求的代理版本、协议版本、当前 revision。

`aligned` 是 `false` 时不会收到任何下发，需要按提示更新代理。

`last_error` 是排查问题的第一站，同步失败的原因都在这里。

### 设备

列出所有已登记设备、在线状态、操作系统架构、证书到期时间、`drift`（与本机实际文件的差异条数）。

可以做的操作：吊销设备、切换设备类型（我的设备 / 只接收设备）。

吊销立即生效，不依赖设备在线。吊销后，曾经渲染到那台设备上的密钥会进入下面的轮换清单，直到你逐个处理完。

`drift` 大于 0 表示云端期望的文件与设备上的实际文件不一致。正常情况下云端会自动补齐，长期不为 0 说明同步有问题。

### 资产

所有 bundle 列表，每个显示条目数和类型。

点开一个 bundle 可以看内容、编辑文件、新增文件、保存并下发。保存会立刻生成一次 commit 并推送到在线设备。

资产里的密钥写法是占位符，明文由云端在渲染时替换进去，不会存进 git。

### 密钥

密钥列表，默认遮蔽。可以新增、修改、删除。

点「查看明文」会在审计日志里留下一条记录。这不是防你，是留痕以便追溯。

删除仍被引用的密钥默认会被拒绝，避免把正在使用的东西删掉导致渲染失败。

### 历史

选资产或密钥，输入名字，查看该对象的提交历史。每次提交都有 revision、作者、来源设备、提交说明。

可以回滚到任意历史版本。回滚不是改写历史，而是把目标内容以一次新提交写回，所以回滚本身也能再被回滚。

### 项目

三块内容：本机项目登记、云端项目与订阅、用户平面订阅。

本机项目登记决定这个项目在哪台设备的哪个路径。云端项目与订阅决定每个项目拿到哪些 bundle。用户平面订阅决定每台设备默认拿到哪些 bundle。

项目路径必须在本机已登记，否则下发无处可去。

---

## 四、怎么确认操作生效

不需要靠感觉，三步可验证。

### 第一步：确认云端收到了

```bash
curl -s -X POST http://127.0.0.1:8787/api/call \
  -H 'Content-Type: application/json' \
  -d '{"method":"list_assets","payload":{}}'
```

返回里能看到 bundle 和条目数。数字变了，说明云端已提交。

### 第二步：确认 commit 生成了

```bash
curl -s -X POST http://127.0.0.1:8787/api/call \
  -H 'Content-Type: application/json' \
  -d '{"method":"history","payload":{"kind":"asset","id":"<bundle名>"}}'
```

每次写入都应该有一条新记录。

### 第三步：确认文件落到设备上了

直接看落点目录：

```bash
ls -la <项目路径>/.claude/skills/
```

或者在管理面「设备」页看 `drift` 是否回到 0。回到 0 就是完全收敛。

---

## 五、断代清理

旧一代的产物还留在这台机器上，占 474M，并铺在 26 个项目里。清理脚本在 `scripts/`。

### 先看清楚要删什么

```bash
bash scripts/purge-legacy-dec.sh
```

默认预演，只打印清单，不动任何文件。确认清单无误再往下。

### 真正执行

```bash
bash scripts/purge-legacy-dec.sh --apply
```

### 连 .secrets 一起删

```bash
bash scripts/purge-legacy-dec.sh --apply --purge-secrets
```

默认不删 `.secrets/`，因为里面是凭据，删了不可恢复。确认里面的东西都不需要了再加这个参数。

### Windows 端

本机预演：

```powershell
powershell -ExecutionPolicy Bypass -File .\purge-legacy-dec.ps1
```

本机执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\purge-legacy-dec.ps1 -Apply
```

### 通过网络即时执行

两台机器在同一局域网时，在 macOS 这边起一个临时分发服务：

```bash
bash scripts/serve-purge.sh
```

它会打印出可以直接粘贴到 Windows 的命令，以及本机的局域网 IP。服务默认 300 秒后自动退出。

在 Windows 的 PowerShell 里执行预演：

```powershell
irm http://192.168.x.x:8765/purge-legacy-dec.ps1 | iex
```

确认清单后执行：

```powershell
$env:PURGE_APPLY='1'; irm http://192.168.x.x:8765/purge-legacy-dec.ps1 | iex
```

Windows 端也可以用同一条命令拉取并执行 macOS 脚本（需要 bash 环境，例如 Git Bash）：

```bash
curl -fsSL http://192.168.x.x:8765/purge-legacy-dec.sh | bash
```

### 清理脚本保护什么

任何情况下都不碰这些：

- `~/.dec-agent/`（新代理状态）
- `~/Library/LaunchAgents/com.dec.agent.plist`（新代理服务）
- `~/.local/bin/dec-agent`（新代理二进制）
- 各项目 IDE 目录里不带 `dec-` 前缀的条目（旧系统留下的资产副本）

判定依据是新旧两代的落点命名规则不同：旧带 `dec-` 前缀，新不带。这个差异已被实测验证。

### 清理后必须重启代理

`dec-agent` 在清理期间可能还持有旧路径的句柄。清理完成后重启：

```bash
launchctl kickstart -k gui/$(id -u)/com.dec.agent
```

然后访问 `http://127.0.0.1:8787/admin` 确认管理面仍然正常，`drift` 为 0。

---

## 六、常见问题

### 管理面打不开

先换个地址试：`https://local.dec.firoyang.com/admin` 打不开时，用 `http://dec.localhost:8787/admin`，再不行用 `http://127.0.0.1:8787/admin`。三个都打不开，说明代理没在跑：

```bash
launchctl list | grep dec.agent
```

没有输出说明服务没起来，重新加载：

```bash
launchctl load ~/Library/LaunchAgents/com.dec.agent.plist
```

### https 地址打不开，但 dec.localhost 能开

本机证书还没下发，或已过期。托盘会变黄并给出原因，管理面「概览」也会提示。证书由云端签发和续期，本机不用做什么；长时间不恢复时把托盘提示的原因发给维护者。这期间用 `http://dec.localhost:8787/admin` 不受影响。

### 托盘图标不见了

点过菜单里的"退出"后，托盘要到下次登录才会出现。想马上找回来：

```bash
launchctl kickstart -k gui/$(id -u)/com.dec.tray
```

如果提示找不到这个服务，说明托盘没装上，确认 `dec-tray-*` 文件在 `dec-agent` 同目录后重新执行 `dec-agent install`。

### 日志在哪

本机代理只写一个日志文件：`~/.dec-agent/logs/agent.log`，写满后轮转为 `agent.log.1`、`agent.log.2` 等。托盘菜单里的"查看日志"打开的就是它。旧版本留下的 `agent.err.log` 之类的文件已经不再写入，可以删掉。

### 页面显示"连接中"

代理活着但连不上云端。查 `last_error`：

```bash
curl -s http://127.0.0.1:8787/api/status
```

常见原因是版本未对齐（`aligned: false`），或设备证书过期。

### 改完资产，设备上没变化

看「设备」页的 `drift`。如果一直是 0 但文件没变，说明设备不在线，云端会在设备上线时补齐。如果 `drift` 大于 0 且长期不降，看 `last_error`。

### 想确认某个操作有没有生效

按上面「四、怎么确认操作生效」的三步走。不要凭印象判断。

### 旧 CLI 还能用，要紧吗

项目根目录的 `./dec` 是旧版，它操作的 `~/.dec` 已经不存在了，跑它会报错或做出无意义的事。清理脚本会处理掉它依赖的环境。它本身是个文件，可以直接删。
