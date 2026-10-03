# 蓝盾 API 调用指南

## 鉴权方式

优先走统一 Skill 的 `scripts/auth.py`（`config.json` / `--access-token` / 环境变量）。下面的 MCP 备选仍可用环境变量 `BK_CI_ACCESS_TOKEN`。

| 变量 | 说明 | 获取方式 |
|------|------|----------|
| `BK_CI_ACCESS_TOKEN` | 蓝盾 access_token | https://devops.woa.com/ms/auth/api/user/bkToken/get |

Token 读取优先级：系统环境变量 > `.env` 文件（脚本目录 / skill 根目录 / cwd）。

### .env 文件格式

```
BK_CI_ACCESS_TOKEN=your_token_here
```

## 基础请求格式

```bash
curl -s "https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{projectId}/pipelines/{pipelineId}/builds/{buildId}/status" \
  -H "Content-Type: application/json" \
  -H "X-Bkapi-Authorization: {\"access_token\": \"${BK_CI_ACCESS_TOKEN}\"}"
```

## API 网关地址

- 生产环境：`https://devops.apigw.o.woa.com/prod/v4/apigw-user`
- 需在内网环境访问

## 日志 API

### v4_user_log_download（下载日志）

```
GET /projects/{projectId}/logs/download_logs
```

| 参数 | 类型 | 必须 | 说明 |
|------|------|------|------|
| buildId | String | Y | 构建 ID（b-开头） |
| pipelineId | String | N | 流水线 ID（p-开头） |
| tag | String | N | Element ID（e-开头），对应 task 的 elementId |
| containerHashId | String | N | Container Hash ID（c-开头） |
| jobId | String | N | Job ID |
| executeCount | integer | N | 执行次数（默认 1） |
| archiveFlag | boolean | N | 是否查询归档数据 |

响应：`application/octet-stream`（日志文本），请求时 Accept 头需设为 `application/octet-stream`。

### v4_user_artifactory_log_download（归档日志）

```
GET /projects/{projectId}/artifactories/log
```

| 参数 | 类型 | 必须 | 说明 |
|------|------|------|------|
| pipelineId | String | Y | 流水线 ID |
| buildId | String | Y | 构建 ID |
| elementId | String | Y | 插件 elementId |
| executeCount | String | Y | 执行序号 |

响应：JSON `{ "data": { "url": "下载链接", "url2": "" }, "status": 0 }`

用于日志被熔断截断后获取完整归档日志的下载 URL。

## Exit Code 约定

脚本统一使用语义化 exit code：

| Code | 含义 | Agent 建议动作 |
|------|------|----------------|
| 0 | 成功 | 继续分析输出 |
| 1 | Token 缺失 | 引导用户获取 token |
| 2 | 网络不可达 | 检查内网环境 |
| 3 | API 业务错误 | 检查参数是否正确 |
| 4 | 资源不存在 | 确认 URL/ID 是否有效 |
| 5 | 参数错误 | 检查 --url 或 --project/--pipeline/--build |

## MCP 备选方案

脚本不可用时，通过 mcporter 接入蓝盾 MCP 服务：

```json
{
  "devops-pipeline": {
    "url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/devops-prod-pipeline-streamable/mcp/",
    "headers": {
      "X-Bkapi-Authorization": "{\"access_token\": \"${BK_CI_ACCESS_TOKEN}\"}"
    }
  }
}
```

### MCP 工具列表

| 工具 | 功能 |
|------|------|
| `v4_user_build_status` | 查看构建状态 |
| `v4_user_build_start` | 启动构建 |
| `v4_user_build_list` | 获取构建历史 |
| `v4_user_build_startInfo` | 获取启动参数 |
