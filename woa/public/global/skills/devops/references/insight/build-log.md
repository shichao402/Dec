# 构建日志下载与诊断

## 脚本调用

```bash
# 自动定位失败 task 并下载其日志（提取关键错误行）
python3 scripts/devops_build_log.py --url "{蓝盾URL}"

# 下载所有失败 task 的日志
python3 scripts/devops_build_log.py --url "{蓝盾URL}" --all-failed

# 指定 element 下载日志（tag = elementId，e-开头）
python3 scripts/devops_build_log.py --url "{蓝盾URL}" --tag e-xxxxx

# 获取熔断归档日志的下载 URL
python3 scripts/devops_build_log.py --url "{蓝盾URL}" --tag e-xxxxx --archive

# 限制输出行数（默认 500）
python3 scripts/devops_build_log.py --url "{蓝盾URL}" --lines 200

# 自定义 regex 搜索日志
python3 scripts/devops_build_log.py --url "{蓝盾URL}" --search "OutOfMemory|OOMKilled"

# 使用独立参数
python3 scripts/devops_build_log.py --project {projectId} --pipeline {pipelineId} --build {buildId}
```

## 参数说明

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --url | string | 二选一 | 蓝盾流水线 URL（自动解析 project/pipeline/build） |
| --project | string | 二选一 | 项目 ID（项目英文名） |
| --pipeline | string | 二选一 | 流水线 ID（p-开头） |
| --build | string | 二选一 | 构建 ID（b-开头） |
| --access-token | string | 否 | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |
| --tag | string | 否 | Element ID（e-开头），不指定则自动定位失败 task |
| --container | string | 否 | Container Hash ID（c-开头） |
| --job | string | 否 | Job ID |
| --archive | flag | 否 | 获取熔断归档日志 URL（而非直接下载） |
| --lines | int | 否 | 最大输出行数，默认 500 |
| --all-failed | flag | 否 | 下载所有失败 task 的日志 |
| --search | string | 否 | 自定义搜索 regex（替代内置错误关键词） |

## API 信息

### v4_user_log_download（下载日志）

- **接口地址**: `https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{projectId}/logs/download_logs`
- **请求方法**: GET
- **响应格式**: `application/octet-stream`（日志文本）
- **关键参数**: buildId（必填）、pipelineId、tag（elementId）、containerHashId、jobId、executeCount

### v4_user_artifactory_log_download（归档日志）

- **接口地址**: `https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{projectId}/artifactories/log`
- **请求方法**: GET
- **响应格式**: JSON `{ "data": { "url": "下载链接", "url2": "" }, "status": 0 }`
- **关键参数**: pipelineId、buildId、elementId、executeCount（均必填）
- **适用场景**: 日志被熔断截断后获取完整归档日志

## 返回字段

### 自动模式（无 --tag）

| 字段 | 说明 |
|------|------|
| totalFailures | 失败 task 总数 |
| queriedTasks | 已查询的 task 数 |
| results[].task | Task 名称 |
| results[].elementId | Element ID |
| results[].atomCode | 插件标识 |
| results[].errorMsg | 错误信息摘要 |
| results[].logSource | 日志来源（download/archive/unavailable） |
| results[].totalLines | 日志总行数 |
| results[].shownLines | 输出行数 |
| results[].logs | 关键日志行（含上下文） |

### 指定 --tag 模式

| 字段 | 说明 |
|------|------|
| elementId | Element ID |
| totalLines | 日志总行数 |
| shownLines | 输出行数 |
| searchPattern | 自定义搜索模式（如有） |
| logs | 关键日志行 |

## 日志智能截取策略

1. 日志 ≤ max_lines 且无自定义搜索：完整输出
2. 内置错误关键词匹配：error、FAILED、Exception、Traceback、npm ERR 等，提取匹配行及前后 3 行上下文
3. 自定义 `--search` regex：替代内置关键词进行匹配
4. 无匹配：输出最后 max_lines 行
5. 下载失败：自动降级到归档日志 URL
