# 按构建查询制品

命令：`python scripts/search_build_artifacts.py`

查询蓝盾流水线某次构建所产出的制品列表。

## 从构建链接提取参数

用户经常直接粘贴蓝盾构建链接，从中提取所需参数：

```
https://devops.woa.com/console/pipeline/{projectId}/{pipelineId}/detail/{buildId}/executeDetail
```

示例：
```
https://devops.woa.com/console/pipeline/myproject/p-abc123/detail/b-xyz789/executeDetail
→ projectId:  myproject
→ pipelineId: p-abc123
→ buildId:    b-xyz789
```

## 参数说明

### 必需参数

| 参数 | 说明 |
|------|------|
| `--project-id` | 项目 ID |
| `--pipeline-id` | 流水线 ID（`p-` 开头） |
| `--build-id` | 构建 ID（`b-` 开头） |

### 可选参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--page` | 页码，从 1 开始 | 1 |
| `--page-size` | 每页条数 | 20 |

## 示例

### 查询指定构建的所有制品

```bash
python scripts/search_build_artifacts.py \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --build-id b-xyz789
```

### 分页查询

```bash
python scripts/search_build_artifacts.py \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --build-id b-xyz789 \
  --page 2 \
  --page-size 50
```

## 输出格式

以 Markdown 格式输出，先展示汇总表格，再列出每个制品的详细信息：

```markdown
共 3 个制品，第 1/1 页（每页 20 条）

| # | 文件名 | 大小 | 类型 | MD5 |
|---|--------|------|------|-----|
| 1 | app-1.0.0.tgz | 10.00 MB | PIPELINE | 2947b3... |
| 2 | test-report.zip | 2.50 MB | PIPELINE | a1b2c3... |
| 3 | config-1.0.0.tar.gz | 512.00 KB | CUSTOM_DIR | d4e5f6... |

**详细信息：**

**1. app-1.0.0.tgz**
- fullPath: `/app-1.0.0.tgz`
- artifactoryType: `PIPELINE`
- size: 10.00 MB (10485760 bytes)
- md5: `2947b3932900d4534175d73964ec22ef`
- modifiedTime: 1706342400
- properties:
  - version: 1.0.0
```

## API 说明

- **接口**：`GET /v4/apigw-user/projects/{projectId}/artifactories/file_info`
- **认证**：`X-Bkapi-Authorization: {"access_token":"xxx","bk_username":"xxx"}`
- **必要参数**：`buildId`、`pipelineId`
