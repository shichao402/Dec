# 0019 — 远端设备自动置备（SSH provisioning）

- **状态**：已接受（已实现；真机与发布验收未完）
- **日期**：2026-08-31
- **关联**：[0008](0008-service-facade-split.md)、[0017](0017-local-layout-version.md)、[0018](0018-instance-lock-and-console.md)
- **影响范围**：`dec-server` 新增设备级 provisioning 能力、`dec` 新增 `__service-setup` 内部命令、`internal/config` 暴露 `management_listen` 幂等写入与校验、Console 连接页、`dec-mcp` 工具面、`scripts/install.sh`

## 问题

0018 让 Console 能管理「一台 `dec-server` 所在设备」，但只解决了**连接**，没解决**从无到有**。远端设备今天必须人工完成一串前置动作才能被管理，而这不是一处缺失，是四个约束叠加：

1. **端口不可发现**。`internal/servicehost/listen.go` 的 `defaultListenAddr = "127.0.0.1:0"` 是随机端口。本机路径靠读 `~/.dec/run/server.json` 拿 endpoint，远端拿不到该文件，Console 连接页只能让人**手填端口**——所以远端必须先人工在 `~/.dec/config.yaml` 配 `management_listen` 固定端口。
2. **没人拉起进程**。`dec-server` 的设计是由门面按需拉起（`internal/service/client.go` 的 `startServerProcess`），远端没有门面。
3. **拉起了也会消失，且没有拉起通道**。0008 定的空闲超时（`server_idle_timeout`，默认 30 分钟）会让远端进程在 Console 断开后退出，下次连过去是空的。空闲退出本身不是问题——本机也如此，靠门面按需拉起兜底；问题是远端**没有等价的拉起通道**。`client/README.md` 里「远端由该设备的服务管理器负责重启」所指的服务管理器**并不存在**，是留给人手工补的。
4. **二进制得先在那儿**。`scripts/install.sh` 只能由人登录目标机执行。

结果是「面板能管远端」在体验上退化为「先手工运维一遍，面板才能管」。同时 `dec-mcp` 完全没有这条路径。

## 决策

### 1. 能力归属：置备是服务端能力，执行者是发起端 `dec-server`

provisioning 落在 `internal/app`（新增 provisioning 包），经 `internal/servicehost/dispatch.go` 暴露为服务方法与写操作，**不在 Tauri 客户端实现**。

执行者是**发起端的 `dec-server`**：它作为 SSH 客户端连到目标机执行置备。Console 与 `dec-mcp` 都通过同一条服务端能力获得该功能，符合 0008 的门面/服务分工——客户端不执行 `internal/app`。

设备级长任务经 `RunOperation` 走进度流（与 `pull` 同类）。但 broker 的互斥键当前是 `ProjectRoot`（`RunOperation` 中 `s.broker.start(req.ProjectRoot, ...)`），置备不属于任何项目，因此使用合成键 `device:<alias>` 占位，保证同一目标机不被并发置备，且该键**不得**被当作真实路径落盘或参与项目解析。

### 2. 四步链路

**探测（只读）**：`probe_remote_host` 走 `Invoke`。检查 SSH 连通性、`uname` 得到 os/arch、`~/.dec/bin` 下四件套是否存在及版本、`git` 与 `ssh-keygen` 是否就位、`~/.dec` 可写、以及**非交互 SSH 能否拉起后台进程**（按需拉起的前提）。它同时是后续每一步的前置检查，也让 Console 在人点「部署」之前就能给出「这台机器行不行」。

探测**不再**关心 systemd user / launchd / linger 状态——远端不做常驻化，这些信息对置备决策已无影响。

**安装**：`provision_remote_host` 走 `RunOperation`，把 `scripts/install.sh` 经 stdin 注入执行（`ssh 'bash -s'`）。脚本用 `go:embed` 嵌入 `dec-server`，保证注入的脚本与仓库同源、不产生第二份副本，也不依赖目标机先能取到脚本。管道模式下 `[ -t 0 ]` 为假，脚本既有的「自动覆盖安装」与「已是最新且四件套完整则退出 0」正好提供幂等。

