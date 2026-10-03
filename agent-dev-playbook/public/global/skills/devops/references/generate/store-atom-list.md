# 查询市场插件列表

查询市场中可用的流水线插件。

**API 端点**：`GET /atoms/atom_list`

## 命令

```bash
# 查询项目可见插件
python scripts/pipeline_generate_client.py list-atoms \
  --project-id {projectId}

# 按关键词过滤
python scripts/pipeline_generate_client.py list-atoms \
  --project-id {projectId} \
  --keyword "归档"

# 查询无编译环境插件
python scripts/pipeline_generate_client.py list-atoms \
  --project-id {projectId} \
  --job-type AGENT_LESS
```

## 参数说明

| 参数 | 必须 | 说明 |
|------|------|------|
| `--project-id` | ✅ | 项目编码（用于过滤项目可见插件） |
| `--category` | | 插件类别：`TRIGGER`（触发器）/ `TASK`（任务，默认） |
| `--keyword` | | 搜索关键字（仅在结果过多时使用） |
| `--os` | | 操作系统：`ALL` / `WINDOWS` / `LINUX` / `MACOS` |
| `--job-type` | | Job 类型：`AGENT`（编译环境）/ `AGENT_LESS`（无编译环境） |
| `--service-scope` | | 服务范围（默认 `PIPELINE`，可传 `QUALITY` 等） |
| `--page` | | 页码 |
| `--page-size` | | 每页数量（最大 100） |

## 默认参数（脚本已内置）

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `serviceScope` | `PIPELINE` | 查询流水线可用插件 |
| `category` | `TASK` | 只查任务类插件 |
| `queryProjectAtomFlag` | `true` | 包含项目插件 |
| `pageSize` | `100` | 每页最多 100 条 |

## 返回字段说明

| 字段 | 说明 |
|------|------|
| `name` | 插件名称 |
| `atomCode` | 插件唯一标识码（用于 `atom-yaml` / `atom-detail` 和编排） |
| `version` | 当前版本 |
| `defaultVersion` | 默认版本（如 `2.*`） |
| `classType` | 插件类型：`marketBuild`（有编译环境）/ `marketBuildLess`（无编译环境） |
| `os` | 支持的操作系统列表 |
| `summary` | 插件简介 |
| `installed` | 是否已安装 |
