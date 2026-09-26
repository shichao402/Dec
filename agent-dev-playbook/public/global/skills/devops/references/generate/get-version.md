# 获取流水线指定版本的编排（YAML / modelAndSetting）

拉取某条流水线**某个版本号**对应的完整编排，YAML 字段可直接用于阅读、修改与转发；modelAndSetting JSON 字段用于精细字段操作。

> 💡 **本接口在保存流程中的关键作用**：当 `save-draft` 用 YAML 失败、改用 modelAndSetting JSON 兜底保存成功后，**必须**用本接口的 `--format yaml` 拉一次后端规范化 YAML，在对话里重新展示给用户。详见 [`save-draft.md`](./save-draft.md)。

**API 端点**：`GET /projects/{projectId}/version/get_version?pipelineId={pipelineId}&version={version}`

---

## 命令

```bash
python scripts/pipeline_generate_client.py get-version \
  --project-id {projectId} \
  --pipeline-id {pipelineId} \
  --version {version} \
  [--format yaml|json|summary|full]
```

| 参数 | 必填 | 说明 |
|------|------|------|
| `--project-id` | ✅ | 项目英文名 |
| `--pipeline-id` | ✅ | 流水线 ID（`p-` 开头） |
| `--version` | ✅ | 流水线编排版本号（整数，来自 `pipelines` 列表的 `version` / `pipelineVersion` 字段） |
| `--format` | | 输出格式，默认 `yaml`。可选 `yaml` / `json` / `summary` / `full` |

> ⚠️ 这里的 `version` 是**流水线编排版本号**（整数自增），不是构建号 `buildNum`。

---

## 输出格式

### `--format yaml`（默认，推荐）

当 `yamlSupported=true` 且 `yamlPreview.yaml` 非空时直接输出 YAML 原文，并在顶部用注释附带版本元信息。`yamlSupported=false` 时自动降级为 summary 视图。

```yaml
# projectId:    myproject
# pipelineId:   p-abc123
# version:      42 (V42)
# baseVersion:  41 (V41)
# updater:      {username}
# updateTime:   2026-05-15 10:08:13
# description:  {版本变更说明}
# ----- YAML 编排原文如下 -----
version: v3.0
name: {流水线名称}
...
```

### `--format json`

输出 `_meta` + `modelAndSetting` 的格式化 JSON，供 AI 解析编排结构（用户视图始终保持 YAML，不要把 JSON 字典展示给用户）。

### `--format summary`

只输出版本元信息 + stage / job / 插件的结构摘要，适合快速概览编排骨架。

### `--format full`

输出 API 原始 `data` 全部字段，适合排查字段差异。

---

## 返回的关键字段

| 字段 | 说明 |
|------|------|
| `data.modelAndSetting.model` | JSON 编排（stages → containers → elements 四层结构） |
| `data.modelAndSetting.setting` | 流水线设置（通知、并发、PAC 等） |
| `data.yamlPreview.yaml` | **YAML 原文**（`yamlSupported=true` 时为完整 YAML 编排） |
| `data.yamlSupported` | 是否支持 YAML 解析 |
| `data.yamlInvalidMsg` | YAML 解析异常信息（YAML 不可用时的原因） |
| `data.version` / `versionName` | 当前请求的版本号 / 版本名 |

---

## 典型使用场景

| 场景 | 推荐流程 |
|------|---------|
| 查看流水线当前编排 | `pipelines` 拿 `version` → `get-version --format yaml` 展示 |
| 基于现有流水线改编排 | `get-version --format yaml > pipeline.yml` → 修改 → `save-draft --yaml-file` |
| 快速看骨架（stage/job/插件） | `get-version --format summary` |
| YAML 保存失败、JSON 兜底后回拉规范 YAML | `get-version --version <new_version> --format yaml` |
| 排查字段差异 | `get-version --format full` |