**配置**：不在 Go 侧手写远端 YAML。新增 `dec __service-setup` **内部命令**在目标机本地执行，用现成的 config 包幂等写入 `management_listen`（保留其余字段）。**「装二进制」由脚本负责，「写配置」由远端 `dec` 自己负责**——这样配置合并逻辑只有一份实现，不会因远端 schema 升级（0017）而漂移。

命令形态统一到既有的内部命令惯例（`__freshness-check`），**不新建 `service` 子命令族**：console-first 明确要求不新增用户面 Cobra 子命令，而这一步的调用方只有发起端的置备流程，人不会手敲它，与 `__freshness-check` 同类。SSH 非交互执行时没有 TTY，hidden 命令仍可直接调用。

固定 loopback 端口常量（可显式覆盖）。因为只监听 loopback，端口冲突风险低，而固定端口是隧道能自动找到它的前提。

**连接（按需拉起）**：建立 SSH 隧道 → `Authenticate` 换 control token → 登记设备。`connect_ssh`（`client/src-tauri/src/lib.rs`）的隧道链路已验证可用，连接页改为「填 SSH 主机即可」，端口由探测结果带出；检测到未安装时给「一键部署」入口。

### 3. 远端不做常驻化，改为 SSH 按需拉起

**远端不安装 systemd user unit / launchd plist，也不关闭空闲退出。** 远端 `dec-server` 与本机使用同一套生命周期：空闲即退出，需要时由请求方拉起。

原因是 SSH 本身就是远端的「门面拉起通道」，与本机门面调用 `startServerProcess` 等价：

- 建隧道前先 `ssh <target> 'dec-server'` 拉起（`Setsid` 已使其脱离 SSH 会话，见 `internal/service/process_unix.go`），轮询 `~/.dec/run/server.json` 就绪后再建隧道。这与 `connect_local` 的「读 metadata → 失败则 spawn → 轮询」完全同构。
- 会话期间不会被空闲退出打断：Console 解锁后持有 `KeepAlive` 长连，服务端 `KeepAlive` 已 `presence.connected()`；`presenceTracker` 在有连接时不启动定时器。
- 断开后远端自行退出，不留常驻进程。

这样做同时消掉了常驻方案里的一串附带复杂度：**不需要**为「永不退出」新增 `server_idle_timeout: off` 语义、不需要改 `presenceTracker`、不需要生成与维护两套 service manager 单元、不需要处理 linger，也不会出现「空闲退出后被 service manager 立刻重启」的空转。

代价是每次连接多一次 SSH 拉起与就绪等待（约数百毫秒到数秒），可接受；换来的是远端与本机生命周期语义一致，只有一套模型需要理解和维护。

### 4. 第一版只支持 Linux / macOS 远端

`scripts/install.ps1` 存在，但 Windows 上的 SSH 服务端形态与服务注册（SCM / 计划任务）是另一套实现，第一版不做。探测阶段识别到 Windows 目标机应明确拒绝并说明原因，而不是尝试后失败。

### 5. 安全边界

注入脚本执行本质是远程代码执行，两条硬要求：

- **产物完整性校验**。`install.sh` 当前只 `curl` 不校验，`version.json` 也只有 `version` 字段。需在发布侧为 `version.json` 增加各产物 `sha256`，`install.sh` 下载后校验；摘要缺失时降级为显式警告而非静默通过。
- **首次置备强制显式确认**。MCP 侧沿用 `dec_delete` 的 `confirmed=true` 惯例（`internal/mcp/server.go`），Console 侧对首次置备要求 typed confirm（真实键入目标主机名），不能只弹一个提示框。置备属于 0018 定义的 session 独占动作，不与其他写操作并行。

SSH 凭据只存**引用**（`~/.ssh/config` 别名或 Dec 管理的 `.sshkey` 名，见 0005），私钥内容与口令不进全局配置、不进日志。远端 `dec-server` 仍只监听 loopback，不因置备而放开非 loopback 监听——0018 的「非 loopback 必须 TLS」不变。

