# 获取制品 APP 跳转链接二维码

命令：`python scripts/get_app_download_url.py`

调用 APP 跳转链接接口，并将返回的跳转链接生成本地 PNG 二维码文件。仅支持手机端 App 安装包制品（`.apk`、`.apks`、`.ipa`、`.hap`）。

## 典型流程

先用 `search_build_artifacts.py` 查询构建制品，再用本脚本生成 APP 跳转链接二维码：

```
步骤 1：查询构建制品，获取 artifactoryType 和 fullPath
python scripts/search_build_artifacts.py --project-id {projectId} --pipeline-id {pipelineId} --build-id {buildId}

步骤 2：根据制品信息生成 APP 跳转链接二维码
python scripts/get_app_download_url.py --project-id {projectId} --artifactory-type {artifactoryType} --path {fullPath}
```

## 参数说明


| 参数                   | 必填  | 说明                                                                         |
| -------------------- | --- | -------------------------------------------------------------------------- |
| `--project-id`       | 是   | 项目 ID                                                                      |
| `--artifactory-type` | 是   | 制品仓库类型：`PIPELINE`（流水线仓库）或 `CUSTOM_DIR`（自定义仓库），来自搜索结果的 `artifactoryType` 字段 |
| `--path`             | 是   | 手机端 App 安装包文件完整路径，仅支持 `.apk`、`.apks`、`.ipa`、`.hap`，来自搜索结果的 `fullPath` 字段   |
| `--output`           | 否   | 二维码 PNG 输出路径，默认 `{name}_{ext_type}.png`，如 `app-1.0.0_apk.png`             |


## 示例

### 获取流水线仓库制品的 APP 跳转链接二维码

```bash
python scripts/get_app_download_url.py \
  --project-id myproject \
  --artifactory-type PIPELINE \
  --path /app-1.0.0.apk
```

### 获取自定义仓库制品的 APP 跳转链接二维码

```bash
python scripts/get_app_download_url.py \
  --project-id myproject \
  --artifactory-type CUSTOM_DIR \
  --path /releases/app-1.0.0.ipa
```

## 输出格式

```yaml
qrcodes:
  - file: app-1.0.0_apk.png
  - file: app-1.0.0_apk_2.png   # 当接口返回备用跳转链接 url2 时生成
```

默认文件名规则：使用 `{name}_{ext_type}.png`，其中 `name` 为去掉安装包后缀的制品名，`ext_type` 为安装包后缀去掉点号后的类型。例如制品 `app-1.0.0.apk` 生成 `app-1.0.0_apk.png`，避免批量生成多个二维码时互相覆盖。

## 展示要求

生成二维码 PNG 后，必须将图片展示给用户，同时可附带文件路径。不要只输出 `qrcodes.file` 路径而不展示二维码图片。

## API 说明

- **接口**：`GET /v4/apigw-user/projects/{projectId}/artifactories/app_download_url`
- **认证**：`X-Bkapi-Authorization: {"access_token":"xxx","bk_username":"xxx"}`
- **参数**：`artifactoryType`、`path`

