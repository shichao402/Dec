# 从模板创建流水线

命令：`python scripts/template_client.py create-pipeline`

通过指定模板创建一条新的流水线。

## 参数

### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |
| --template-id | 模板ID |
| --template-version | 模板版本号 |
| --pipeline-name | 流水线名称 |

### 可选参数

| 参数 | 类型 | 说明 |
|------|------|------|
| --instance-type | string | FREEDOM(自由模式)/CONSTRAINT(约束模式) |
| --empty-template | boolean | 是否为空模板（true/false） |
| --access-token | string | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

## 示例

### 基础创建

```bash
python scripts/template_client.py create-pipeline \
  --project-id myproject \
  --template-id xxx \
  --template-version 1 \
  --pipeline-name my-pipeline
```

### 约束模式创建

```bash
python scripts/template_client.py create-pipeline \
  --project-id myproject \
  --template-id xxx \
  --template-version 1 \
  --pipeline-name constrained-pipeline \
  --instance-type CONSTRAINT
```

## 返回说明

```json
{
  "data": {
    "pipelineId": "p-xxx",
    "pipelineName": "my-pipeline",
    "version": 1,
    "versionName": "init",
    "versionNum": 1
  },
  "status": 0
}
```

## 工作流

```
1. 调用 list 获取可用模板
2. 调用 get 查看模板详情和版本
3. 向用户确认模板和参数 ⚠️
4. 调用 create-pipeline 创建流水线
5. 使用返回的 pipelineId 进行后续操作
```

## 使用场景

1. **单条流水线创建**：指定模板和版本，创建一条流水线
2. **约束模式**：实例不可修改模板定义的内容
3. **自由模式**：实例可自由修改模板内容（默认）
