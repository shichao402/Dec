# 发布流水线版本

将草稿发布为正式版本，使流水线可被执行。

**API 端点**：`POST /projects/{projectId}/version/release_version?pipelineId={pipelineId}&version={version}`

## 命令

```bash
python scripts/pipeline_generate_client.py release-version \
  --project-id {projectId} \
  --pipeline-id {pipelineId} \
  --version {draftVersion} \
  --description "版本描述"
```

## 参数说明

| 参数 | 必须 | 说明 |
|------|------|------|
| `--project-id` | ✅ | 项目英文名 |
| `--pipeline-id` | ✅ | 流水线 ID（`create-pipeline` 返回） |
| `--version` | ✅ | 草稿版本号（`save-draft` 返回的 `data.version`） |
| `--description` | | 版本描述 |

## 请求体（脚本自动构建）

```json
{
  "description": "版本描述",
  "enablePac": false,
  "yamlInfo": null
}
```

## 返回关键字段

| 字段 | 说明 |
|------|------|
| `data.pipelineId` | 流水线 ID |
| `data.pipelineName` | 流水线名称 |
| `data.version` | 发布的版本号 |
| `data.versionNum` | 发布版本序号 |