置备完成后远端服务仍是**锁定态**（0018），需 `Authenticate` 解锁；常驻服务重启后同样锁定。置备不携带、不缓存 Bitwarden 主密码。

### 6. 受管设备登记

`internal/types` 的 `GlobalConfig` 新增受管设备清单，与 `ManagedProjects` 同层：记录别名、SSH 目标、监听地址、标签、最近置备版本。它只作为 Console/MCP 的连接入口，不改变资产解析。「移除设备」只删登记，**不**卸载远端服务、不删远端 `~/.dec`；卸载必须是独立的显式操作。

## 实施阶段

1. ~~探测（只读，无副作用）+ Windows 目标机拒绝~~ — **已实施**（`internal/app/remote_provision.go`）
2. ~~注入安装（embed `install.sh`）+ 发布侧 sha256 与脚本校验~~ — **已实施**（`internal/app/remote_provision_install.go`、`scripts/gen_checksums.py`）
3. ~~`dec __service-setup` 内部命令（`management_listen` 幂等写入）+ SSH 按需拉起与就绪等待~~ — **已实施**（`cmd/service_setup.go`、`internal/config/management_listen.go`、`internal/app/remote_service.go`）
4. ~~设备登记 + 连接页收口 + `dec_provision_remote` MCP 工具~~ — **已实施**（`internal/config/managed_devices.go`、`client/src/pages/connect-page.tsx`、`client/src-tauri/src/lib.rs`、`internal/mcp/server.go`）

1 完成即可独立交付价值（诊断远端为什么连不上），2/3 是闭环主体，4 是体验收口。

### 实施阶段确定的取值

- **固定端口**：`RemoteProvisionPort = 47653`，即 `management_listen: 127.0.0.1:47653`。
- **SSH 客户端**：走系统 `ssh` 命令（`exec.CommandContext`），**不引入** `golang.org/x/crypto/ssh`。与 Console 的 `connect_ssh` 复用同一条信任链（`~/.ssh/config`、ssh-agent、known_hosts），Dec 不接管主机校验与密钥管理，`go.mod` 无新增依赖。
- **SSH 参数**：固定 `BatchMode=yes`（禁交互，否则目标机要求口令时置备静默挂死）、`ConnectTimeout=10`、`StrictHostKeyChecking=accept-new`。
- **探测用 `sh`、安装用 `bash`**：探测阶段不能假设目标机有 bash；而 `install.sh` 是 `#!/bin/bash` 且用了数组与 `<<<`，因此 **bash 是探测的一等阻断项**。
- **脚本行尾强制规范化为 LF**：Windows 上 git 可能按 `core.autocrlf` 把脚本 checkout 成 CRLF，喂给远端 bash 会以 `$'\r': command not found` 失败。
- **`version.json` 摘要结构**：新增 `checksums` 段，key 为产物文件名（与 `install.sh` 拼出的 `${binary}-${platform}` 一致）。摘要必须随 `ReleaseLatest` 提交，否则脚本取不到摘要、校验被降级为警告。
- **分支名注入防护**：`DEC_BRANCH` 会拼进远端命令行，字符集限制为 `[A-Za-z0-9._/-]`。
- **存活判定用 `kill -0`，不用「文件存在」**：`run/server.json` 只在正常退出时清理，进程被 `kill -9` 后会残留。仅看文件存在会误判为运行中，从而跳过拉起、去连一个没人监听的端口。
- **拉起用 `setsid nohup ... < /dev/null &`**：SSH 会话结束会向进程组发 SIGHUP，只靠 `&` 的进程会跟着死；`dec-server` 自身的 `detachedProcessAttributes` 只作用于它拉起的子进程，管不到它自己被谁拉起。stdin 必须切断，否则 SSH 会话不会返回。
- **远端命令走绝对路径 `${DEC_HOME:-$HOME/.dec}/bin/dec`**：非交互 SSH 的 PATH 通常不含 `~/.dec/bin`。
- **置备默认包含配置步骤**：`SkipConfigure` 零值为 false。装完二进制却没配固定端口的机器仍然连不上，置备只做一半没有意义。
- **SSH 连接页只保留一个目标输入**：接受 `~/.ssh/config` Host 别名、主机名、`user@host`，以及 `host:36000` 这种端口写法（变成 `ssh -p`，与 SSH 密钥落地约定一致）。密钥、代理跳转仍由系统 SSH 配置管理。gRPC 地址固定为 `127.0.0.1:47653`，不再让用户重复填写。
- **设备清单与 Console 本地连接信息分层**：`GlobalConfig.managed_devices` 是 Console/MCP 共享的受管设备 SSOT；Tauri 的 `connections.json` 只保存 UI 偏好与系统凭据库引用。加载时合并，保存 SSH 连接时 upsert 登记，删除只移除两处本机记录，不触碰远端。
- **连接前能力走精确 pre-auth 白名单**：Console 尚未连接目标机、也尚未输入 Bitwarden 主密码时，需要先通过本机 `dec-server` 探测/置备/拉起远端。通用 `Invoke` / `RunOperation` 只有在持有本机 `server.json` transport token 时才能进入，随后按业务名只放行设备生命周期方法；资产、secrets、仓库配置仍要求 `InstanceUnlocked`。

