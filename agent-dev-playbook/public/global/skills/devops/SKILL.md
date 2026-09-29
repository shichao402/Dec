---
name: devops
description: 蓝盾（BK-CI，亦称蓝鲸 CI）DevOps 全能助手，覆盖项目查询与管理、流水线查询与构建、编排生成（含 YAML lint、代码库托管、git 凭据、定时/TAPD 触发）、构建日志与故障诊断、构建制品、bk-repo 制品库、流水线模板、Model JSON 解释和管理员权限治理。当用户提及蓝盾、蓝鲸 CI、BK-CI 的项目、创建/编辑项目、普通流水线、构建、部署、CI/CD、YAML 编排、Helm、插件、定时/巡检、TAPD 触发、git 授权、代码库托管、构建日志、失败原因、卡住、制品、bk-repo、流水线模板、蓝盾权限、用户组、授权或流水线代持人时使用。不处理创作流或云桌面取文件。
---

# 蓝盾流水线全能助手

按意图只读 `references/` 下对应文件，禁止一次读完整个目录。

## 会话初始化与热更新

每次会话首次加载本 skill 时，只在当前 skill 根目录执行一次 `python3 ./hot_reload.py`。用户明确说「更新 skill / 拉最新版」时改为 `--force`。

必须读取 stdout 的 JSON，禁止仅凭退出码或空输出判断。`updated` / `up_to_date` 才表示已更新或已是最新；`skipped` 只是跳过。`updated` 且 `local_backup` 非空时，告知用户备份路径。

`action` 为 `error`，或脚本找不到文件时，只简短提示并继续处理原需求；同一会话不要重试，也不要手工拼文件。TTL、冷却、开关和覆盖规则见 [热更新](references/hot-reload.md)。

## 鉴权

token 从 <https://devops.woa.com/ms/auth/api/user/bkToken/get> 获取。引导时必须说明：先访问该链接；仅当返回 401、无权限或未登录时，先登录 <https://devops.woa.com/console/> 再重试。禁止省略这条条件，也不要把登录控制台写成固定前置。

配置只允许两种：

1. 用户把 token 发给 AI：不回显；本次 `--access-token`，并用 `scripts/auth_cli.py login` 写入 `config.json`。
2. 用户自行复制 `config.example.json` 为 `config.json`。

不要推荐环境变量。缺 token 或 HTTP 401 时读 [鉴权配置](references/auth-setup.md)。权限治理还需对话或配置中的 `user_id`。

## 核心 ID

`projectId` 为项目英文名；`pipelineId` 以 `p-` 开头；`buildId` 以 `b-` 开头；`elementId`/`tag` 以 `e-` 开头。构建 URL：`https://devops.woa.com/console/pipeline/{projectId}/{pipelineId}/detail/{buildId}`。

## 意图路由

路径均相对 `references/`。只打开当前意图需要的文件。

### 项目管理 — `scripts/project_client.py`

[project-query.md](references/project/project-query.md) · [project-manage.md](references/project/project-manage.md) · [project-models.md](references/project/project-models.md)

编辑前必须先 get 完整详情再提交。列表必须显式分页，规则见 query。

### 流水线运行 — `scripts/pipeline_client.py`

[pipeline-list](references/pipeline/pipeline-list.md) · [build-startinfo](references/pipeline/build-startinfo.md) · [build-start](references/pipeline/build-start.md) · [build-stop](references/pipeline/build-stop.md) · [build-status](references/pipeline/build-status.md) · [build-list](references/pipeline/build-list.md) · [pipeline-update-params](references/pipeline/pipeline-update-params.md) · [error-handling](references/pipeline/error-handling.md)

### 编排生成 — `pipeline_generate_client.py` / `codelib_client.py` / `pipeline_yaml_lint.py`

