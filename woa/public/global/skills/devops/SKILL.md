---
name: devops
description: 蓝盾（BK-CI，亦称蓝鲸 CI）DevOps 全能助手，覆盖项目查询与管理、流水线查询与构建、编排生成（含 YAML lint、代码库托管、git 凭据、定时/TAPD 触发）、构建日志与故障诊断、构建制品、bk-repo 制品库、流水线模板、Model JSON 解释和管理员权限治理。当用户提及蓝盾、蓝鲸 CI、BK-CI 的项目、创建/编辑项目、普通流水线、构建、部署、CI/CD、YAML 编排、Helm、插件、定时/巡检、TAPD 触发、git 授权、代码库托管、构建日志、失败原因、卡住、制品、bk-repo、流水线模板、蓝盾权限、用户组、授权或流水线代持人时使用。不处理创作流或云桌面取文件。
---

# 蓝盾流水线全能助手

## 环境要求

- Python 3.8+（Windows / macOS / Linux 均可；自动换票的 `login.py` 需要 Windows 系统 Edge）。
- 唯一第三方依赖 `PyYAML`：缺失时首次运行会自动安装到 skill 私有 `lib/` 目录（`pip install --target`），不触碰全局环境，无需任何手动步骤。
- 其余全部为 Python 标准库（`urllib`/`json` 等），零额外安装。

按意图只读 `references/` 下对应文件，禁止一次读完整个目录。路径相对 `references/`，文件名即该目录下的 `<名>.md`。

## 会话初始化与热更新

每次会话首次加载，在当前 skill 根目录执行一次 `python3 ./hot_reload.py`（只检查，不覆盖）。用户明确说「更新 skill / 拉最新版」时改为 `--force`。

必须读 stdout JSON，禁止只看退出码或空输出。`up_to_date`、`skipped` 都不要再提热更新。

`update_available`：先问用户。明确同意前禁止 `--apply` / `--force`，继续用本地 Skill。必须原文展示 `ask`；覆盖本地修改和 ZIP 备份路径写在问题正文，不要只放进选项；禁止缩成「是否安装」或「是否更新」。选项保持简短，避免被界面截断：

1. 安装新版本
2. 先不更新，继续用本地版
3. 以后不再检查更新

选 1 执行 `python3 ./hot_reload.py --apply`。选 3 在 `config.json` 写入 `"hot_reload": false`。`updated` 且 `local_backup` 非空时告知备份路径。`error` 或找不到脚本时只简短提示并继续原需求；同一会话不重试，不手工拼文件。TTL、冷却、开关和覆盖规则见 [热更新](references/hot-reload.md)。

## 鉴权

首选自动换票：用户在自己的终端执行 `python scripts/login.py`（工作目录为含 `SKILL.md` 的 skill 根目录，或用绝对路径）。脚本自动拉起系统 Edge 打开蓝盾控制台，未登录时等用户在窗口里登录，拿到 `bk_ticket` 后自动完成 bkoauth 换票 + 校验 + 续期并回写 `config.json`，无需手动复制任何 token。token 失效时重跑该脚本即可（profile 已保存登录态，多数情况无需再登录）。加 `--headless` 可无窗口运行，但仅当 profile 内票据仍存活时有效。`app_code`/`app_secret`/`user_id` 需先写入 `config.json`。缺 token 或 HTTP 401 时读 [鉴权配置](references/auth-setup.md)。

手动方式兜底：token 从 <https://devops.woa.com/ms/auth/api/user/bkToken/get> 获取。引导必须说明：先访问该链接；仅当返回 401、无权限或未登录时，先登录 <https://devops.woa.com/console/> 再重试。禁止省略该条件，也不要把登录控制台写成固定前置。已写入 `config.json` 的不用重配。

手动配置的三种途径：

1. 用户把 token 发给 AI。不回显；本次 `--access-token`，并用 `scripts/auth_cli.py login --access-token` 写入 `config.json`。之后不再带该参数。这一次命令行参数可能出现在进程列表中。
2. 也可以让用户在自己的终端，进入含 `SKILL.md` 的 skill 根目录再登录。必须写出该目录绝对路径，例如 `cd /实际路径` 后执行 `python3 scripts/auth_cli.py login`。不要加 `--access-token`，输入不回显。token 不进对话、不进进程参数。禁止只给命令不写目录。Agent 不代跑这条无参命令。
3. 自行配置时，必须给出同一绝对路径，以及 `cd /实际路径` 后执行 `cp config.example.json config.json`。这两个文件和 `SKILL.md` 在同一层。禁止只说「复制 config.example.json」。

