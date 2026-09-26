# 修改流水线参数

命令：`python scripts/pipeline_client.py update-params`

修改流水线的默认启动参数（即流水线编辑页面中的"流水线变量"默认值）。

## ⚠️ 重要：必须先获得用户确认

**在调用此命令前，必须向用户展示将要修改的参数内容并获得明确确认。未经确认禁止执行。**

确认内容必须包括：
- 项目 ID（projectId）
- 流水线 ID（pipelineId）
- 待修改的参数名及新值

## 参数

### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |
| --pipeline-id | 流水线 ID（p-开头） |
| --params | 要修改的参数，JSON 字符串，格式为 `{"参数名": "新值", ...}` |

### 可选参数

| 参数 | 类型 | 说明 |
|------|------|------|
| --access-token | string | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

## 工作流

```
1. ⚠️ 向用户展示将要修改的参数名和新值，等待用户确认
2. 用户确认后调用 update-params 执行修改
3. 脚本自动 GET 流水线定义 → 修改对应参数 → PUT 回去
4. 验证修改结果（脚本会输出修改前后的值和新版本号）
```

## 示例

### 修改单个参数

```bash
python scripts/pipeline_client.py update-params \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --params '{"BRANCH_NAME": "release/1.4.0.0"}'
```

### 同时修改多个参数

```bash
python scripts/pipeline_client.py update-params \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --params '{"BRANCH_NAME": "release/1.4.0.0", "BUILD_TYPE": "release"}'
```

## 返回说明

成功修改后输出：

```
修改参数: BRANCH_NAME: release/1.3.0.0 -> release/1.4.0.0
PUT 成功，新版本号: 25
验证成功: BRANCH_NAME = release/1.4.0.0
```

## 注意事项

- **⚠️ 必须在修改前获得用户的明确确认**
- 修改的是流水线定义中 `stages[0].containers[0].params` 的 `defaultValue`，即流水线变量的默认值
- 若指定的参数名在流水线中不存在，脚本会报错提示
