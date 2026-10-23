# 需求文档：dec-agent 真实 MCP 入口与源码断代

## 引言

Dec Cloud 的生产链路已经切到新架构（CI 只构建 `dec-cloud` 与 `dec-agent`），但留下两处未闭环：

1. `dec-agent` 的 `/mcp` 只是一个 JSON 转发壳，不讲 MCP 协议。标准 MCP 客户端的 `initialize` 握手会被原样当作云端的“首次初始化”方法转发。真正的 MCP 工具实现（`internal/agenttools`、`internal/servicehost/mcp_http.go`）只服务于旧一代的 `dec-server`。代码注释里说的“M5 阶段”在计划中并不存在，这个推迟没有被任何任务承接。
2. 源码树没有断代：`cmd/` 下仍有 `dec-server`、`dec-exec`、`dec-host-setup`、`dec-registry`，`Makefile` 默认目标仍构建旧二进制，旧包仍在仓库里。

本需求把这两处补完，并把“完成”的判定从“代码存在、单测通过”改为“真实使用方经真实链路可用”。MCP 入口是旧资产迁移（dec-cloud 计划第 12 项）的前置条件。

不在本次范围：日志输出流的整理（目前全部写入 stderr）、菜单栏或 Dock 外壳、旧资产迁移本身。

## 需求

### 需求 1：dec-agent 提供符合 MCP 协议的入口

**用户故事：** 作为使用 Claude、Codex、Cursor 等 Agent 的我，我希望 Agent 能通过标准 MCP 直接读写 Dec 的资产和密钥，以便迁移和日常维护都可以交给 Agent，而不需要我手动调用 `/api/call`。

#### 验收标准

1. WHEN 标准 MCP 客户端以 Streamable HTTP 方式连接 `http://127.0.0.1:<端口>/mcp` THEN 代理 SHALL 按 MCP 协议（JSON-RPC 2.0）完成 `initialize`、`notifications/initialized`、`tools/list`、`tools/call`。
2. 代理 SHALL 使用 go.mod 中已有的官方 MCP Go SDK 实现协议层，SHALL NOT 自行手写 JSON-RPC 解析。
3. WHEN 收到 MCP 的 `initialize` THEN 代理 SHALL 在本地完成握手，SHALL NOT 把它转发给云端的 `initialize`（首次初始化）方法。
4. 工具清单 SHALL 按新协议方法重新定义，SHALL NOT 复用旧 `internal/agenttools` 的工具语义。最小工具集为：状态查询、列出资产、读取资产、写入资产（整体替换，描述中写明“少传条目即删除”）、按路径删除资产条目、列出密钥元数据、写入密钥、删除密钥、查询历史、列出与修改项目登记及订阅。
5. 工具清单 SHALL NOT 包含明文读取密钥、设备管理、回滚、根密钥轮换（需求 18.10）。IF 通过 `tools/call` 调用了不在清单中的名称 THEN 代理 SHALL 返回 MCP 错误，而不是转发给云端。
6. 所有 MCP 调用转发到云端时，来源 SHALL 固定为 `mcp`，由服务端再做一次权限判定。本地清单只是第一道限制，SHALL NOT 被视为安全边界。
7. 每个工具 SHALL 带有输入 schema 和中文描述，描述中写明副作用：写入后会立即推送到在线设备，并会进入审计与历史。
8. WHEN 代理尚未登记、版本不对齐或服务端处于维护状态 THEN 工具调用 SHALL 返回能够指出原因的错误信息，SHALL NOT 静默返回空结果。
9. 回环入口原有的 Host 与 Origin 校验 SHALL 对 `/mcp` 同样生效。不带 Origin 头的本机 MCP 客户端 SHALL 能正常连接。

### 需求 2：本机 IDE 自动接入 Dec 的 MCP

**用户故事：** 作为用户，我希望打开任何一个 IDE 时，Dec 的 MCP 已经可用，以便不用记端口、不用手动改配置。

#### 验收标准

