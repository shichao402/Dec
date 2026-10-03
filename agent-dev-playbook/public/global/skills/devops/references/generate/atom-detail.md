# 获取插件详细信息

根据插件代码和版本获取插件的详细信息，**核心作用是获取 `props` 字段**来了解插件的输入/输出参数定义。通常在 `atom-yaml` 返回的模板中有字段不清楚时作为补充查询。

**API 端点**：`GET /atoms/atom_detail`

## 命令

```bash
python scripts/pipeline_generate_client.py atom-detail \
  --atom-code {atomCode} \
  [--version {version}]
```

## 参数说明

| 参数 | 必须 | 说明 |
|------|------|------|
| `--atom-code` | ✅ | 插件代码（从 `list-atoms` 返回的 `atomCode`） |
| `--version` | | 版本号（如 `2.*`），默认 `1.*` |
| `--service-scope` | | 服务范围（`PIPELINE` / `QUALITY` 等） |

## 返回字段说明

| 字段 | 说明 |
|------|------|
| `name` | 插件名称 |
| `atomCode` | 插件唯一标识码 |
| `classType` | 插件类型：`marketBuild` / `marketBuildLess` |
| **`props`** | **⭐ 核心字段：插件参数定义** |
| `props.input` | **输入参数定义**（编排时必须关注） |
| `props.output` | 输出参数定义 |
| `versionList` | 可用版本列表 |

## props.input 字段解析

`props.input` 是一个 Map，每个 key 是参数名，value 是参数定义对象：

```json
{
  "label": "参数显示名称",
  "type": "vuex-input | vuex-textarea | atom-checkbox | enum-input | ...",
  "default": "默认值",
  "required": true,
  "desc": "参数描述",
  "rely": {
    "operation": "AND",
    "expression": [{ "key": "依赖的参数名", "value": "依赖的值" }]
  }
}
```

| props.input 中的 type | 编排时值类型 |
|---|---|
| `vuex-input` / `vuex-textarea` | 字符串 |
| `atom-checkbox` | boolean |
| `enum-input` | 字符串（从 list 中选 value） |
