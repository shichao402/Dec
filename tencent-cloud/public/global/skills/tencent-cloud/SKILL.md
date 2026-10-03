---
name: tencent-cloud
description: >
  通过 dec-tencent-cloud 单一 MCP 操作腾讯云 CVM/COS/DNSPod/SSL/EdgeOne(Pages·Makers)/Lighthouse/CDB。
  密钥由 .config/mise/conf.d/tencent-cloud.toml 注入，敏感销毁/全开放安全组等操作已禁用。
---

# 腾讯云 MCP 运维（v0.3.0）

## 何时使用

1. **首次配置密钥**
   - 运行 `meta_sync_pkv_config`（或手动维护 `.config/mise/conf.d/tencent-cloud.toml`）
   - 密钥不入库，仅写入 gitignore 的 `tencent-cloud.toml`

2. **查看可用能力**
   - `meta_list_namespaces` — 列出 cvm / cos / dns / ssl / pages / teo / lighthouse / cdb
   - `meta_list_blocked_operations` — 查看已禁用的敏感操作

3. **常用操作**

   | 场景 | Tools |
   |------|-------|
   | 查 CVM / Lighthouse | `cvm_describe_instances`, `lighthouse_describe_instances` |
   | 实例执行命令 / 传文件 | `cvm_run_command`, `cvm_upload_file`, `cvm_download_file`（TAT 云助手） |
   | COS 对象与桶配置 | `cos_list_objects`, `cos_upload_object`, `cos_download_object`, `cos_get_bucket_cors` |
   | COS 自定义源站域名 | `cos_get_bucket_domain`, `cos_put_bucket_domain`（合并写入，Type 固定 REST，需 `confirm=true`） |
   | DNS 解析 | `dns_describe_records`, `dns_create_record`, `dns_modify_record`, `dns_delete_record` |
   | SSL 证书 | `ssl_describe_certificates`, `ssl_apply_certificate`, `ssl_deploy_certificate_instance` |
   | EdgeOne Pages/Makers 站点 | `pages_describe_projects`, `pages_create_project`, `pages_deploy_dir`（需 `confirm=true`）, `pages_describe_deployments` |
   | Pages 自定义域名 | `pages_create_zone_custom_domain`, `pages_describe_zone_custom_domains` |
   | EdgeOne 站点侧 | `teo_describe_zones`, `teo_verify_ownership`, `teo_check_cname_status` |
   | EdgeOne 免费证书 | `teo_apply_free_certificate`, `teo_check_free_certificate_verification`, `teo_modify_hosts_certificate`（均需 `confirm=true`） |
   | CDB 管控 | `cdb_describe_instances`, `cdb_describe_wan_service`, `cdb_open_wan_service`, `cdb_describe_databases`, `cdb_create_database` |
   | CDB 外网 | `cdb_open_wan_service` 开通外网；`cdb_close_wan_service` 需 `confirm=true` |
   | CDB 安全组 | `cdb_describe_security_groups` 查规则；`cdb_add_security_group_ingress` 添加入站（需 `confirm=true`，外网端口默认同时放通 TCP 3306） |
   | CDB SQL | `cdb_query_sql`（只读）, `cdb_execute_sql`（有限 DML） |

4. **同步 Dec 资产**
   - `meta_sync_dec_assets` 等价于项目根 `dec pull`

## `.config/mise/conf.d/tencent-cloud.toml` 配置项

```toml
[env]
# 必填：腾讯云 API
TENCENTCLOUD_SECRET_ID="..."
TENCENTCLOUD_SECRET_KEY="..."
TENCENTCLOUD_REGION="ap-chengdu"

# COS（对象存储）
COS_BUCKET="your-bucket-1250000000"
COS_REGION="ap-chengdu"

# EdgeOne Pages / Makers（pages_* 工具专用，与上面的 API 密钥是两套凭据）
EDGEONE_PAGES_API_TOKEN="..."
EDGEONE_PAGES_REGION="china"   # china=国内站，global=国际站

# CDB 管控 API（可选）
CDB_REGION="ap-chengdu"
CDB_INSTANCE_ID="cdb-xxxxxxxx"

# CDB SQL 直连（可选，用于 query_sql / execute_sql）
CDB_HOST="cdb-xxx.sql.tencentcdb.com"
CDB_PORT="3306"
CDB_USER="root"
CDB_PASSWORD="..."
CDB_DATABASE="mydb"
```

