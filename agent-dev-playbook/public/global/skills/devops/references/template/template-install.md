# 安装研发商店模板

命令：`python scripts/template_client.py install`

将研发商店中的模板安装到指定项目。

## 参数

### 必需参数

| 参数 | 说明 |
|------|------|
| --template-code | 商店模板代码（商店中的标识） |
| --project-codes | 目标项目列表（逗号分隔） |

### 可选参数

| 参数 | 说明 |
|------|------|
| --return-id | 加上此标志后返回安装后生成的模板ID |
| --access-token | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

## 示例

### 安装到单个项目

```bash
python scripts/template_client.py install \
  --template-code xxx \
  --project-codes myproject
```

### 安装到多个项目

```bash
python scripts/template_client.py install \
  --template-code xxx \
  --project-codes project1,project2,project3
```

### 安装并返回模板ID

```bash
python scripts/template_client.py install \
  --template-code xxx \
  --project-codes myproject \
  --return-id
```

## 返回说明

### 不带 --return-id

```json
{
  "data": true,
  "message": "",
  "status": 0
}
```

### 带 --return-id

返回安装后各项目的模板ID映射，便于后续直接使用模板进行实例化或创建流水线。

## 使用场景

1. **安装后立即使用**：加 `--return-id` 拿到模板ID，直接调用 create-pipeline
2. **批量分发模板**：一次安装到多个项目
