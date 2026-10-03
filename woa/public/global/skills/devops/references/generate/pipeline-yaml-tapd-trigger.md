# TAPD 事件触发器（YAML）

通过 YAML `on` 块监听 **TAPD** 需求（story）/ 缺陷（bug）事件，自动启动普通流水线。

> 权威示例来源：[监听 TAPD 事件](https://iwiki.woa.com/p/4023261231)  
> Schema 字段以仓库 `.docs/pipeline-yaml-schema.json` 的 `properties.on` 为准（含完整 `action` 枚举）。

**编排与保存优先走 YAML**：对话展示 YAML → `save-draft --yaml-file` → 失败再 `--draft-file` JSON 兜底 → `get-version --format yaml` 回拉。

---

## 1. 核心字段

| 字段 | 必填 | 说明 |
|------|------|------|
| `type` | ✅ | 固定写 `tapd` |
| `workspace-id` | ✅ | TAPD 项目（workspace）ID；须向用户确认，禁止臆造 |
| `story` | 可选 | 监听需求事件；至少配置 `action` |
| `bug` | 可选 | 监听缺陷事件；至少配置 `action` |
| `manual` | 可选 | 同块叠加手动触发：`enabled` / `disabled` |
| `id` / `name` / `enable` | 可选 | 写在 `story` / `bug` 上，多触发器时区分 |

### `story.action`

`create` · `update` · `delete` · `status_change` · `bug_link` · `bug_unlink` · `story_link` · `story_unlink` · `add_comment` · `update_comment` · `delete_comment`

### `bug.action`

`create` · `update` · `delete` · `status_change` · `add_comment` · `update_comment` · `delete_comment`

---

## 2. 仅监听 1 个 TAPD 项目

```yaml
version: v3.0
name: trigger/t-tapd.yml
on:
  type: tapd
  workspace-id: "{your_tapd_workspace_id}"
  story:
    action:
      - create
      - update
      - delete
  bug:
    action:
      - create
      - update
      - delete
stages:
  - name: stage-handle
    jobs:
      job_main:
        name: handle
        steps:
          - name: print-tapd-context
            run: |-
              echo "ci.tapd_workspace_id is ${{ ci.tapd_workspace_id }}"
              echo "ci.tapd_id is ${{ ci.tapd_id }}"
              echo "ci.event_url is ${{ ci.event_url }}"
              echo "ci.event is ${{ ci.event }}"
              echo "ci.action is ${{ ci.action }}"
              echo "ci.event_from is ${{ ci.event_from }}"
              echo "ci.actor is ${{ ci.actor }}"
              echo "ci.build_msg is ${{ ci.build_msg }}"
              echo "ci.tapd_parent_id is ${{ ci.tapd_parent_id }}"
              echo "ci.tapd_priority is ${{ ci.tapd_priority }}"
              echo "ci.tapd_link_id is ${{ ci.tapd_link_id }}"
              echo "ci.tapd_link_type is ${{ ci.tapd_link_type }}"
              echo "ci.tapd_title is ${{ ci.tapd_title }}"
```

> PAC 文档也支持顶层 `steps:` 简写；本 skill 生成普通流水线时优先用 `stages`/`jobs`/`steps`，与 `save-draft` / `get-version` 回拉形态一致。

---

## 3. TAPD + 手动触发

```yaml
on:
  type: tapd
  workspace-id: "{your_tapd_workspace_id}"
  story:
    action: [create, update, delete]
  bug:
    action: [create, update, delete]
  manual: enabled
```

---

## 4. 多个 TAPD 项目（`on` 数组）

```yaml
on:
  - manual: enabled
  - type: tapd
    workspace-id: "{your_tapd_workspace_id_1}"
    story:
      id: trigger_1
      action: [create, update, delete]
    bug:
      id: trigger_2
      action: [create, update, delete]
  - type: tapd
    workspace-id: "{your_tapd_workspace_id_2}"
    story:
      id: trigger_3
      action: [create, update, delete]
    bug:
      id: trigger_4
      action: [create, update, delete]
```

---

## 5. 运行时上下文

| 表达式 | 含义 |
|--------|------|
| `${{ ci.tapd_workspace_id }}` | TAPD 项目 ID |
| `${{ ci.tapd_id }}` | story/bug ID |
| `${{ ci.tapd_title }}` | 标题 |
| `${{ ci.tapd_parent_id }}` | 父对象 ID |
| `${{ ci.tapd_priority }}` | 优先级 |
| `${{ ci.tapd_link_id }}` / `${{ ci.tapd_link_type }}` | 关联对象 |
| `${{ ci.event }}` / `${{ ci.action }}` | 事件类型 / 动作 |
| `${{ ci.event_url }}` / `${{ ci.event_from }}` | 链接 / 来源 |
| `${{ ci.actor }}` / `${{ ci.build_msg }}` | 操作人 / 构建说明 |

---

## 6. Agent 操作清单

1. 用户提到 TAPD 触发 / 监听需求缺陷 → 用本文写 YAML `on`
2. 确认 `workspace-id` 与 `action` 列表
3. 对话展示完整 YAML → 确认后 `save-draft --yaml-file`
4. YAML 失败一次 → 立即 JSON 兜底 → `get-version --format yaml` 回拉
5. 不要臆造 workspace-id；不要优先手搓 JSON trigger 插件

---

## 7. 反模式

| 反模式 | 正确做法 |
|--------|---------|
| 臆造 `workspace-id` | 用户提供或从现有编排回读 |
| 多项目仍用单一 `on` object | 使用 `on: [ ... ]` |
| 漏写 `type: tapd` | 每个 TAPD 项必须带 `type: tapd` |
| 优先 modelAndSetting JSON 保存 | 尽可能 `--yaml-file`；仅失败才兜底 |
