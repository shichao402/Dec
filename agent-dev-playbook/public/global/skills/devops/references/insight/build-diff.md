# 构建对比

对比两次构建的参数、耗时、错误差异，用于定位"为什么这次失败/变慢了"。

## 脚本调用

```bash
# 自动找最近成功构建对比（推荐）
python3 scripts/devops_build_diff.py --url "{蓝盾URL}" --auto-baseline

# 指定两次构建对比
python3 scripts/devops_build_diff.py --url "{蓝盾URL}" --good b-xxx --bad b-yyy

# 使用独立参数
python3 scripts/devops_build_diff.py --project {projectId} --pipeline {pipelineId} --good b-xxx --bad b-yyy

# 输出原始 JSON
python3 scripts/devops_build_diff.py --url "{蓝盾URL}" --auto-baseline --json
```

## 参数说明

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --url | string | 二选一 | 蓝盾流水线 URL（自动解析 project/pipeline，URL 中的 buildId 作为 bad） |
| --project | string | 二选一 | 项目 ID（项目英文名） |
| --pipeline | string | 二选一 | 流水线 ID（p-开头） |
| --good | string | 否 | 基准构建 buildId（成功的），不指定时自动查找 |
| --bad | string | 否 | 问题构建 buildId（失败/慢的），可从 URL 自动提取 |
| --auto-baseline | flag | 否 | 自动使用最近一次成功构建作为 good baseline |
| --access-token | string | 否 | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |
| --json | flag | 否 | 输出原始对比 JSON |

## API 信息

调用两个已有接口：
- `v4_user_build_status`：分别获取 good/bad 构建的详细信息
- `v4_user_build_list`（--auto-baseline 时）：查找最近成功构建

## 返回字段

| 字段 | 说明 |
|------|------|
| good | 基准构建摘要（buildNum/status/duration/trigger） |
| bad | 问题构建摘要（buildNum/status/duration/trigger） |
| durationDiff | 总耗时差异 |
| paramDiffs[] | 参数差异列表（param/good/bad） |
| paramDiffSummary | 参数差异摘要 |
| stageDiffs[] | Stage 耗时差异（stage/good_ms/bad_ms/delta_ms/ratio） |
| newErrors[] | 问题构建的错误列表（task/code/msg） |