入口：[pipeline-yaml-patterns.md](references/generate/pipeline-yaml-patterns.md)。按需再读同目录：atom-yaml、atom-detail、store-atom-list、store-atoms-quickref、create-pipeline、save-draft、release-version、get-version、pipeline-yaml-lint、pipeline-yaml-schedules、pipeline-yaml-tapd-trigger、git-auth、codelib-api、ci-runtime-pitfalls。

硬规则见 patterns / save-draft / git-auth。摘要：分轮保存，禁止一次写出编译+勾选+Helm 大稿；lint 0 error 再 YAML `save-draft`，失败一次立刻 JSON 并回拉 YAML；代码库先 `resolve`；未托管拉私有代码时让用户选授权方案，token 进凭据中心，禁止明文和 `set -x`；PAC 不用 `SELF`；定时走 `on.schedules` 并发布；TAPD 走 `on.type: tapd`。

### 日志与诊断

用户只贴 URL 时先查状态。诊断路由、假绿假红、熔断日志见 [build-status.md](references/insight/build-status.md) 与 [api-reference.md](references/log/api-reference.md)。

- 原始日志：`download_log.py`
- Model：`get_pipeline_model.py`
- 状态/递归/卡住：`devops_build_status.py` → [build-status.md](references/insight/build-status.md)
- 历史：`devops_build_history.py` → [build-history.md](references/insight/build-history.md)
- 错误日志：`devops_build_log.py` → [build-log.md](references/insight/build-log.md)
- 对比：`devops_build_diff.py` → [build-diff.md](references/insight/build-diff.md)
- 模式/报告/API：[error-patterns.md](references/insight/error-patterns.md) · [report-template.md](references/insight/report-template.md) · [api-guide.md](references/insight/api-guide.md)

### 流水线制品

`search_build_artifacts.py` / `get_download_url.py` / `get_script_download_url.py` / `get_app_download_url.py`，见 `references/artifact/`。二维码仅 `.apk` `.apks` `.ipa` `.hap`。下载命令见 [artifact-script-download-url.md](references/artifact/artifact-script-download-url.md)。

### bk-repo

`bkrepo_search.py` / `bkrepo_download.py` / `bkrepo_upload.py` / `bkrepo_detail.py`，见 [bkrepo.md](references/bkrepo/bkrepo.md)。与流水线构建制品不是同一套接口。

### 模板 — `scripts/template_client.py`

见 `references/template/`：template-query、template-create-template、template-update、template-crud、template-create-pipeline、template-instance、pipeline-query、template-install。

### 权限治理 — `scripts/auth_client.py`

见 `references/auth/`：auth-query、auth-metadata-validate、auth-insight、auth-admin-manage、auth-authorization。管理员治理，不承接普通成员自助申请/续期/退出。

### Model JSON 解释

无脚本。按需读 `references/interpreter/`：1-json-analysis-playbook、2-common-plugin-and-container-hints、3-classtype-and-enum-catalog。

## 写操作确认

执行前必须展示完整目标、参数和影响，等待明确确认。失败后不要自动重试。

- 项目：`create`、`edit`
- 流水线：`start`、`stop`、`update-params`
- 编排：`save-draft`、`release-version`
- 代码库：`codelib_client.py create`、`credential-create`
- 制品库：`bkrepo_upload.py`（覆盖须 `--overwrite`）
- 模板：`instantiate`、`update-instances`、`create-pipeline`、`delete`、`delete-version`
- 权限：`add-members`、`renewal`、`remove`、`handover`、`remove-from-project`、`clone-permissions`、`reset-pipeline-authorization`

权限移除/交接先 `exit-check`；克隆先 `dryRun=true`。

## 典型调用

用户贴构建 URL 时先查状态，失败再按下一步的 `elementId` 下载日志。ID 从 URL 或上一步输出取，不要猜。

```bash
python3 scripts/devops_build_status.py --url "https://devops.woa.com/console/pipeline/{projectId}/{pipelineId}/detail/{buildId}"
python3 scripts/download_log.py --project-id {projectId} --pipeline-id {pipelineId} --build-id {buildId} --tag {elementId} --output ./step.log
```