1. 代理 SHALL 把一个名为 `dec`、指向本机 `/mcp` 的内置 MCP 条目写入本机已安装 Agent 宿主（Claude、Codex、Cursor）的用户级 MCP 配置。
2. 写入时 SHALL 只覆盖 `dec` 这一条，SHALL NOT 改动配置文件中的其他条目（需求 6.3）。
3. IF 某个宿主的用户级配置目录不存在 THEN 代理 SHALL 跳过该宿主，SHALL NOT 为其创建目录。
4. WHEN 代理的监听端口变化 THEN 内置条目 SHALL 在下一轮同步中更新为新地址。
5. WHEN 执行 `dec-agent uninstall` THEN 代理 SHALL 移除所有宿主中的 `dec` 条目。
6. 代理 SHALL 提供 `dec-agent mcp-config` 命令，用来打印当前条目内容与已写入的宿主列表，方便排查。

### 需求 3：源码断代

**用户故事：** 作为维护者，我希望仓库里只剩新一代的代码，以便不会误用、误构建或误参考旧架构。

#### 验收标准

1. 删除前 SHALL 在当前 HEAD 打 `legacy-final` 标签，旧代码只通过 git 历史保留。
2. 仓库 SHALL 删除 `cmd/dec-server`、`cmd/dec-exec`、`cmd/dec-host-setup`、`cmd/dec-registry`，以及所有不再被 `cmd/dec-cloud`、`cmd/dec-agent` 传递依赖的 `internal/` 包和 `tools/` 下的旧工具。
3. 计划中约定保留的模块（`internal/secrets` 的 Bitwarden API、可复用的渲染与 vars 模块）IF 仍被新代码引用 THEN SHALL 保留，否则 SHALL 一并删除。
4. `Makefile` 的默认构建、测试、安装目标 SHALL 只面向 `dec-cloud` 与 `dec-agent`。
5. 仓库 SHALL 新增一个依赖守卫测试：IF `dec-cloud` 或 `dec-agent` 的依赖图中出现已删除包的路径前缀 THEN 测试 SHALL 失败。
6. 删除完成后，`go mod tidy`、`go build ./...`、`go vet ./...`、`go test ./...` SHALL 全部通过，`go.mod` 中只被旧代码使用的依赖 SHALL 被移除。
7. 断代 SHALL NOT 改变线上行为：CI 产物的文件名、部署脚本、协议版本保持不变。

### 需求 4：以真实链路验收

**用户故事：** 作为用户，我希望“做完了”有一个我自己也能跑的客观判据，以便不再出现“标记完成但其实用不了”的情况。

#### 验收标准

1. 仓库 SHALL 提供 `scripts/acceptance.sh`，用真实的 MCP 客户端（基于官方 SDK 写的小程序或现成 CLI）对本机运行中的代理执行：握手、`tools/list`、状态查询、列出资产、读取 relkit、写入一个临时测试 bundle、确认它落地、再删除该 bundle 并确认落点清理干净。
2. 验收脚本 SHALL 针对生产云端运行，结束后 SHALL 不留下任何测试资产。IF 中途失败 THEN 脚本 SHALL 尝试清理，并打印残留项。
3. 脚本 SHALL 同时检查管理面 `/admin` 可访问、`/api/status` 显示 `aligned: true`，并检查需求 2 中各宿主的 `dec` 条目存在。
4. 本需求的每条验收标准 SHALL 对应验收脚本中的一项检查，或者对应一个具名测试。IF 某条标准两者都没有对应 THEN 该条 SHALL 视为未完成。
5. 代码中 SHALL NOT 出现“后续阶段再接上”这类没有对应任务的推迟注释。确实需要推迟的项目 SHALL 列入本文档的“不在本次范围”一节。

---

## 验收对照表

每条验收标准对应的脚本检查或具名测试。脚本检查指 `scripts/acceptance.sh`（经 `dec-agent acceptance` 调用 `internal/acceptance`），测试指 `go test` 可单独运行的具名用例。

### 需求 1：MCP 入口

