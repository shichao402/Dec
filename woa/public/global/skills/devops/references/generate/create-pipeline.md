# 通过模板创建流水线

通过指定模板创建一条新的流水线（仅创建骨架，不含编排内容），返回 `pipelineId` 供后续 `save-draft` 使用。

**API 端点**：`POST /projects/{projectId}/version/create_with_template`

> 此接口只创建空骨架。流水线描述在后续 `save-draft` 的 `setting.desc` 中设置，不在此接口传入。

## 命令

```bash
python scripts/pipeline_generate_client.py create-pipeline \
  --project-id {projectId} \
  --pipeline-name "流水线名称" \
  --template-id {templateId} \
  --template-version {templateVersion}
```

## 参数说明

| 参数 | 必须 | 说明 |
|------|------|------|
| `--project-id` | ✅ | 项目英文名 |
| `--pipeline-name` | ✅ | 流水线名称 |
| `--template-id` | ✅ | 模板 ID（从项目模板列表获取） |
| `--template-version` | ✅ | 模板版本号（从项目模板列表获取） |

> 💡 **获取模板参数**：在蓝盾控制台「流水线」→「新建流水线」→「从模板创建」页面中可找到可用模板。若项目有空白模板，`templateId` 和 `templateVersion` 使用该模板对应的值。

## 请求体（脚本自动构建）

```json
{
  "pipelineName": "流水线名称",
  "templateId": "{templateId}",
  "templateVersion": {templateVersion},
  "instanceType": "FREEDOM",
  "emptyTemplate": true
}
```

## 返回关键字段

| 字段 | 说明 |
|------|------|
| `data.pipelineId` | 新创建的流水线 ID（后续 `save-draft` 和 `release-version` 使用） |
| `data.version` | 初始版本号 |
