# 获取流水线手动启动参数

命令：`python scripts/pipeline_client.py startup-info`

获取流水线启动时需要填写的参数信息。在启动构建前调用此接口了解需要传递哪些参数。

## 参数

| 参数 | 必需 | 说明 |
|------|------|------|
| --project-id | 是 | 项目英文名 |
| --pipeline-id | 是 | 流水线 ID（p-开头） |
| --version | 否 | 流水线版本号 |
| --branch | 否 | 分支版本，仅 PAC 流水线有效。当流水线开启了 PAC，需要触发分支版本的流水线时传入 |
| --access-token | 否 | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

## 使用场景

1. **启动构建前**：先调用此接口获取参数列表
2. **了解参数要求**：查看哪些参数是必填的
3. **获取默认值**：了解参数的默认值

## 示例

### 基本用法

```bash
python scripts/pipeline_client.py startup-info \
  --project-id myproject \
  --pipeline-id p-abc123
```

### 指定版本

```bash
python scripts/pipeline_client.py startup-info \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --version 45
```

### 指定分支（PAC 流水线）

```bash
python scripts/pipeline_client.py startup-info \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --branch feature/my-feature
```

## 工作流

```
1. 调用 startup-info 获取参数定义
2. 根据返回的参数列表准备构建参数
3. 调用 start 启动构建
```

## 返回说明

返回数据中的 `properties` 数组包含了流水线启动时需要传递的参数定义：

- `name`: 参数名
- `type`: 参数类型
- `defaultValue`: 默认值
- `required`: 是否必填
- `options`: 可选项（如果是枚举类型）
- `desc`: 参数描述
