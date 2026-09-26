# YAML 本地校验（save-draft 前置）

后端保存时会用 JSON Schema 校验编排，不合法会返回：

```
{"status":2100130,"message":"yaml不合法 [$.on: object found, array expected, ...]"}
```

这类错误全部可以在本地提前发现。**写完 YAML 先 lint，再 save-draft**，不要拿后端当校验器。

---

## 用法

```bash
# 校验并输出可直接照做的「修法」
python scripts/pipeline_yaml_lint.py --yaml-file ./pipeline.yml

# 机械错误自动修（会重排格式、丢注释，慎用）
python scripts/pipeline_yaml_lint.py --yaml-file ./pipeline.yml --fix --in-place

# 机器可读结果
python scripts/pipeline_yaml_lint.py --yaml-file ./pipeline.yml --json
```

退出码：`0` 通过（可能有 warning），`2` 有 error，`1` 参数/读取错误。

`save-draft --yaml-file` 会自动跑一次同样的校验，不过就 exit 2 且不发请求；确需绕过时加 `--skip-lint`。

校验分两层：

| 层 | 依赖 | 覆盖 |
|----|------|------|
| 规则校验 | 无 | `on` / `variables` / `props` / cron / step 的高频写错，附修法 |
| JSON Schema 全量校验 | `pip install jsonschema` | 官方 `scripts/pipeline-yaml-schema.json` 的所有约束 |

未装 `jsonschema` 时自动降级为规则校验并提示，不会报错中断。

---

## 高频坑速查

### 1. `on.manual` 没有 `enabled` 字段

```yaml
# ❌ $.on.manual: property 'enabled' is not defined in the schema
on:
  manual:
    enabled: true

# ✅ 简写
on:
  manual: enabled

# ✅ 对象写法：开关字段叫 enable，且是布尔
on:
  manual:
    name: 手动触发
    enable: true
    can-skip-step: true
    use-latest-inputs: true
```

`manual` 对象只接受 `id`、`name`、`enable`、`can-skip-step`、`use-latest-inputs`。

> `remote.enable` 反过来是**字符串** `enabled`/`disabled`，别和 `manual.enable` 的布尔混用。

### 2. 多触发器时 `on` 用数组

单个触发器用对象，多个（尤其多个 TAPD 项目）必须用数组：

```yaml
on:
  - manual: enabled
  - type: tapd
    workspace-id: "{workspace_id}"
    story:
      action: [create, update]
```

### 3. `props.type` 是硬枚举

只能取：`vuex-input`、`vuex-textarea`、`selector`、`checkbox`、`boolean`、`git-ref`、`svn-tag`、`code-lib`、`container-type`、`artifactory`、`sub-pipeline`、`custom-file`、`tips`、`repo-ref`、`form-list`。

| 想要 | ✅ | ❌ |
|------|----|----|
| 单行文本 | `vuex-input` | `input` / `text` / `string` |
| 多行文本 | `vuex-textarea` | `textarea` |
| 下拉单选 | `selector` + `options` | `select` / `enum` |
| 下拉多选 | `selector` + `multiple: true` | `multi-selector` |
| 布尔 | `boolean` / `checkbox` | `bool` |
| 敏感值 | 用凭据 `${{settings.<credId>.password}}` | `password`（没有这个控件） |

### 4. 变量层与 `props` 层别放错字段

变量层只接受 `value`（必填）、`readonly`、`required`、`const`、`allow-modify-at-startup`、`value-not-empty`、`props`；`label` / `description` / `options` 属于 `props`。两层都禁止多余字段。

```yaml
# ❌ description 写在变量层
target-branch:
  value: master
  description: 目标分支

# ✅
target-branch:
  value: master
  props:
    label: 目标分支
    description: 要同步的分支
    type: vuex-input
```

### 5. 不要自定义 `BK_CI_` 开头的变量

`BK_CI_*` / `CI_*` 是系统内置变量前缀，自定义会冲突。要指定构建机写 job 的 `runs-on`，不要造一个 `BK_CI_NODE_AGENT_ID` 变量塞值。

### 6. cron 是 5 段且要加引号

```yaml
on:
  schedules:
    cron: "0 9 * * *"   # 分 时 日 月 周，不支持秒级
    always: true
    repo-type: NONE
```

### 7. 裸 `on:` 在 YAML 1.1 里是布尔

用 PyYAML 程序化生成 YAML 时，顶层 `on:` 可能被解析成 `True` 键，导致触发器整块丢失。lint 工具已按 YAML 1.2 语义处理；自己写解析逻辑时用 `"on":` 更保险。

---

## 报错了怎么办

1. 把出错的 YAML 存成文件，跑 `pipeline_yaml_lint.py --yaml-file`，按「修法」改。
2. 纯机械错误可以 `--fix --in-place` 一把过，改完复校一次。
3. 复校通过再 `save-draft --yaml-file`。
4. 如果后端报了 lint 没抓到的新错误，把这条规则补进本文档和 `pipeline_yaml_lint.py`，避免下次重犯。
