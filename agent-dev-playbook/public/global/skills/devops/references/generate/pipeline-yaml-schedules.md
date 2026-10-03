# 定时触发（YAML `on.schedules`）

通过普通流水线的 **定时触发器** 按 cron 周期自动跑任务。

> 配置：[在YAML文件中添加触发器 · schedules](https://iwiki.woa.com/p/4009967228)  
> FAQ：[【定时触发】常见问题](https://iwiki.woa.com/p/17504759)  
> Schema：`scripts/pipeline-yaml-schema.json` → `on.schedules`

---

## 🔴 判定口诀：凡「将来要自动反复执行」→ `on.schedules`，禁止 Agent 自定时

不要只认「定时」「每天」字面。命中下列任一即按定时意图处理：

| 类型 | 示例 |
|------|------|
| 显式周期 | 定时、定期、每天/每周/每月、工作日、每隔 N 分钟/小时 |
| 日程口语 | 到点自动、排期、挂计划任务、nightly、cron、schedule |
| 业务暗示 | 巡检、探活、同步、清理、备份、日报/周报、对账、续期、无人值守、别每次手动点 |
| 改编排 | 给现有流水线加定时、改触发时间 |

**边界：** 「现在跑一次」→ 普通启动，不加 schedules；纯要方案可先讲 cron，并问是否落地流水线定时。

本 skill 只处理**普通流水线**定时。

---

## 字段与示例

| 字段 | 说明 |
|------|------|
| `cron` | crontab；不支持秒级 |
| `always` | `false`=代码有变更才跑；`true`=到点必跑 |
| `branches` | 最多 3 个明确分支，不支持 `*` |
| `repo-name` / `repo-id` / `repo-type` | 代码库；无仓库可用 `repo-type: NONE` |
| `start-params` | 定时注入变量 → `${{ variables.xxx }}` |

```yaml
on:
  schedules:
    cron: "0 1 * * *"
    repo-type: NONE
    always: true
  manual: enabled
```

改 cron 后必须 `save-draft --yaml-file` + `release-version`。

### 自然语言 → cron（常用）

| 说法 | cron 示例 |
|------|-----------|
| 每天 01:00 | `0 1 * * *` |
| 工作日 09:00 | `0 9 * * 1-5` |
| 每周五 17:00 | `0 17 * * 5` |
| 每小时 | `0 * * * *` |

无代码库、到点必跑：`always: true` + `repo-type: NONE`。有仓库且「有变更才跑」：`always: false` 并写清 `repo-name` / `repo-id`。
