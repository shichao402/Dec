# 模板删除

## 删除模板

命令：`python scripts/template_client.py delete`

### ⚠️ 重要：必须先获得用户确认

**在调用此命令前，必须向用户展示模板信息并获得明确确认。未经确认禁止执行。**

确认内容必须包括：
- 项目 ID（projectId）
- 模板 ID（templateId）
- 模板名称

### 参数

| 参数 | 必需 | 说明 |
|------|------|------|
| --project-id | 是 | 项目英文名 |
| --template-id | 是 | 模板ID |
| --access-token | 否 | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

### 示例

```bash
python scripts/template_client.py delete \
  --project-id myproject \
  --template-id xxx
```

### 返回说明

```json
{
  "data": true,
  "message": "",
  "status": 0
}
```

---

## 删除模板版本

命令：`python scripts/template_client.py delete-version`

### ⚠️ 重要：必须先获得用户确认

### 参数

| 参数 | 必需 | 说明 |
|------|------|------|
| --project-id | 是 | 项目英文名 |
| --template-id | 是 | 模板ID |
| --version | 是 | 版本号 |
| --access-token | 否 | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

### 示例

```bash
python scripts/template_client.py delete-version \
  --project-id myproject \
  --template-id xxx \
  --version 2
```

### 工作流

```
1. 调用 get 查看模板详情，确认版本信息
2. ⚠️ 向用户展示版本信息，等待用户确认
3. 用户确认后调用 delete-version 删除指定版本
```
