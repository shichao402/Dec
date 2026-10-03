# 保存流水线编排草稿

将完整的流水线编排保存为草稿，支持 **YAML（对话首选展示）** 与 **modelAndSetting JSON（保存兜底）** 两种格式。

**API 端点**：`POST /projects/{projectId}/version/save_draft`

---

## 🔴 默认策略：尽可能用 YAML 保存，失败再用 JSON 兜底

> **尽可能使用 YAML 进行流水线保存。**  
> 对话中要让用户看 YAML（直观、好读、好改）；保存首选 `--yaml-file`。  
> **YAML 失败后不要在 YAML 上反复纠错，立即用 JSON 兜底，再用 `get-version --format yaml` 拿后端规范化的 YAML 在对话里重新展示。**

### 三角闭环

```
[1] AI 在对话中以 YAML 完整展示编排（设计意图、给用户阅读/修改）
    ↓
[2] 用户确认后写到 ./pipeline.yml，调 save-draft --yaml-file
    ↓
[3a] ✅ 成功 → 取 data.version → 进入 release-version
[3b] ❌ YAML 本地校验失败（exit 2）或 API 校验失败
     ↓ 立即（不在 YAML 上反复纠）
     把同一份编排意图转成等价的 modelAndSetting JSON 写到 ./draft.json
     调 save-draft --draft-file
     ↓
     ✅ 成功 → 取 data.version
     ↓
     【关键】调 get-version --pipeline-id ... --version <new_version> --format yaml
     拿到后端渲染的规范 YAML
     ↓
     在对话中以这份后端 YAML 重新展示（声明：「已用 JSON 兜底保存，下方为后端规范化版本」）
```

---

## 命令

### 模式 A：YAML（推荐）

```bash
# 先本地校验（必做），0 error 再保存
python scripts/pipeline_yaml_lint.py --yaml-file ./pipeline.yml

python scripts/pipeline_generate_client.py save-draft \
  --project-id {projectId} \
  --pipeline-id {pipelineId} \
  --pipeline-name "{流水线名称}" \
  --yaml-file ./pipeline.yml \
  --description "v1.0 草稿"
```

`save-draft` 在发请求前会做两层预校验，任一失败都 exit 2 且不发请求：`yaml.safe_load` 语法校验，以及按 `scripts/pipeline-yaml-schema.json` 的 schema 校验（拦截后端 `status: 2100130 yaml不合法`）。高频坑与修法见 [pipeline-yaml-lint.md](./pipeline-yaml-lint.md)，确需绕过用 `--skip-lint`。

### 模式 B：modelAndSetting JSON（YAML 失败时的稳定兜底）

```bash
python scripts/pipeline_generate_client.py save-draft \
  --project-id {projectId} \
  --draft-file ./draft.json
```

`draft.json` 需包含 `pipelineId` / `modelAndSetting`，`storageType` 脚本自动补 `MODEL`。

**保存成功后必须紧跟一步**：用 `get-version --pipeline-id ... --version <new_version> --format yaml` 拿后端规范化 YAML，在对话中重新展示给用户。

---

## 参数说明

| 参数 | 必须 | 说明 |
|------|------|------|
| `--project-id` | ✅ | 项目英文名 |
| `--pipeline-id` | YAML 模式必填 | 流水线 ID（来自 `create-pipeline` 返回值） |
| `--pipeline-name` | | 流水线名称（YAML 模式可选，仅作 modelAndSetting 兜底字段） |
| `--description` | | 本次草稿的版本变更说明 |
| `--yaml` | | YAML 编排字符串（推荐使用 `--yaml-file`） |
| `--yaml-file` | | 从文件读取 YAML 编排（推荐方式） |
| `--draft` | | 草稿 JSON 字符串 |
| `--draft-file` | | 从文件读取 modelAndSetting JSON |

---

## modelAndSetting JSON 结构（兜底保存用）

