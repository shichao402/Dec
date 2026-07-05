<!-- 本文件由 `dec pull` 从 .dec/cache/tencent-cloud/ 渲染生成，请勿直接编辑。
     修改流程：编辑 .dec/cache/tencent-cloud/... → dec push → dec pull 验证 -->

# @shichao402/tencent-cloud-mcp

单一高权限腾讯云 MCP：CVM、COS、DNSPod、Lighthouse、CDB MySQL 通过 SDK 直连。**敏感销毁/全开放安全组等操作已禁用。**

## 架构

- **单进程**：Cursor 只挂载 `dec-tencent-cloud` 一个 MCP
- **SDK 直连**：`tencentcloud-sdk-nodejs` / `cos-nodejs-sdk-v5` / `mysql2`（可选 SQL）
- **密钥链路**：`pkv → .config/mise/conf.d/tencent-cloud.toml → mise exec → MCP`

## Namespaces（v0.2.0，共 43 tools）

| Namespace | Tools |
|-----------|-------|
| **meta** (4) | `list_namespaces`, `list_blocked_operations`, `sync_pkv_config`, `sync_dec_assets` |
| **cvm** (6) | `describe_instances`, `describe_security_groups`, `run_command`, `upload_file`, `download_file`, `describe_invocation` |
| **lighthouse** (6) | `describe_instances`, `describe_firewall_rules`, `run_command`, `upload_file`, `download_file`, `describe_invocation` |
| **cos** (10) | `list_objects`, `upload_object`, `download_object`, `head_object`, `delete_object`†, `get_bucket_info`, `get_bucket_cors`, `put_bucket_cors`, `get_bucket_policy`, `put_bucket_policy` |
| **dns** (5) | `describe_domains`, `describe_records`, `create_record`, `modify_record`, `delete_record` |
| **cdb** (12) | `describe_instances`, `describe_wan_service`, `open_wan_service`, `close_wan_service`†, `describe_databases`, `describe_accounts`, `describe_tables`, `create_database`, `describe_security_groups`, `add_security_group_ingress`†, `query_sql`‡, `execute_sql`‡ |

† `cos.delete_object`、`cdb.close_wan_service`、`cdb.add_security_group_ingress` 需 `confirm=true`  
‡ SQL 需在 `.config/mise/conf.d/tencent-cloud.toml` 配置 `CDB_HOST` / `CDB_PORT` / `CDB_USER` / `CDB_PASSWORD`（`CDB_PASSWORD` 为 MySQL 账号密码，非 API 密钥）

## 敏感操作（已禁用）

运行 `meta.list_blocked_operations` 查看完整列表。主要包括：

- **CVM/Lighthouse**：销毁实例、重置密码、对 `0.0.0.0/0` 开放危险端口的安全组/防火墙规则
- **COS**：删除存储桶、批量删除
- **CDB**：DROP/TRUNCATE/无 WHERE 的 DELETE（SQL 层拦截）
- **DNS**：批量删除（单条删除需 `domain` + `recordId`）

## `.config/mise/conf.d/tencent-cloud.toml` 配置

```toml
[env]
TENCENTCLOUD_SECRET_ID="..."
TENCENTCLOUD_SECRET_KEY="..."
TENCENTCLOUD_REGION="ap-chengdu"
COS_BUCKET="your-bucket-1250000000"
COS_REGION="ap-chengdu"
# CDB 管控 API（describe_instances / describe_databases 等）
CDB_REGION="ap-chengdu"
CDB_INSTANCE_ID="cdb-xxxxxxxx"
# CDB SQL 直连（query_sql / execute_sql 必填 CDB_PASSWORD）
# 外网已开启 → WanDomain；仅内网 → Vip（本机需 VPN 或同 VPC 跳板）
CDB_HOST="10.0.0.9"
CDB_PORT="3306"
CDB_USER="root"
CDB_PASSWORD="..."
# CDB_DATABASE="mydb"  # 可选
```

## 文件传输（CVM / Lighthouse）

通过 **TAT RunCommand + base64** 实现，要求实例已安装并运行**云助手 Agent**。

1. `*.upload_file` — 写入实例内路径  
2. `*.download_file` — 触发下载任务，配合 `*.describe_invocation` 查看 base64 输出

## 本地开发

```bash
npm install
npm run build
npm test
RUN_LIVE_TESTS=1 npm test   # 需 AgentsHelpMe/.config/mise/conf.d/tencent-cloud.toml
```

## 版本更新（Dec 嵌入模式为主）

1. 在 `.dec/cache/tencent-cloud/skills/tencent-cloud/server/` 修改源码 → `npm run build`
2. bump `package.json` version（若需要）
3. `dec push` → 各项目 `dec pull` → `cd .dec/cache/tencent-cloud/skills/tencent-cloud/server && npm ci`
4. 重启 Cursor MCP

独立 npm 发布（`npm publish`）为可选；Dec 分发不依赖 npm registry。
