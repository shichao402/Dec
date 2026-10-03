# 构建历史查询

## 脚本调用

```bash
# 查最近 10 次构建
python3 scripts/devops_build_history.py --url "{蓝盾URL}"

# 指定条数
python3 scripts/devops_build_history.py --url "{蓝盾URL}" --count 20

# 只看失败记录
python3 scripts/devops_build_history.py --url "{蓝盾URL}" --status FAILED

# 输出原始 JSON
python3 scripts/devops_build_history.py --url "{蓝盾URL}" --json

# 使用独立参数
python3 scripts/devops_build_history.py --project {projectId} --pipeline {pipelineId}
```

## 参数说明

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --url | string | 二选一 | 蓝盾流水线 URL（自动解析 project/pipeline） |
| --project | string | 二选一 | 项目 ID（项目英文名） |
| --pipeline | string | 二选一 | 流水线 ID（p-开头） |
| --access-token | string | 否 | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |
| --count | int | 否 | 查询条数，默认 10 |
| --status | string | 否 | 按状态过滤（SUCCEED/FAILED/CANCELED/RUNNING） |
| --json | flag | 否 | 输出原始 API JSON |

## API 信息

- **接口名称**: `v4_user_build_list`
- **接口地址**: `https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{projectId}/build_histories?pipelineId={pipelineId}&page=1&pageSize=10`
- **请求方法**: GET
- **认证方式**: Header 中携带 `X-Bkapi-Authorization: {"access_token":"xxx"}`

## 返回字段

| 字段 | 说明 |
|------|------|
| summary.total | 查询到的构建总数 |
| summary.succeed | 成功次数 |
| summary.failed | 失败次数 |
| summary.canceled | 取消次数 |
| summary.successRate | 成功率 |
| summary.consecutiveFailures | 连续失败次数 |
| summary.verdict | 判定（偶发失败/持续失败/全部成功） |
| builds[] | 构建记录列表 |
| builds[].buildNum | 构建序号 |
| builds[].status | 状态 |
| builds[].startTime | 开始时间 |
| builds[].duration | 耗时 |
| builds[].trigger | 触发方式 |
| builds[].buildId | 构建 ID |
| builds[].errors | 错误摘要（最多 3 条） |