> **API 密钥 ≠ 数据库密码**：`TENCENTCLOUD_SECRET_ID` / `TENCENTCLOUD_SECRET_KEY` 仅用于调用腾讯云 OpenAPI；`CDB_PASSWORD` 是 MySQL 账号密码（控制台「数据库管理」创建/查看），二者不可混用。修改 DB 密码请用控制台或 CDB API（`ModifyAccountPassword` 未暴露为 MCP tool）。

> CDB 管控 API 只需 SecretId/Key + InstanceId；SQL 读写还需实例连接地址与账号密码（控制台「数据库管理」或私有网络内网地址）。

## CDB 本机 SQL 连通 Checklist

从本机 MCP 直连 CDB 外网，按序执行：

1. **`cdb_open_wan_service`** — 开通外网（若已开通可跳过）
2. **`cdb_describe_wan_service`** — 读取 `WanDomain` 与 **`WanPort`**（外网端口常为 `22915` 等，**不一定等于 3306**）
3. **`cdb_describe_security_groups`** — 确认实例关联的安全组
4. **`cdb_add_security_group_ingress`** — 放通本机公网 IP（`cidr=你的IP/32`）：
   - `port` = 上一步的 **WanPort**
   - 默认 `includeInternalPort=true` 会同时放通 TCP **3306**（外网代理需要）
   - 必须 **`confirm=true`**
5. **更新 `tencent-cloud.toml`**：
   - `CDB_HOST` = `WanDomain`
   - `CDB_PORT` = `WanPort`（勿写死 3306）
   - `CDB_USER` / `CDB_PASSWORD` = 数据库账号密码（非 API 密钥）
6. **`cdb_query_sql`** — `SELECT 1` 验证连通

仅内网场景：用 `describe_instances` 的 `Vip` 作 `CDB_HOST`，本机需 VPN 或同 VPC 跳板，无需外网与安全组步骤。

## EdgeOne 两套凭据（最容易踩的一脚）

EdgeOne 是腾讯云产品（CAM 简称 `teo`），但 Makers 的接口被拆成了两个鉴权面：

| 面 | 入口 | 凭据 | 工具 |
|---|---|---|---|
| 项目与部署 | `pages-api.cloud.tencent.com/v1` | 控制台签发的 API Token（Bearer） | `pages_*` |
| 站点、CNAME、免费证书 | 标准 OpenAPI（TC3 签名） | `TENCENTCLOUD_SECRET_ID/KEY` | `teo_*` |

**`TENCENTCLOUD_SECRET_*` 无法调用 `pages_*`**，反过来 API Token 也签不了 `teo_*`。API Token 只能在
Pages 控制台「API Token」页人工创建（有效期 1 天到 1 年），这是整条链上唯一绕不开的手工步骤；拿到后写进
`EDGEONE_PAGES_API_TOKEN`，其余全可脚本化。

## 部署静态站 Playbook（Pages / Makers）

**前置**：`EDGEONE_PAGES_API_TOKEN` 已配置。新账号通常 `teo_describe_zones` 返回 0 个站点，
**站点是绑定自定义域名时才产生的**，所以顺序不能颠倒。

1. **`pages_create_project`** — 默认 `Provider=Upload`（不接 Git，由本地产物部署）；项目名只允许
   小写字母/数字/连字符，长度 5-63
2. **`pages_deploy_dir`** — 传本地目录或单个 `.zip`，需 `confirm=true`。内部三步：取 COS 临时凭据 →
   逐文件上传 → `CreatePagesDeployment`。上限 2000 个文件 / 64MiB
