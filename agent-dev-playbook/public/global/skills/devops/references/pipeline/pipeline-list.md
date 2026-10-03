# 获取流水线列表

查询项目下的流水线列表，返回数据已清洗保留关键字段。

## 用法

```bash
python scripts/pipeline_client.py pipelines --project-id <projectId> [options]
```

## 参数说明

### 必需参数

| 参数 | 说明 |
|------|------|
| `--project-id` | 项目英文名 |

### 可选参数

| 参数 | 说明 | 默认值 | 示例 |
|------|------|--------|------|
| `--page` | 页码 | 0 | `--page 0` |
| `--page-size` | 每页条数 | 20 | `--page-size 50` |
| `--filter-by-pipeline-name` | 按流水线名称筛选 | - | `--filter-by-pipeline-name mypipe` |
| `--filter-by-creator` | 按创建人筛选 | - | `--filter-by-creator tom` |
| `--filter-by-labels` | 按标签筛选 | - | `--filter-by-labels label1,label2` |
| `--filter-by-view-ids` | 按流水线组ID筛选 | - | `--filter-by-view-ids group1,group2` |
| `--view-id` | 流水线组ID | allPipeline | `--view-id group-abc` |
| `--view-name` | 流水线组名称 | - | `--view-name 我的流水线组` |
| `--sort-type` | 排序类型 | LAST_EXEC_TIME | `--sort-type UPDATE_TIME` |
| `--collation` | 排序方式（ASC/DESC） | DESC | `--collation ASC` |
| `--is-project` | 是否为项目流水线 | - | `--is-project true` |
| `--show-delete` | 是否显示已删除 | - | `--show-delete false` |

## sort-type 枚举值

- `CREATE_TIME`：按创建时间排序
- `UPDATE_TIME`：按更新时间排序
- `PIPELINE_NAME`：按流水线名称排序
- `LAST_EXEC_TIME`：按最后执行时间排序

## 返回数据结构（已清洗）

```json
{
  "status": 0,
  "message": "",
  "data": {
    "count": 100,
    "page": 0,
    "pageSize": 20,
    "totalPages": 5,
    "records": [
      {
        "pipelineId": "p-abc123",
        "pipelineName": "my-pipeline",
        "pipelineDesc": "流水线描述",
        "creator": "tom",
        "createTime": 1700000000000,
        "updater": "tom",
        "updateTime": 1700100000000,
        "latestBuildStatus": "SUCCEED",
        "latestBuildStartTime": 1700100000000,
        "latestBuildEndTime": 1700100100000,
        "latestBuildNum": 42,
        "buildCount": 100,
        "runningBuildCount": 0,
        "canManualStartup": true,
        "hasPermission": true,
        "hasCollect": false,
        "delete": false,
        "version": 10,
        "pipelineVersion": 10,
        "viewNames": ["默认流水线组"]
      }
    ]
  },
  "_cleaned": true
}
```

## 常用示例

### 获取项目所有流水线

```bash
python scripts/pipeline_client.py pipelines --project-id myproject
```

### 获取项目下我创建的流水线

```bash
# 固定 view-id 为 myPipeline
python scripts/pipeline_client.py pipelines --project-id myproject --view-id myPipeline
```

### 获取项目下我收藏的流水线

```bash
# 固定 view-id 为 collect
python scripts/pipeline_client.py pipelines --project-id myproject --view-id collect
```


### 获取项目下指定分组的流水线

```bash
python scripts/pipeline_client.py pipelines --project-id myproject --view-name 流水线组
```


### 搜索指定名称的流水线

```bash
python scripts/pipeline_client.py pipelines \
  --project-id myproject \
  --filter-by-pipeline-name "deploy"
```

### 获取最近更新的10条流水线

```bash
python scripts/pipeline_client.py pipelines \
  --project-id myproject \
  --sort-type UPDATE_TIME \
  --collation DESC \
  --page-size 10
```