| 标准 | 对应项 | 类型 |
|---|---|---|
| 1.1 Streamable HTTP 完成 initialize / notifications/initialized / tools/list / tools/call | `TestMCPHandshakeIsLocal`（本地完成握手）；脚本检查「MCP 握手」 | 具名测试 + 脚本 |
| 1.2 用官方 SDK 而非手写 JSON-RPC | `internal/agent/mcp.go` 只经由 `mcp.NewServer` / `mcp.NewStreamableHTTPHandler` 构造；`TestMCPHandshakeIsLocal` 用 `mcp.NewClient` 连入，握手成功即证明协议由 SDK 实现 | 具名测试 |
| 1.3 握手本地完成、不转发云端 initialize | `TestMCPHandshakeIsLocal`；脚本检查「MCP 握手」返回 `serverInfo=dec` | 具名测试 + 脚本 |
| 1.4 最小工具集（状态/列资产/读资产/写资产并写明「少传条目即删除」/按路径删条目/列密钥元数据/写密钥/删密钥/历史/项目登记/订阅） | `TestMCPToolsList`（逐项校验名称与描述关键词，含写资产的整体替换语义）；脚本检查「tools/list 含全部预期工具」 | 具名测试 + 脚本 |
| 1.5 不含明文读密钥 / 设备管理 / 回滚 / 轮换根密钥；清单外名称返回 MCP 错误且不转发 | `TestMCPBannedToolNotCallable`、`TestMCPToolsList` 的黑名单断言；脚本检查「被禁工具不可见」 | 具名测试 + 脚本 |
| 1.6 转发来源固定 `mcp` | `TestMCPForwardsWithMCPPayload`、`TestMCPWriteAssetPayload` | 具名测试 |
| 1.7 每个工具有输入 schema 与中文描述，写明推送与审计副作用 | `TestMCPToolsList` 断言每个工具的 `InputSchema` 非空、`Description` 含中文与副作用措辞 | 具名测试 |
| 1.8 未登记 / 版本不对齐 / 维护中返回可定位原因而非空结果 | `TestMCPErrorMessages`；脚本检查「dec_status」读到 `server` 与 `required` | 具名测试 + 脚本 |
| 1.9 Host 与 Origin 校验对 `/mcp` 生效，不带 Origin 的本机客户端可连 | `TestMCPRejectsExternalOrigin`、`TestMCPAcceptsNoOrigin`；脚本全程使用不带 Origin 的官方 SDK 客户端 | 具名测试 + 脚本 |

### 需求 2：本机 IDE 自动接入

| 标准 | 对应项 | 类型 |
|---|---|---|
| 2.1 把 `dec` 条目写入 Claude / Codex / Cursor 用户级配置 | `TestUpsertJSONCreatesFile`、`TestUpsertTOMLAppendsWhenMissing`；脚本检查「各宿主 dec 条目」 | 具名测试 + 脚本 |
| 2.2 只覆盖 `dec` 一条，不动其他条目 | `TestUpsertJSONOnlyTouchesDec`、`TestUpsertTOMLOnlyTouchesDec` | 具名测试 |
| 2.3 配置目录不存在则跳过，不创建目录 | `TestSyncSkipsMissingHosts` | 具名测试 |
| 2.4 端口变化下一轮更新 | `TestSyncUpdatesPort` | 具名测试 |
| 2.5 `uninstall` 移除所有宿主 `dec` 条目 | `TestRemoveHostsRemovesOnlyDec`、`TestDeleteJSONOnlyRemovesDec`、`TestDeleteTOMLRemovesOnlyDec` | 具名测试 |
| 2.6 提供 `dec-agent mcp-config` | `TestInspectHostsReportsThreeStates`（命令的全部判断逻辑即 `InspectHosts` 的三态输出）；命令本体见 `cmd/dec-agent/main.go` 的 `cmdMCPConfig`，本轮由脚本「各宿主 dec 条目」从外部验证 | 具名测试 + 脚本 |

### 需求 3：源码断代