不要推荐环境变量。缺 token 或 HTTP 401 时读 [鉴权配置](references/auth-setup.md)。权限治理还需对话或配置中的 `user_id`。

## 核心 ID

`projectId` 为项目英文名；`pipelineId` 以 `p-` 开头；`buildId` 以 `b-` 开头；`elementId`/`tag` 以 `e-` 开头。构建 URL：`https://devops.woa.com/console/pipeline/{projectId}/{pipelineId}/detail/{buildId}`。

## API 调用铁律

调用任何蓝盾 v4 OpenAPI 前必须先查 `api/v4-user-apis.md`（端点字典：223 个 v4 用户态接口 + Stream 车道，含方法/路径/中文说明）。禁止猜接口路径、禁止按常见 RESTful 形态拼 URL 试探、禁止假设存在动态发现机制（不存在）。字典未覆盖的应用态 `v4_app_*`、v3 存量或参数详情，按字典末尾「查证入口」上 APIGW 文档中心按 `apiName` 查。

## 意图路由

只打开当前意图需要的文件。

### 项目管理 — `scripts/project_client.py`

`project/`：project-query、project-manage、project-models。编辑前必须先 get 完整详情再提交。列表必须显式分页，规则见 query。

### 流水线运行 — `scripts/pipeline_client.py`

`pipeline/`：pipeline-list、build-startinfo、build-start、build-stop、build-status、build-list、pipeline-update-params、error-handling。

### 编排生成 — `pipeline_generate_client.py` / `codelib_client.py` / `pipeline_yaml_lint.py`

入口 `generate/pipeline-yaml-patterns.md`。同目录按需：atom-yaml、atom-detail、store-atom-list、store-atoms-quickref、create-pipeline、save-draft、release-version、get-version、pipeline-yaml-lint、pipeline-yaml-schedules、pipeline-yaml-tapd-trigger、git-auth、codelib-api、ci-runtime-pitfalls。

硬规则见 patterns / save-draft / git-auth。摘要：分轮保存，禁止一次写出编译+勾选+Helm 大稿；lint 0 error 再 YAML `save-draft`，失败一次立刻 JSON 并回拉 YAML；代码库先 `resolve`；未托管拉私有代码时让用户选授权方案，token 进凭据中心，禁止明文和 `set -x`；PAC 不用 `SELF`；定时走 `on.schedules` 并发布；TAPD 走 `on.type: tapd`。

### 日志与诊断

只贴 URL 时先查状态。

- `download_log.py` 原始日志；`get_pipeline_model.py` Model
- `devops_build_status.py` → `insight/build-status.md`（诊断路由、递归、卡住、假绿假红）
- `devops_build_history.py` → `insight/build-history.md`
- `devops_build_log.py` → `insight/build-log.md`
- `devops_build_diff.py` → `insight/build-diff.md`
- `insight/error-patterns.md`、`report-template.md`、`api-guide.md`；熔断日志见 `log/api-reference.md`

### 流水线制品

`search_build_artifacts.py`、`get_download_url.py`、`get_script_download_url.py`、`get_app_download_url.py`，见 `artifact/`。二维码仅 `.apk` `.apks` `.ipa` `.hap`。下载命令见 `artifact/artifact-script-download-url.md`。

### bk-repo

`bkrepo_search.py`、`bkrepo_download.py`、`bkrepo_upload.py`、`bkrepo_detail.py`，见 `bkrepo/bkrepo.md`。与流水线构建制品不是同一套接口。

### 模板 — `scripts/template_client.py`

`template/`：template-query、template-create-template、template-update、template-crud、template-create-pipeline、template-instance、pipeline-query、template-install。

### 权限治理 — `scripts/auth_client.py`

`auth/`：auth-query、auth-metadata-validate、auth-insight、auth-admin-manage、auth-authorization。管理员治理，不承接普通成员自助申请/续期/退出。

### Model JSON 解释

无脚本。`interpreter/`：1-json-analysis-playbook、2-common-plugin-and-container-hints、3-classtype-and-enum-catalog。

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

贴构建 URL 先查状态，失败再按下一步的 `elementId` 下载日志。ID 从 URL 或上一步输出取，不要猜。

```bash
python3 scripts/devops_build_status.py --url "https://devops.woa.com/console/pipeline/{projectId}/{pipelineId}/detail/{buildId}"
python3 scripts/download_log.py --project-id {projectId} --pipeline-id {pipelineId} --build-id {buildId} --tag {elementId} --output ./step.log
```
