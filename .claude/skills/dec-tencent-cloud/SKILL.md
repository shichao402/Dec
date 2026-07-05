<!-- 本文件由 `dec pull` 从 .dec/cache/tencent-cloud/ 渲染生成，请勿直接编辑。
     修改流程：编辑 .dec/cache/tencent-cloud/... → dec push → dec pull 验证 -->

---
name: tencent-cloud
description: >
  通过 dec-tencent-cloud 单一 MCP 操作腾讯云 CVM/COS/DNSPod/Lighthouse/CDB。
  密钥由 .config/mise/conf.d/tencent-cloud.toml 注入，敏感销毁/全开放安全组等操作已禁用。
---

# 腾讯云 MCP 运维（v0.2.0）

## 何时使用

1. **首次配置密钥**
   - 运行 `meta.sync_pkv_config`（或手动维护 `.config/mise/conf.d/tencent-cloud.toml`）
   - 密钥不入库，仅写入 gitignore 的 `tencent-cloud.toml`

2. **查看可用能力**
   - `meta.list_namespaces` — 列出 cvm / cos / dns / lighthouse / cdb
   - `meta.list_blocked_operations` — 查看已禁用的敏感操作

3. **常用操作**

   | 场景 | Tools |
   |------|-------|
   | 查 CVM / Lighthouse | `cvm.describe_instances`, `lighthouse.describe_instances` |
   | 实例执行命令 / 传文件 | `cvm.run_command`, `cvm.upload_file`, `cvm.download_file`（TAT 云助手） |
   | COS 对象与桶配置 | `cos.list_objects`, `cos.upload_object`, `cos.download_object`, `cos.get_bucket_cors` |
   | DNS 解析 | `dns.describe_records`, `dns.create_record`, `dns.modify_record`, `dns.delete_record` |
   | CDB 管控 | `cdb.describe_instances`, `cdb.describe_wan_service`, `cdb.open_wan_service`, `cdb.describe_databases`, `cdb.create_database` |
   | CDB 外网 | `cdb.open_wan_service` 开通外网；`cdb.close_wan_service` 需 `confirm=true` |
   | CDB 安全组 | `cdb.describe_security_groups` 查规则；`cdb.add_security_group_ingress` 添加入站（需 `confirm=true`，外网端口默认同时放通 TCP 3306） |
   | CDB SQL | `cdb.query_sql`（只读）, `cdb.execute_sql`（有限 DML） |

4. **同步 Dec 资产**
   - `meta.sync_dec_assets` 等价于项目根 `dec pull`

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

1. **`cdb.open_wan_service`** — 开通外网（若已开通可跳过）
2. **`cdb.describe_wan_service`** — 读取 `WanDomain` 与 **`WanPort`**（外网端口常为 `22915` 等，**不一定等于 3306**）
3. **`cdb.describe_security_groups`** — 确认实例关联的安全组
4. **`cdb.add_security_group_ingress`** — 放通本机公网 IP（`cidr=你的IP/32`）：
   - `port` = 上一步的 **WanPort**
   - 默认 `includeInternalPort=true` 会同时放通 TCP **3306**（外网代理需要）
   - 必须 **`confirm=true`**
5. **更新 `tencent-cloud.toml`**：
   - `CDB_HOST` = `WanDomain`
   - `CDB_PORT` = `WanPort`（勿写死 3306）
   - `CDB_USER` / `CDB_PASSWORD` = 数据库账号密码（非 API 密钥）
6. **`cdb.query_sql`** — `SELECT 1` 验证连通

仅内网场景：用 `describe_instances` 的 `Vip` 作 `CDB_HOST`，本机需 VPN 或同 VPC 跳板，无需外网与安全组步骤。

## 部署应用 Playbook（CVM / Lighthouse）

**前置**：目标实例已安装并运行**云助手 Agent**（TAT）；实例**安全组/防火墙**须在控制台放通所需端口（MCP 不自动改 CVM 安全组）。

1. **`cvm.describe_instances`**（或 `lighthouse.describe_instances`）— 确认实例 ID 与状态
2. **`cvm.upload_file`** — 上传应用包/脚本到实例路径（base64 + TAT）
3. **`cvm.run_command`** — 解压、安装依赖、启动服务（可配合 `describe_invocation` 查看输出）
4. 需要回传日志时：**`cvm.download_file`** + **`cvm.describe_invocation`**

Lighthouse 将 `cvm.*` 换为 `lighthouse.*` 即可。

## 敏感操作（已禁用）

- **CVM/Lighthouse**：TerminateInstances、ResetInstancesPassword、对 0.0.0.0/0 开放危险端口
- **COS**：deleteBucket、批量删除；`cos.delete_object` 需 `confirm=true`
- **CDB SQL**：DROP/TRUNCATE/无 WHERE 的 DELETE
- **DNS**：批量删除；单条删除需 `domain` + `recordId`

## 密钥链路

```text
pkv get tencent-cloud env  →  .config/mise/conf.d/tencent-cloud.toml [env]  →  mise exec  →  dec-tencent-cloud
```

fallback：`~/.pkv/env/HelloKnightTasker.json`；旧项目可暂留 `mise.local.toml`（MCP 读取时 conf.d 优先）。

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
    "${workspaceFolder}/.dec/cache/tencent-cloud/skills/tencent-cloud/server/dist/index.js"
  ]
}
```

- **服务端路径**：`.dec/cache/tencent-cloud/skills/tencent-cloud/server/`（随 skill 一并 dec push/pull；含 `src/`、`dist/`、`package-lock.json`）
- **`dist/` 预构建入库**：`dec pull` 后可直接启动；运行时仍依赖 `node_modules`（见下方首次安装）
- **项目根**：默认 `process.cwd()`（Cursor 以 workspace 为 cwd 启动 MCP）
- **密钥**：由 `mise exec` 加载 `.config/mise/conf.d/tencent-cloud.toml` 中的 `TENCENTCLOUD_*` / `COS_*` / `CDB_*` 等，不入 dec 资产

### 在新项目中启用

1. `dec config init` → 在 `enabled` 中启用 `tencent-cloud` → `dec pull`
2. 项目根 `mise.toml` 声明 `node = "20"`（或由全局 mise 提供）
3. **首次或依赖变更后**安装运行时依赖：
   ```bash
   cd .dec/cache/tencent-cloud/skills/tencent-cloud/server && npm ci
   ```
4. 复制 `.config/mise/conf.d/tencent-cloud.toml.example` 为 `tencent-cloud.toml` 并填写密钥
5. 重启 Cursor MCP

### 开发与更新 MCP

- **独立开发仓库**：`tencent-cloud-mcp` 可作为开发源；改完后复制到 `.dec/cache/tencent-cloud/skills/tencent-cloud/server/` → `dec push`
- **修改 MCP 启动配置**：编辑 `.dec/cache/tencent-cloud/mcp/tencent-cloud.json` → `dec push` → 各项目 `dec pull`

## 注意

- 不要手动编辑 `.cursor/mcp.json` 中的 `dec-tencent-cloud`；改 `.dec/cache/tencent-cloud/` 后 `dec push` / `dec pull`
- CVM/Lighthouse 文件传输依赖**云助手 Agent**（TAT RunCommand + base64）
- 单一 MCP 常驻，无需 register 子服务