| 标准 | 对应项 | 类型 |
|---|---|---|
| 3.1 删除前在 HEAD 打 `legacy-final` 标签 | `TestLegacyFinalTagExists`（标签缺失即失败） | 具名测试 |
| 3.2 删除 4 个旧 cmd 与不再被传递依赖的 `internal/`、`tools/` | `TestLegacyPackagesGone`（断言目录不存在） | 具名测试 |
| 3.3 约定保留的模块按引用情况保留或删除 | `TestNoLegacyDependencies`（依赖图中不得出现旧包前缀） | 具名测试 |
| 3.4 Makefile 只面向 `dec-cloud` 与 `dec-agent` | `TestMakefileOnlyBuildsNewGeneration`（Makefile 不得引用旧入口，且必须含两个新入口） | 具名测试 |
| 3.5 新增依赖守卫测试 | `TestNoLegacyDependencies`、`TestOnlyNewGenerationCommands` | 具名测试 |
| 3.6 `go mod tidy` / `build` / `vet` / `test` 全通过 | 全量命令见下「验证命令」；发布 CI 的「测试」步骤即 `go test ./...` | CI + 人工执行 |
| 3.7 断代不改变线上行为（产物名、部署脚本、协议版本不变） | `TestReleaseArtifactNamesUnchanged`（产物名与平台组合）；线上 `healthz` 的 `protocol` 仍为 `1`，与断代前一致 | 具名测试 + 脚本 |

### 需求 4：真实链路验收

| 标准 | 对应项 | 类型 |
|---|---|---|
| 4.1 提供 `scripts/acceptance.sh`，跑完整链路 | 脚本本身；步骤「MCP 握手」「tools/list 含全部预期工具」「dec_status」「dec_list_assets」「读取 relkit」「写入临时 bundle」「临时 bundle 落地本机」「删除临时 bundle」「临时 bundle 已从列表消失」「落点已清理」 | 脚本 |
| 4.2 针对生产运行，结束不留测试资产，失败也清理并打印残留 | 本机模式：`cleanup` 无论成败都执行，`Result.Residue` 有残留时退出码 3。线上模式：`scripts/acceptance-prod.sh` 直接查生产服务器的资产目录，验收残留 bundle 必须以 `dec-acceptance` 开头且一个不剩 | 脚本 |
| 4.3 检查 `/admin`、`/api/status` 的 `aligned: true`、各宿主 `dec` 条目 | 本机模式：步骤「/admin 可访问」「代理已登记且 aligned」「各宿主 dec 条目」。线上模式：确认 `/admin` 与 `/mcp` 在公网返回 404（它们只应存在于代理回环），`hello` 的 `aligned` 为 true | 脚本 |
| 4.4 每条标准都要有对应检查，否则视为未完成 | 本表 | 本文档 |
| 4.5 不得有无对应任务的推迟注释 | `TestNoOrphanDeferralComments` | 具名测试 |

### 验证命令

```bash
# 全量构建与测试（与 CI 同环境）
GOWORK=off go mod tidy
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test ./...

# 本机完整链路（需要本机代理在跑；会临时写入并清理）
./scripts/acceptance.sh

# 线上验收（在服务器上查生产真实状态，只读）
./scripts/acceptance.sh --remote --key ~/.ssh/dec_cloud_ops
```

本轮的实际执行记录：上述命令全部通过；本机模式退出码 0，16 项检查全部 PASS；线上模式退出码 0，39 项检查全部 PASS（覆盖服务状态、对外端口、hello 与版本拦截、15 个方法无证书一律拒绝、`/mcp` 与 `/admin` 公网 404、证书签发与有效期、SSH 加固、资产残留、审计提交、下载 sha256 与发布清单一致）。

### 两种模式的分工

| | 本机模式 | 线上模式 |
|---|---|---|
| 验什么 | MCP 协议与落盘链路 | 生产服务器此刻的事实 |
| 依赖 | 本机代理在跑 | 只要能 SSH 到服务器 |
| 写操作 | 写入并删除临时 bundle | 只有一次 `hello`（更新在线时间） |
| 代表性 | 证明「这台机器能用」 | 证明「服务端那台机器是对的」 |

MCP 入口只存在于代理里，云端不提供 `/mcp`。所以「用 MCP 走完整链路」这件事只能在有代理的机器上做；线上模式补的是另一半——服务端本身的配置、加固与残留。两者都跑过，才算这条需求真的成立。

## 不在本次范围

- 云端管理面的 UI 改造：本轮只保证 `/admin` 可访问与现有页面功能不变。
- 多用户 / 多租户隔离：仍是单用户模型。
- 宿主配置的非 macOS 验证：`hosts` 表的路径都是相对家目录的点目录，理论上跨平台可用，但本轮只在 macOS 上实测。
