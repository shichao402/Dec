# 获取插件官方 YAML 模板

获取指定插件的官方 YAML 配置模板，是生成流水线 YAML 时 `steps` 内容的**首选来源**。

**API 端点**：`GET /atoms/atom_yml_v2_detail?atomCode={atomCode}&defaultShowFlag={true|false}`

## 命令

```bash
# 获取插件 YAML 模板（推荐，直接拷到 steps 下使用）
python scripts/pipeline_generate_client.py atom-yaml \
  --atom-code {atomCode}

# 获取带完整注释/示例的详细模板
python scripts/pipeline_generate_client.py atom-yaml \
  --atom-code {atomCode} \
  --default-show true

# 只输出 YAML 原文（便于重定向到文件）
python scripts/pipeline_generate_client.py atom-yaml \
  --atom-code {atomCode} \
  --format raw > step_template.yml
```

## 参数说明

| 参数 | 必须 | 说明 |
|------|------|------|
| `--atom-code` | ✅ | 插件 `atomCode`（从 `list-atoms` 返回） |
| `--default-show` | | `true` 返回带注释/示例的完整模板；`false` 返回精简模板 |
| `--format` | | `pretty`（默认，含元信息头）/ `raw`（只输出 YAML）/ `envelope`（原始 JSON） |

## 与其他命令的关系

| 命令 | 返回内容 | 适用场景 |
|------|---------|---------|
| `atom-yaml` | 即用 YAML 模板（`uses` + `with` 完整骨架） | ⭐ **生成流水线 YAML 的首选** |
| `list-atoms` | 插件列表（名称、atomCode、版本） | 查找插件、确认 atomCode |
| `atom-detail` | 字段级元信息（`props.input` 字段定义） | 查某个字段的含义/类型/枚举值 |

## 推荐工作流

```
1. list-atoms  → 找到目标插件，拿到 atomCode
2. atom-yaml   → 拿官方 YAML 模板，直接粘贴到流水线 YAML 的 steps 下
3. 若有字段疑问 → atom-detail 查 props.input 的字段定义
```

## 输出示例（pretty 格式）

```
# atomCode:        linuxScript
# defaultShowFlag: None
# source:          atom_yml_v2_detail
# ----- 插件官方 YAML 模板（可直接拷贝到 steps 下使用） -----
- name: Linux 脚本
  uses: linuxScript@1.*
  with:
    script: |
      echo "hello"
```
