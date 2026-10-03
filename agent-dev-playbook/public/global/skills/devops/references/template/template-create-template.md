# 基于已有模板创建新模板

命令：`python scripts/template_client.py create-template`

基于已有模板的流水线模型（Model），创建一个新的模板。

## 参数

### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |
| --source-template-id | 源模板ID |
| --template-name | 新模板名称 |

### 可选参数

| 参数 | 类型 | 说明 |
|------|------|------|
| --source-version | number | 源模板版本号（默认最新） |
| --description | string | 新模板描述 |
| --access-token | string | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

## 示例

### 基础创建

```bash
python scripts/template_client.py create-template \
  --project-id myproject \
  --source-template-id xxx \
  --template-name my-new-template
```

### 指定源版本和描述

```bash
python scripts/template_client.py create-template \
  --project-id myproject \
  --source-template-id xxx \
  --source-version 2 \
  --template-name my-new-template \
  --description "基于xxx模板创建"
```

## 返回说明

```json
{
  "data": {
    "id": "new-template-id"
  },
  "status": 0
}
```

## 工作流

```
1. 调用 get 查看源模板详情和版本列表
2. 向用户确认新模板名称 ⚠️
3. 调用 create-template 创建新模板
4. 调用 get 验证新模板已创建
```