```json
{
  "pipelineId": "p-xxxxx",
  "storageType": "MODEL",
  "baseVersion": 0,
  "description": "",
  "modelAndSetting": {
    "model": {
      "@type": "Model",
      "name": "流水线名称",
      "desc": "流水线描述",
      "stages": [ ... ],
      "labels": [],
      "pipelineCreator": "用户名",
      "projectId": "项目ID",
      "pipelineId": "p-xxxxx",
      "events": {},
      "staticViews": []
    },
    "setting": {
      "projectId": "项目ID",
      "pipelineId": "p-xxxxx",
      "pipelineName": "流水线名称",
      "desc": "流水线描述",
      "labels": [],
      "labelNames": [],
      "successSubscription": {
        "types": [], "groups": [], "users": "",
        "wechatGroupFlag": false, "wechatGroup": "",
        "wechatGroupMarkdownFlag": false, "detailFlag": false, "content": ""
      },
      "failSubscription": {
        "types": ["RTX", "EMAIL"], "groups": [], "users": "${{ci.actor}}",
        "wechatGroupFlag": false, "wechatGroup": "",
        "wechatGroupMarkdownFlag": false, "detailFlag": false,
        "content": "【${{ci.project_name}}】- 【${{ci.pipeline_name}}】#${{ci.build_num}} 执行失败，耗时${{ci.pipeline_execute_time}}, 触发人: ${{ci.actor}}。"
      },
      "runLockType": "MULTIPLE",
      "maxQueueSize": 10,
      "maxConRunningQueueSize": 40,
      "buildCancelPolicy": "RESTRICTED",
      "maxPipelineResNum": 50,
      "cleanVariablesWhenRetry": false,
      "pipelineAsCodeSettings": {
        "enable": false,
        "projectDialect": "CLASSIC",
        "inheritedDialect": true
      }
    }
  }
}
```

### Stage-1：触发阶段（固定结构，**不是用户阶段**）

> 🔴 JSON 里 `stage-1` 恒为触发器 stage，空白流水线也有，前端从 `stage-2` 起展示。向用户描述阶段时**跳过它**：用户的第 1 个阶段 = JSON `stage-2`。用户没写阶段就说「还没有阶段」，别说「已有 stage-1」。YAML 编排无此问题（触发器写在 `on`，`stages` 只有用户阶段）。

```json
{
  "containers": [{
    "@type": "trigger",
    "id": "0",
    "name": "trigger",
    "elements": [{
      "@type": "manualTrigger",
      "name": "手动触发",
      "id": "T-1-1-1",
      "version": "1.*",
      "classType": "manualTrigger",
      "atomCode": "manualTrigger",
      "taskAtom": ""
    }],
    "params": [],
    "containerId": "0",
    "matrixGroupFlag": false,
    "classType": "trigger"
  }],
  "id": "stage-1",
  "name": "stage-1",
  "fastKill": false,
  "finally": false
}
```

### Stage-2+：执行阶段

```json
{
  "containers": [{
    "@type": "vmBuild",
    "id": "1",
    "name": "Job-1",
    "elements": [{
      "@type": "marketBuild",
      "name": "插件名称",
      "atomCode": "插件标识",
      "version": "版本号",
      "data": {
        "input": {},
        "namespace": "",
        "output": {}
      },
      "additionalOptions": {
        "enable": true,
        "continueWhenFailed": false,
        "retryWhenFailed": false,
        "retryCount": 1,
        "timeout": 900,
        "runCondition": "PRE_TASK_SUCCESS"
      },
      "classType": "marketBuild"
    }],
    "buildEnv": {},
    "dispatchType": {
      "buildType": "PUBLIC_DEVCLOUD",
      "value": "linux",
      "imageType": "BKDEVOPS"
    },
    "matrixGroupFlag": false,
    "classType": "vmBuild"
  }],
  "id": "stage-2",
  "name": "执行阶段名称",
  "fastKill": false,
  "finally": false
}
```

> 💡 `dispatchType.buildType` 根据实际执行环境填写（`PUBLIC_DEVCLOUD` / `THIRD_PARTY_AGENT_ENV` 等）。

---

## 返回关键字段

| 字段 | 说明 |
|------|------|
| `data.version` | **草稿版本号（release-version 必需）** |
| `data.pipelineId` | 流水线 ID |