3. **`pages_describe_deployments`** — 看构建状态；失败用 `pages_describe_deployment_log` 取日志地址
4. **`pages_create_zone_custom_domain`** — 绑自定义域名，返回 `Cname`、`ZoneId` 与归属校验要求
5. **`dns_create_record`** — 把域名 CNAME 到上一步返回的 `Cname`
6. **`teo_check_cname_status`** — 必须是 `active` 才算接入成功（`moved` = 没指对，`invalid` = 指到了别的域名的 CNAME）
7. **`teo_apply_free_certificate`** → **`teo_check_free_certificate_verification`** →
   **`teo_modify_hosts_certificate`**（`Mode=eofreecert`）开 HTTPS，均需 `confirm=true`

删项目、摘域名没有暴露成 tool，请用控制台。

## 部署应用 Playbook（CVM / Lighthouse）

**前置**：目标实例已安装并运行**云助手 Agent**（TAT）；实例**安全组/防火墙**须在控制台放通所需端口（MCP 不自动改 CVM 安全组）。

1. **`cvm_describe_instances`**（或 `lighthouse_describe_instances`）— 确认实例 ID 与状态
2. **`cvm_upload_file`** — 上传应用包/脚本到实例路径（base64 + TAT）
3. **`cvm_run_command`** — 解压、安装依赖、启动服务（可配合 `describe_invocation` 查看输出）
4. 需要回传日志时：**`cvm_download_file`** + **`cvm_describe_invocation`**

Lighthouse 将 `cvm.*` 换为 `lighthouse.*` 即可。

## 敏感操作（已禁用）

- **CVM/Lighthouse**：TerminateInstances、ResetInstancesPassword、对 0.0.0.0/0 开放危险端口
- **COS**：deleteBucket、批量删除；`cos_delete_object` 需 `confirm=true`
- **CDB SQL**：DROP/TRUNCATE/无 WHERE 的 DELETE
- **DNS**：批量删除；单条删除需 `domain` + `recordId`

## 密钥链路

```text
pkv get tencent-cloud env  →  .config/mise/conf.d/tencent-cloud.toml [env]  →  mise exec  →  dec-tencent-cloud
```

本机也可通过 `dec exec --bundle tencent-cloud` 注入 `.secrets` / Bitwarden 中的 `TencentAPI_SecretId` / `TencentAPI_SecretKey`（MCP 已收录这两种历史键名）。

fallback：`~/.pkv/env/HelloKnightTasker.json`；旧项目可暂留 `mise.local.toml`（MCP 读取时 conf.d 优先）。

## Windows 冷启动（复发点，必须修进资产）

Cursor 首次拉起 MCP 会跑 `server/run.mjs`；缺 `node_modules` 时自动 `npm ci`。三次复发的修复必须进 Dec vault，否则「修好 → push → 下次 pull 又挂」：

1. **`spawnSync("npm")` 在 Windows 上 ENOENT** — `run.mjs` 必须用 `npm.cmd` + `shell: true`。手工在目录里跑一次 `npm ci` 只能救命当次会话。
2. **密钥键名** — `dec exec` 注入的是 `TencentAPI_SecretId` / `TencentAPI_SecretKey`，`pickProcessEnv` 必须收录，否则误报缺密钥。
3. **项目根** — 子进程 cwd 常是 `server/`；`resolveProjectRoot` 要认 `DEC_PROJECT_ROOT` / `TENCENT_CLOUD_MCP_PROJECT_ROOT`，或从 `.dec/cache/...` 回推。

自检：删除 `server/node_modules` 后执行 `node run.mjs`，应能自动 `npm ci` 并阻塞在 MCP stdio。

## Dec 分发模式

MCP 源码嵌入 dec vault，**使用 `${workspaceFolder}` 相对路径**，`dec pull` 后任意项目可用，**不依赖 npm publish**：

```json
{
  "command": "mise",
  "args": [
    "exec",
    "node@20",
    "--",
    "node",
    "${workspaceFolder}/.dec/cache/tencent-cloud/skills/tencent-cloud/server/run.mjs"
  ]
}
```

