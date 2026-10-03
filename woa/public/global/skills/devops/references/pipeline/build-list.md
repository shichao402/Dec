# 获取流水线构建历史

命令：`python scripts/pipeline_client.py list`

查询流水线的历史构建记录，支持分页和多种筛选条件。

## 参数

### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |
| --pipeline-id | 流水线 ID（p-开头） |

### 可选参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| --page | number | 1 | 页码 |
| --page-size | number | 20 | 每页条数（最大100） |
| --status | string | - | 按状态筛选（SUCCEED/FAILED/CANCELED/RUNNING/QUEUE/STAGE_SUCCESS） |
| --trigger | string | - | 按触发方式筛选（MANUAL/TIME_TRIGGER/WEB_HOOK/PIPELINE/REMOTE） |
| --start-user | string | - | 按启动人筛选 |
| --build-no-start | number | - | 构建号范围起始 |
| --build-no-end | number | - | 构建号范围结束 |
| --start-time-start | number | - | 开始时间范围起始（13位时间戳） |
| --start-time-end | number | - | 开始时间范围结束（13位时间戳） |
| --end-time-start | number | - | 结束时间范围起始（13位时间戳） |
| --end-time-end | number | - | 结束时间范围结束（13位时间戳） |
| --archive-flag | boolean | false | 是否查询归档数据 |
| --build-msg | string | - | 按构建信息筛选 |
| --remark | string | - | 按备注筛选 |
| --material-alias | string | - | 按代码库别名筛选 |
| --material-branch | string | - | 按代码库分支筛选 |
| --material-commit-id | string | - | 按提交ID筛选 |
| --update-time-desc | boolean | - | 是否按更新时间降序 |
| --access-token | string | - | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

## 示例

### 查询最近10次构建

```bash
python scripts/pipeline_client.py list \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --page 1 \
  --page-size 10
```

### 查询失败的构建

```bash
python scripts/pipeline_client.py list \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --status FAILED
```

### 查询某人触发的构建

```bash
python scripts/pipeline_client.py list \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --start-user zhangsan
```

### 查询时间范围内的构建

```bash
# 2024-01-01 00:00:00 到 2024-01-31 23:59:59
python scripts/pipeline_client.py list \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --start-time-start 1704038400000 \
  --start-time-end 1706716799000
```

### 查询指定代码库分支的构建

```bash
python scripts/pipeline_client.py list \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --material-branch master
```

### 组合筛选

```bash
python scripts/pipeline_client.py list \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --status SUCCEED \
  --trigger MANUAL \
  --page-size 5
```

## 返回说明

```json
{
  "data": {
    "count": 100,           // 总记录数
    "page": 1,              // 当前页码
    "pageSize": 20,         // 每页条数
    "totalPages": 5,        // 总页数
    "pipelineVersion": 45,  // 流水线版本
    "hasDownloadPermission": true,
    "records": [
      {
        "id": "b-xxx",           // 构建 ID
        "buildNum": 123,         // 构建号
        "buildNumAlias": "",     // 构建号别名
        "status": "SUCCEED",     // 状态
        "startTime": 1234567890, // 开始时间
        "endTime": 1234567900,   // 结束时间
        "totalTime": 10,         // 总耗时（秒）
        "userId": "zhangsan",    // 启动人
        "trigger": "MANUAL",     // 触发方式
        "startType": "MANUAL",
        "buildMsg": "",          // 构建信息
        "remark": "",            // 备注
        "stageStatus": [...],    // 各阶段状态
        "material": [...],       // 代码库信息
        "artifactList": [...],   // 构建产物
        "buildParameters": [...] // 构建参数
      }
    ]
  },
  "status": 0,
  "message": ""
}
```