## 被否方案

| 方案 | 否决理由 |
|------|----------|
| 只在 Tauri 客户端做 SSH 注入 | `dec-mcp` 拿不到该能力；违反 0008「客户端不执行 `internal/app`」 |
| 远端 `dec-server` 监听公网直连，不走 SSH | 需要为每台设备运维 TLS 证书，攻击面显著扩大；SSH 已是既有信任通道 |
| 远端装 systemd user unit / launchd 常驻 | SSH 已是等价的按需拉起通道，常驻是多余的第二套生命周期模型；且需引入 `server_idle_timeout: off`、改 `presenceTracker`、维护两套 unit、处理 `loginctl enable-linger`，并可能「空闲退出后被立刻重启」空转 |
| 自动执行 `loginctl enable-linger` / 装 root 级 unit | 随常驻方案一并废弃；不再需要修改系统状态 |
| 第一版含 Windows 远端 | SSH 服务端与服务注册是另一套实现，会拖慢闭环 |
| 在 Go 侧直接改写远端 `config.yaml` | 绕过 config 包的 kind/version 合并（0017），远端 schema 升级后必然漂移 |
| 新建 `dec service` 用户面命令族（`service setup` / `service status`） | 违反 console-first「不新增独立 Cobra 子命令」；该步只有置备流程调用，人不手敲，与 `__freshness-check` 同类，故统一为内部 hidden 命令 |
| 把 `install.sh` 逻辑在 Go 里重写一份 | 安装逻辑分裂成两份，版本比对与幂等规则必然不一致 |

### 一处已修正的误判

本 ADR 初稿曾以「鸡生蛋：建隧道时进程必须已在；MCP 场景无人交互可兜底」为由否掉「按需拉起」，并据此选择常驻化。该理由不成立：

`ssh <target> 'dec-server'` 本身就是拉起通道，**不需要**隧道先建好，也不需要人交互——MCP 场景同样能执行 SSH。当时把「建隧道」与「拉起进程」看成必须同时满足的一步，因而误判为循环依赖；实际二者有明确先后：先拉起、等就绪、再建隧道，与 `connect_local` 的顺序完全一致。

## 后果

- 文档：`client/README.md` 中「服务管理器负责重启」应改为「由连接方经 SSH 按需拉起」；`Documents/ARCHITECTURE.md` 需补设备置备与远端按需拉起链路
- 发布流程：`version.json` 增加产物摘要，属发布侧改动，需与自更新路径一起验证
- 测试：探测结果解析、`install.sh` 管道幂等、`management_listen` 幂等写入、`device:<alias>` 互斥键不泄漏为路径、SSH 拉起后的就绪等待与超时
- 远端空闲退出后进程消失属**预期行为**，不是故障；连接失败时应先尝试拉起再报错
- 遗留：Windows 远端、批量置备多台设备、以及「希望远端常驻」的场景（如远端需被动接收任务）均留待后续决策
