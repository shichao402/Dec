# 模板实例管理

## 批量实例化模板

命令：`python scripts/template_client.py instantiate`

从模板批量创建多条流水线。

### 参数

#### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |
| --template-id | 模板ID |
| --version | 模板版本 |
| --instances | 实例列表JSON |

#### 可选参数

| 参数 | 类型 | 说明 |
|------|------|------|
| --use-template-settings | boolean | 是否应用模板设置（true/false） |
| --access-token | string | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

### --instances 格式

```json
[
  {"pipelineName": "pipeline-1"},
  {"pipelineName": "pipeline-2", "param": [], "buildNo": null}
]
```

### 示例

#### 基础实例化

```bash
python scripts/template_client.py instantiate \
  --project-id myproject \
  --template-id xxx \
  --version 1 \
  --instances '[{"pipelineName":"pipe-1"},{"pipelineName":"pipe-2"}]'
```

#### 使用模板设置

```bash
python scripts/template_client.py instantiate \
  --project-id myproject \
  --template-id xxx \
  --version 1 \
  --use-template-settings true \
  --instances '[{"pipelineName":"pipe-1"}]'
```

### ⚠️ 重要：实例化前必须获得用户确认

**在调用此命令前，必须先获取模板详情并向用户展示以下信息：**

1. 模板名称、当前版本
2. 模板定义的参数列表（参数名、类型、默认值、可选值）
3. 询问用户是否需要修改参数默认值
4. 确认实例名称和数量

**确认流程：**
```
1. 调用 get 获取模板详情
2. 提取 params 字段，展示参数表格
3. 询问用户：
   - 实例名称是什么？需要创建几个？
   - 是否需要修改参数的默认值？
4. 用户确认后，构建 instances JSON 并执行
```

### 返回说明

```json
{
  "data": {
    "successPipelines": ["pipe-1", "pipe-2"],
    "successPipelinesId": ["p-xxx1", "p-xxx2"],
    "failurePipelines": [],
    "failureMessages": {}
  },
  "status": 0
}
```

---

## 获取模板实例列表

命令：`python scripts/template_client.py instances`

### 参数

#### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |
| --template-id | 模板ID |

#### 可选参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| --page | number | - | 页码 |
| --page-size | number | 20 | 每页条数（最大30） |
| --search-key | string | - | 名字搜索关键字 |
| --sort-type | string | - | PIPELINE_NAME/VERSION/UPDATE_TIME/STATUS |
| --desc | boolean | - | 是否降序（true/false） |
| --access-token | string | - | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

### 示例

#### 基本查询

```bash
python scripts/template_client.py instances \
  --project-id myproject \
  --template-id xxx
```

#### 搜索并排序

```bash
python scripts/template_client.py instances \
  --project-id myproject \
  --template-id xxx \
  --search-key deploy \
  --sort-type UPDATE_TIME \
  --desc true
```

### 返回说明

```json
{
  "data": {
    "templateId": "xxx",
    "count": 5,
    "latestVersion": {
      "version": 3,
      "versionName": "v1.2",
      "creator": "user1"
    },
    "instances": [
      {
        "pipelineId": "p-xxx",
        "pipelineName": "实例名",
        "version": 2,
        "versionName": "v1.1",
        "status": "UPDATED",
        "hasPermission": true
      }
    ]
  },
  "status": 0
}
```

---

## 批量更新模板实例

命令：`python scripts/template_client.py update-instances`

支持两种方式指定目标版本：
- `--version`：按数字版本号
- `--version-name`：按版本名称

### 参数

#### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |
| --template-id | 模板ID |
| --version 或 --version-name | 目标版本（二选一） |
| --instances | 实例列表JSON |

#### 可选参数

| 参数 | 类型 | 说明 |
|------|------|------|
| --use-template-settings | boolean | 是否应用模板设置 |
| --access-token | string | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

### --instances 格式

```json
[
  {
    "pipelineId": "p-xxx",
    "pipelineName": "流水线名称",
    "resetBuildNo": false
  }
]
```

### 示例

#### 按版本号更新

```bash
python scripts/template_client.py update-instances \
  --project-id myproject \
  --template-id xxx \
  --version 3 \
  --instances '[{"pipelineId":"p-xxx","pipelineName":"my-pipe"}]'
```

#### 按版本名称更新

```bash
python scripts/template_client.py update-instances \
  --project-id myproject \
  --template-id xxx \
  --version-name v1.2 \
  --instances '[{"pipelineId":"p-xxx","pipelineName":"my-pipe"}]'
```

### ⚠️ 重要：更新实例前必须获得用户确认

**在调用此命令前，必须先获取模板详情并向用户展示以下信息：**

1. 目标版本与当前版本的差异（版本号、版本名称）
2. 新版本的完整参数列表，特别标注新增/变更的参数
3. 询问用户每个实例的参数值要如何填写（使用默认值还是自定义）
4. 确认要更新哪些实例

### 工作流

```
1. 调用 get 获取模板详情，了解目标版本的参数
2. 调用 instances 查看当前实例列表和版本
3. 向用户展示：
   - 新旧版本对比
   - 新版本的参数列表（标注新增/变更参数）
   - 询问每个实例的参数如何填写
   - 确认要更新哪些实例
4. 用户确认后，构建 instances JSON 并执行 update-instances
```
