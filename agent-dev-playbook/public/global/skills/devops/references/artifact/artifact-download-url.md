# 获取制品浏览器端下载链接

命令：`python scripts/get_download_url.py`

获取可在浏览器中直接访问的制品下载 URL。

## 典型流程

先用 `search_build_artifacts.py` 查询构建制品，再用本脚本获取其下载链接：

```
步骤 1：查询构建制品，获取 artifactoryType 和 fullPath
python scripts/search_build_artifacts.py --project-id {projectId} --pipeline-id {pipelineId} --build-id {buildId}

步骤 2：根据制品信息获取下载链接
python scripts/get_download_url.py --project-id {projectId} --artifactory-type {artifactoryType} --path {fullPath}
```

## 参数说明

| 参数 | 必填 | 说明 |
|------|------|------|
| `--project-id` | 是 | 项目 ID |
| `--artifactory-type` | 是 | 制品仓库类型：`PIPELINE`（流水线仓库）或 `CUSTOM_DIR`（自定义仓库），来自搜索结果的 `artifactoryType` 字段 |
| `--path` | 是 | 文件完整路径，来自搜索结果的 `fullPath` 字段 |

## 示例

### 获取流水线仓库制品的下载链接

```bash
python scripts/get_download_url.py \
  --project-id myproject \
  --artifactory-type PIPELINE \
  --path /app-1.0.0.tgz
```

### 获取自定义仓库制品的下载链接

```bash
python scripts/get_download_url.py \
  --project-id myproject \
  --artifactory-type CUSTOM_DIR \
  --path /releases/app-1.0.0.tgz
```

## 输出格式

```yaml
url: https://devops.woa.com/download/...    # 主下载链接
url2: https://devops.woa.com/download/...   # 备用下载链接（可能为空）
```

## API 说明

- **接口**：`GET /v4/apigw-user/projects/{projectId}/artifactories/user_download_url`
- **认证**：`X-Bkapi-Authorization: {"access_token":"xxx","bk_username":"xxx"}`
- **参数**：`artifactoryType`、`path`
