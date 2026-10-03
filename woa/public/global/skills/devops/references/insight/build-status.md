# 构建状态查询与递归诊断

## 诊断路由

用户只贴构建 URL 时先查状态；失败时再递归、拉错误日志并结合历史分析。

```
获取构建状态
├─ FAILED → status --recursive + 错误日志 + 历史 → 诊断报告
├─ RUNNING → status --check-stuck + 对比历史耗时
├─ CANCELED → 历史 + 基础状态
└─ SUCCEED → 耗时分析（用户觉得慢时再 diff）
```

归因约束：

- `SUCCEED` 且用户在问发布/提交结果：核对制品或外部 ID，不要把绿灯当成外部已更新（假绿）。
- `FAILED` 且步骤含发布/提交：先确认外部是否已有副作用，不要把红灯当成「什么都没发出去」（假红）。
- 平台 `800006` / 「用户配置错误」不是根因；继续读步骤日志。
- 不要只根据日志最后一行归因，先找时间线上的首次异常。细则见 [ci-runtime-pitfalls.md](../generate/ci-runtime-pitfalls.md) 第 5 节。
- 日志过大时末尾含 `【Please download logs to view.】`，`download_log.py` 会自动拉完整日志 ZIP（需 `--tag`）；`--no-auto-full` 可禁用。见 [api-reference.md](../log/api-reference.md)。
- 报告结构见 [report-template.md](report-template.md)。

## 脚本调用

```bash
# 基础查询
python3 scripts/devops_build_status.py --url "{蓝盾URL}"

# 自动递归分析失败的子流水线（推荐）
python3 scripts/devops_build_status.py --url "{蓝盾URL}" --recursive

# 指定递归深度（默认 3）
python3 scripts/devops_build_status.py --url "{蓝盾URL}" --recursive --depth 5

# 检测构建是否卡住（对比历史耗时）
python3 scripts/devops_build_status.py --url "{蓝盾URL}" --check-stuck

# 输出原始 API JSON
python3 scripts/devops_build_status.py --url "{蓝盾URL}" --json

# 使用独立参数（替代 --url）
python3 scripts/devops_build_status.py --project {projectId} --pipeline {pipelineId} --build {buildId}
```

## 参数说明

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --url | string | 二选一 | 蓝盾流水线 URL（自动解析 project/pipeline/build） |
| --project | string | 二选一 | 项目 ID（项目英文名） |
| --pipeline | string | 二选一 | 流水线 ID（p-开头） |
| --build | string | 二选一 | 构建 ID（b-开头） |
| --access-token | string | 否 | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |
| --recursive | flag | 否 | 自动递归分析失败的子流水线 |
| --depth | int | 否 | 递归深度，默认 3 |
| --check-stuck | flag | 否 | 检测构建是否卡住（对比历史耗时） |
| --json | flag | 否 | 输出原始 API JSON |

## API 信息

- **接口名称**: `v4_user_build_status`
- **接口地址**: `https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{projectId}/build_status?pipelineId={pipelineId}&buildId={buildId}`
- **请求方法**: GET
- **认证方式**: Header 中携带 `X-Bkapi-Authorization: {"access_token":"xxx"}`

## 返回字段

| 字段 | 说明 |
|------|------|
| buildNum | 构建序号 |
| pipelineName | 流水线名称 |
| status | 构建状态（SUCCEED/FAILED/RUNNING/CANCELED/QUEUE） |
| trigger | 触发方式 |
| startTime / endTime | 开始/结束时间 |
| duration | 耗时（格式化） |
| stages | 各 Stage 耗时排序（name/status/elapsed_ms） |
| failures | 失败的 Task 列表（stage/task/errorCode/errorMsg） |
| subPipelines | 子流水线列表（name/pipelineId/buildId/url/outputs） |
| triggerChain | 触发链（父流水线 → 当前） |
| buildParams | 用户构建参数（过滤 BK_CI_ 前缀） |
| failedSubPipelineDiagnosis | （递归模式）失败子流水线的完整诊断 |

### --check-stuck 返回字段

| 字段 | 说明 |
|------|------|
| stuck | 是否疑似卡住（bool） |
| elapsedMinutes | 当前已运行分钟数 |
| historyAvgMinutes | 历史平均耗时（分钟） |
| ratio | 当前/历史比值（≥2.0 判定为卡住） |
| currentStage | 当前正在运行的 Stage |
| verdict | 判定结论 |
