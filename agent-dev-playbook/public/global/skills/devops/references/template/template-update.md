# 更新模板

命令：`python scripts/template_client.py update-template`

获取当前模板的模型，应用修改后提交为新版本。

## 参数

### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |
| --template-id | 模板ID |
| --version-name | 新版本名称 |

### 可选参数

| 参数 | 类型 | 说明 |
|------|------|------|
| --add-params | JSON | 要添加的参数列表 |
| --model-json | JSON | 自定义模型覆盖（高级用法） |
| --access-token | string | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

## --add-params 格式

```json
[
  {
    "id": "paramName",
    "name": "paramName",
    "required": false,
    "type": "STRING",
    "defaultValue": "",
    "desc": "参数描述",
    "readOnly": false,
    "valueNotEmpty": false
  }
]
```

支持的参数类型：`STRING`、`ENUM`、`BOOLEAN`、`TEXTAREA`、`SVN_TAG`、`GIT_REF` 等。

## 示例

### 新增一个字符串参数

```bash
python scripts/template_client.py update-template \
  --project-id myproject \
  --template-id xxx \
  --version-name v2 \
  --add-params '[{"id":"env","name":"env","required":true,"type":"STRING","defaultValue":"prod","desc":"部署环境"}]'
```

### 新增一个枚举参数

```bash
python scripts/template_client.py update-template \
  --project-id myproject \
  --template-id xxx \
  --version-name v3 \
  --add-params '[{"id":"region","name":"region","required":true,"type":"ENUM","defaultValue":"sh","options":[{"key":"sh","value":"sh"},{"key":"bj","value":"bj"}],"desc":"部署地域"}]'
```

## 工作流

```
1. 调用 get 查看模板当前详情和参数
2. 确认要修改的内容
3. 调用 update-template 提交更新
4. 调用 get 验证更新结果
```