- **入口必须是 `run.mjs`，不能直接指向 `dist/index.js`**：`run.mjs` 是冷启动引导，会先确认
  `node_modules` 就绪（缺失或 `package-lock.json` 变更时自动 `npm ci --omit=dev --ignore-scripts`）
  再拉起 `dist/index.js`。绕过它会在首次 `dec pull` 后报
  `ERR_MODULE_NOT_FOUND: Cannot find package '@modelcontextprotocol/sdk'`。
- **服务端路径**：`.dec/cache/tencent-cloud/skills/tencent-cloud/server/`（随 skill 一并 dec push/pull；含 `src/`、`dist/`、`package-lock.json`）
- **`dist/` 预构建入库**：`dec pull` 后可直接启动；运行时依赖由 `run.mjs` 自动补齐
- **项目根**：优先 `TENCENT_CLOUD_MCP_PROJECT_ROOT` / `DEC_PROJECT_ROOT`，否则从 `.dec/cache/...` 回推，最后才 `process.cwd()`
- **密钥**：`mise` conf.d 的 `TENCENTCLOUD_*`，或 `dec exec` 注入的 `TencentAPI_*`；不入 dec 资产正文

### 在新项目中启用

1. `dec config init` → 在 `enabled` 中启用 `tencent-cloud` → `dec pull`
2. 项目根 `mise.toml` 声明 `node = "20"`（或由全局 mise 提供）
3. 复制 `.config/mise/conf.d/tencent-cloud.toml.example` 为 `tencent-cloud.toml` 并填写密钥
4. 重启 Cursor MCP

运行时依赖由 `run.mjs` 在首次启动时自动 `npm ci`，无需手动安装。首次启动因此会多花
几十秒；装完会写 `node_modules/.tencent-cloud-mcp-ci` 标记，之后启动是秒开。若客户端
在首次启动时因超时报连接失败，重连一次即可。手动预热：

```bash
cd .dec/cache/tencent-cloud/skills/tencent-cloud/server && npm ci --omit=dev --ignore-scripts
```

### 开发与更新 MCP

- **独立开发仓库**：`tencent-cloud-mcp` 可作为开发源；改完后复制到 `.dec/cache/tencent-cloud/skills/tencent-cloud/server/` → `dec push`
- **修改 MCP 启动配置**：编辑 `.dec/cache/tencent-cloud/mcp/tencent-cloud.json` → `dec push` → 各项目 `dec pull`

## 注意

- 不要手动编辑 `.cursor/mcp.json` 中的 `dec-tencent-cloud`；改 `.dec/cache/tencent-cloud/` 后 `dec push` / `dec pull`
- CVM/Lighthouse 文件传输依赖**云助手 Agent**（TAT RunCommand + base64）
- 单一 MCP 常驻，无需 register 子服务


## 与官方 MCP 的关系

[TencentCloudCommunity/mcp-server](https://github.com/TencentCloudCommunity/mcp-server) 是腾讯云开发者社区的官方 MCP 集合，按产品拆成几十个独立进程（COS / SSL / TEO / DNSPod hosted 等）。

**不要再挂一个官方 COS/SSL MCP。** 密钥已经在 `dec-tencent-cloud` 里；官方 COS MCP（`cos-mcp`）偏上传下载和数据万象，没有自定义源站域名；官方 SSL MCP 缺 `ApplyCertificate` / `DeployCertificateInstance` 这两步免费证书上 COS 的关键 API；官方 TEO MCP 只有查询，没有 Makers 部署。

**Makers 也不用挂官方 `@edgeone/makers-mcp`。** 部署其实就是「取 COS 临时凭据 → 上传 → 建部署」三个调用，
COS 管线本 MCP 已经有了，所以 v0.3.0 把它做成了 `pages_deploy_dir`。官方那个 MCP 在没配 token 时会拉起
浏览器登录流程，在 Agent / 无头环境里正是我们要避开的东西。CI 里若只想一条命令，用官方 CLI
（`npx edgeone makers deploy -n <项目> -t <token> --json`）比挂 MCP 更合适。

本 skill 继续保持单一 MCP：需要的 COS 域名与 SSL 部署 API 直接加进 `dec-tencent-cloud`。Cursor 调用时工具名必须用下划线（`dns_describe_records`），不要用点号。
