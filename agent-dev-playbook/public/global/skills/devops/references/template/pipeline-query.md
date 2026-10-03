# 流水线查询

提供流水线编排和启动参数的查询能力，方便用户在实例化或更新模板实例时参考已有流水线的参数配置。

---

## 获取流水线编排

命令：`python scripts/template_client.py get-pipeline`

获取流水线的完整编排（Model），包含 stages、containers、elements 等。

### 参数

| 参数 | 必需 | 说明 |
|------|------|------|
| --project-id | 是 | 项目英文名 |
| --pipeline-id | 是 | 流水线ID |
| --params-only | 否 | 仅输出参数列表 |
| --access-token | 否 | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

### 示例

#### 获取完整编排

```bash
python scripts/template_client.py get-pipeline \
  --project-id myproject \
  --pipeline-id p-xxx
```

#### 仅查看参数

```bash
python scripts/template_client.py get-pipeline \
  --project-id myproject \
  --pipeline-id p-xxx \
  --params-only
```

### 返回说明（--params-only）

```json
{
  "status": 0,
  "params": [
    {
      "id": "env",
      "name": "select",
      "type": "ENUM",
      "required": true,
      "defaultValue": "release",
      "options": [
        {"key": "dev", "value": "dev"},
        {"key": "test", "value": "test"}
      ],
      "desc": ""
    }
  ]
}
```

---

## 获取流水线手动启动参数

命令：`python scripts/template_client.py get-pipeline-startup-info`

获取流水线的手动启动参数信息，包含参数类型、默认值、当前值等。

### 参数

| 参数 | 必需 | 说明 |
|------|------|------|
| --project-id | 是 | 项目英文名 |
| --pipeline-id | 是 | 流水线ID |
| --access-token | 否 | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

### 示例

```bash
python scripts/template_client.py get-pipeline-startup-info \
  --project-id myproject \
  --pipeline-id p-xxx
```

### 使用场景

1. **实例化前参考**：查看其他模板实例的参数配置，作为新实例的参考
2. **更新实例前对比**：对比新旧版本参数差异，决定如何填写参数值
3. **调试排查**：查看流水线当前的参数定义是否符合预期
