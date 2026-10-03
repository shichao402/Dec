# 查询模板

## 获取模板列表

命令：`python scripts/template_client.py list`

查询项目下的模板列表，返回数据已清洗保留关键字段。

### 参数

#### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |

#### 可选参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| --page | number | 1 | 页码 |
| --page-size | number | 20 | 每页条数（最大100） |
| --template-type | string | - | 模板类型（CUSTOMIZE/CONSTRAINT/PUBLIC） |
| --store-flag | boolean | - | 是否已关联商店（true/false） |
| --order-by | string | - | 排序字段（NAME/CREATOR/CREATE_TIME） |
| --sort | string | - | 排序方式（ASC/DESC） |
| --access-token | string | - | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

### 示例

#### 获取所有模板

```bash
python scripts/template_client.py list \
  --project-id myproject
```

#### 按类型筛选

```bash
python scripts/template_client.py list \
  --project-id myproject \
  --template-type CUSTOMIZE
```

#### 按名称排序

```bash
python scripts/template_client.py list \
  --project-id myproject \
  --order-by NAME \
  --sort ASC
```

### 返回数据结构（已清洗）

```json
{
  "status": 0,
  "data": {
    "projectId": "myproject",
    "count": 10,
    "hasPermission": true,
    "hasCreatePermission": true,
    "models": [
      {
        "templateId": "xxx",
        "name": "模板名称",
        "templateType": "CUSTOMIZE",
        "templateTypeDesc": "自定义",
        "creator": "user1",
        "updateTime": 1700000000000,
        "version": 3,
        "versionName": "v1.2",
        "storeFlag": false,
        "hasPermission": true,
        "hasInstance2Upgrade": false,
        "canEdit": true,
        "canDelete": true
      }
    ]
  }
}
```

---

## 获取模板详情

命令：`python scripts/template_client.py get`

获取模板的完整详情，包含流水线模型、版本列表、关联实例等。

### 参数

| 参数 | 必需 | 说明 |
|------|------|------|
| --project-id | 是 | 项目英文名 |
| --template-id | 是 | 模板ID |
| --version | 否 | 模板版本（默认最新） |
| --access-token | 否 | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

### 示例

#### 获取最新版本

```bash
python scripts/template_client.py get \
  --project-id myproject \
  --template-id xxx
```

#### 获取指定版本

```bash
python scripts/template_client.py get \
  --project-id myproject \
  --template-id xxx \
  --version 2
```

### 使用场景

1. **创建流水线前**：先查看模板详情和版本
2. **了解模板结构**：查看 stages/containers/elements 定义
3. **获取变量列表**：了解模板定义的参数，用于实例化
