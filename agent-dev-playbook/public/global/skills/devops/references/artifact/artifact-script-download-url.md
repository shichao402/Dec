# 获取制品脚本端下载链接

命令：`python scripts/get_script_download_url.py`

获取供脚本或程序直接下载制品的 URL 列表（第三方下载链接）。

## 与浏览器下载链接的区别

| | 脚本端（本脚本） | 浏览器端 |
|---|---|---|
| 接口 | `thirdPartyDownloadUrl` | `user_download_url` |
| 适用场景 | wget/curl/程序下载 | 浏览器访问/分享 |
| 返回格式 | URL 数组（可能多个） | url + url2 两个字段 |
| 认证方式 | access_token 在 query 参数 | access_token 在 Header |

## 典型流程

先用 `search_build_artifacts.py` 查询构建制品，再用本脚本获取脚本下载链接：

```
步骤 1：查询构建制品，获取 artifactoryType 和 fullPath
python scripts/search_build_artifacts.py --project-id {projectId} --pipeline-id {pipelineId} --build-id {buildId}

步骤 2：获取脚本下载链接
python scripts/get_script_download_url.py --project-id {projectId} --artifactory-type {artifactoryType} --path {fullPath}
```

## 参数说明

| 参数 | 必填 | 说明 |
|------|------|------|
| `--project-id` | 是 | 项目 ID |
| `--artifactory-type` | 是 | 制品仓库类型：`PIPELINE` 或 `CUSTOM_DIR`，来自搜索结果的 `artifactoryType` 字段 |
| `--path` | 是 | 文件完整路径，来自搜索结果的 `fullPath` 字段 |

## 示例

### 获取流水线仓库制品的脚本下载链接

```bash
python scripts/get_script_download_url.py \
  --project-id myproject \
  --artifactory-type PIPELINE \
  --path /app-1.0.0.tgz
```

### 获取自定义仓库制品的脚本下载链接

```bash
python scripts/get_script_download_url.py \
  --project-id myproject \
  --artifactory-type CUSTOM_DIR \
  --path /releases/app-1.0.0.tgz
```

## 输出格式

```yaml
urls:
  - https://devops.woa.com/download/...
  - https://devops.woa.com/download/...   # 可能有多个备用地址
```

## 获取链接后自动下载

获取到下载链接后，如果用户希望直接下载文件，先探测用户的操作系统，再使用对应命令执行下载。使用 `urls` 列表中的第一个地址，文件名取制品的 `name` 字段。

**下载目录：**
- 用户指定了目录 → 使用指定路径
- 未指定 → 下载到当前工作目录

**Linux / macOS：**
```bash
# 优先 wget（-c 支持断点续传）
wget -c -O {文件名} "{urls[0]}"

# wget 不可用时改用 curl
curl -L -C - -o {文件名} "{urls[0]}"
```

**Windows（PowerShell）：**
```powershell
Invoke-WebRequest -Uri "{urls[0]}" -OutFile "{文件名}"
```

**Windows（CMD / 无 PowerShell）：**
```cmd
curl -L -o {文件名} "{urls[0]}"
```
> Windows 10 1803+ 内置 curl，可直接使用。

## API 说明

- **接口**：`GET /v2/apigw-user/artifactories/projects/{projectId}/thirdPartyDownloadUrl`
- **认证**：`access_token` 作为 query 参数传入（与其他接口不同，不在 Header 中）
- **参数**：`artifactoryType`、`path`
