# Dec 使用说明

这份文档面向使用者，回答三个问题：入口在哪、每个页面能干什么、怎么确认操作真的生效。

架构、协议、存储结构的实现细节见 `.agent/dec-cloud/requirements.md` 与 [docs/history/ARCHITECTURE.md](history/ARCHITECTURE.md)（后者描述已废弃的旧架构，仅供追溯）。

---

## 一、入口

### 管理面（日常操作都在这）

```
http://127.0.0.1:8787/admin
```

打开浏览器访问这个地址即可。它不需要登录、不需要密码、不需要证书，因为只绑定在 `127.0.0.1`，外部访问不到。

这个页面由本机的 `dec-agent` 进程提供。浏览器里的每次点击，都由 `dec-agent` 带着设备证书转发给云端 `https://dec.firoyang.com`，真正干活的是云端。

### 根页面

```
http://127.0.0.1:8787/
```

只显示代理自身的连接状态，是排障用的。日常操作走 `/admin`。

### 云端地址

```
https://dec.firoyang.com
```

它监听 443，只有三个端点允许不带设备证书访问：设备登记、客户端下载、健康检查。其余接口一律要求设备证书，直接从公网访问会失败。所以你的浏览器不能直接访问云端管理面，必须经过本机代理。

---

## 二、两个 Dec 不要搞混

| | 旧一代（已废弃） | 新一代（当前） |
|---|---|---|
| 人机入口 | Console 桌面应用 | 浏览器打开 `127.0.0.1:8787/admin` |
| 二进制 | `~/.dec/bin/dec-server`、`dec-exec`、`dec-mcp` | `~/.local/bin/dec-agent` |
| 状态目录 | `~/.dec/` | `~/.dec-agent/` |
| 系统服务 | 由 Console 拉起 | `com.dec.agent`（launchd） |
| 落点命名 | `dec-<名字>` | 无前缀，如 `relkit-ops` |

项目根目录的 `./dec` 是旧版 v1.13.146，能跑 `config`、`push`、`pull`、`search`，但它操作的是一个已经不存在的系统。不要用它。

判断当前跑的是不是新一代，看这一条：

```bash
curl -s http://127.0.0.1:8787/api/status
```

有 `server_url`、`device_id`、`aligned` 这些字段，说明新一代在正常工作。

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
- 各项目 IDE 目录里不带 `dec-` 前缀的条目（如 `relkit-ops`）

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

先确认代理在跑：

```bash
launchctl list | grep dec.agent
```

没有输出说明服务没起来，重新加载：

```bash
launchctl load ~/Library/LaunchAgents/com.dec.agent.plist
```

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
