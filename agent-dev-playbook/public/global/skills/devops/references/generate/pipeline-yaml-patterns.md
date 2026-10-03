# 普通流水线 YAML 生成模式

> **借鉴结构，禁止照搬业务 YAML。** 真实流水线里的 `repo-id`、集群、命名空间、chart、镜像仓库、凭据、模块名都是项目私有的，写进 skill 或直接复用会编排出别人的环境。需要看某条现网编排时，用 `get-version --format yaml` 现拉，只抽 stage/job 关系。
>
> 插件 `with` 不要凭记忆填：先 `atom-yaml --atom-code {code}`，字段含义不清再用 `atom-detail --atom-code {code} --version {ver}`。

配套：[`pipeline-yaml-lint.md`](./pipeline-yaml-lint.md) · [`atom-yaml.md`](./atom-yaml.md) · [`store-atoms-quickref.md`](./store-atoms-quickref.md) · [`pipeline-yaml-schedules.md`](./pipeline-yaml-schedules.md) · [`ci-runtime-pitfalls.md`](./ci-runtime-pitfalls.md)

---

## 0. 生成原则

- **会话逐步确认，禁止一次出复杂 flow。** 成熟大流水线是多轮长出来的。第一轮只对齐名称/触发/这一截要做什么，落可手动跑的最小稿；下一 stage、Helm、matrix、通知等用户点头再加。
- **需求维度心里有清单，嘴上分轮问。** 触发 / 构建机 / 本轮步骤 / 插件 / 凭据与代码库 / 后续通知——每轮只推进相关的 1～3 项，不要一次甩访谈清单。
- **有限选项发卡片。** 会话有 `ai-hub-ask-user-input` 时，模式/触发/集群/模块勾选/是否保存必须走该 skill（`single_select` / `multi_select` / `confirm`），禁止 A/B/C 文本列表。发完停住等 `Q:/A:`。仓库 URL、版本号等自由文本仍自然语言。无该 skill 才对话追问。
- 每轮对话：问 **1～3 个相关问题**，或展示/保存**当前全文**（可以很短）。不要一次甩访谈清单，也不要先贴「将来完整版」再回头填空。
- 用户明确说「一次性出完整编排」或已贴可保存的完整规格，才允许本轮写全。
- 只复用「阶段怎么拆、job 谁依赖谁、变量怎么变成 matrix」，业务字段全部占位 `{your_xxx}`。未确认的值不要编造。
- **插件 `with` 必须有 schema 来源**：本轮每个 `uses` 先 `atom-yaml --atom-code {code}`（字段不清再 `atom-detail`）。用户贴了现网 demo 时，对照官方模板核对字段，**禁止凭记忆默写**。
- **改既有稿：忠实改意图**。`get-version` 或用户粘贴的脚本/注释，只改点名处；不要擅自简化或「优化」。语法以本 skill **v3.0 PAC** 为准，**禁止**套用 Stream CI v2.0 语法。
- **骨架带文件头注释**：功能一句话、触发、构建机/集群、依赖的凭据占位。
- 敏感值只写 `${{settings.{your_cred_id}.username}}` / `${{settings.{your_cred_id}.password}}`，禁止把账号密码、token、镜像仓库密钥写进 `script`。
- 代码库先 `codelib_client.py resolve`，命中才写 `repo-id`。
- 每一截：对话展示**当前**完整 YAML → 写完核对（下节）→ `pipeline_yaml_lint.py` → `save-draft --yaml-file`。发布等用户明确要求。

### 写完、lint 前核对（普通流水线）

- [ ] 本轮每个 `uses` 的 `with` 来自 `atom-yaml`（或对照过官方模板）
- [ ] 无明文密钥；敏感值仅 settings / 凭据 ID；脚本无 `set -x` / token 进 argv
- [ ] 代码库已 resolve 或明确占位；自建集群名来自当前用户
- [ ] PAC 代码库触发用 NAME/`repo-id`，不用 SELF
- [ ] 归档路径：根目录文件不用只写 `**/`；跨 job 产物走归档或同 job
- [ ] Windows 脚本不嵌套 pwsh，检查 `$LASTEXITCODE`
- [ ] 未确认的 Helm / notices / recommended-version / concurrency 未提前写入
- [ ] 改既有稿未擅自删改无关脚本
- [ ] lint 0 error 再请用户确认 save

> Stream CI v2.0 制作指南若出现在仓库中：**只借鉴**「需求维度 / schema 先于填参 / 改稿忠实 / 写完核对」等方法，**不要**抄 v2.0 语法。

---

## 1. 意图 → 模式（先路由再写 YAML）

用户怎么说，决定**下一步加哪一截**，而不是套一条写死的「标准部署流水线」一次生成完。

- 跑一段脚本 / 巡检 / 清理 → **模式 A** 单 stage 单 job
- 编译产物再签名/发布 → **模式 B** 多 stage 顺序
- 多系统/多版本一起测 → **模式 C** 静态 matrix
- 按环境（test/prod）分 stage 部署 → **模式 D** 条件 stage
- **勾选多个微服务 / 模块，先编译再按服务 Helm 发布** → **模式 E**（本文重点）
- 将来自动反复跑 → 叠加 `on.schedules`，不是 Agent 定时

本 skill 只生成普通流水线编排。

---

## 2. 模式 E：多服务勾选 → 编译 → 按服务 matrix 部署

### 2.1 分轮确认（禁止一轮问完、禁止一轮写完）

每轮只推进下表的**一行**。用户说「先保存」就 lint + save 当前稿，不要把后面几行提前写进 YAML。

1. 流水线名称 + 手动触发；要不要启动时勾选模块（还没有名单就占位，先能保存）
2. 有哪些服务/模块？（写入 `checkbox.options`）
3. 业务代码仓、是否还有配置仓？（先 resolve 拿 `repo-id`）+ 构建机集群名
4. 编译/推镜像：镜像仓库、凭据 ID（不要明文密码）——本轮只加「编译」stage
5. 用户确认「接下来做部署」之后：chart 名、版本、BCS 集群与命名空间——再加 Helm job / matrix
6. 失败要不要通知触发人（最后加 `notices`，不要默认抄别人的文案）

用户说不清模块列表时，先保存只有手动触发 + 空 checkbox 或单 job 的骨架。

### 2.2 结构（关系可复用，值必须替换）

```
variables:
  services:  checkbox + options=用户给出的模块
  branch:    git-ref + 已 resolve 的 repo-id
  chart_version / namespace / 其它部署参数: vuex-input

stage 编译:
  job_prepare  把勾选结果变成 matrix JSON（::set-variable name=parameters）
  job_build    depend-on prepare；checkout；编译/推镜像；setEnv 镜像 tag
               与 prepare 同机需求时，构建步骤放同一 job，不要跨 job 传 WORKSPACE

stage 部署:
  job_deploy   strategy.matrix: ${{ fromJSON(variables.parameters) }}
               按 matrix.service 调 helm（先 atom-yaml）
```

同 stage 内 `depend-on` 只保证启动顺序，**不共享磁盘**。编译产物要给部署用：同 job 内 `setEnv`，或归档后再拉，或部署 job 只消费镜像仓库里的 tag。运行时坑（归档 glob、gitignore 跨 step、假绿假红、Windows 退出码）见 [ci-runtime-pitfalls.md](./ci-runtime-pitfalls.md)。

### 2.3 变量写法（checkbox ≠ 布尔开关）

`checkbox` + `options` = 启动时多选，运行时 `services` 是**逗号分隔的 id**。`boolean` 才是 true/false 开关。不要把多服务勾选写成 `selector` 单选，除非用户只要部署一个模块。

```yaml
variables:
  services:
    value: ""
    props:
      label: service
      type: checkbox
      options:
        - id: "{your_svc_a}"
          label: "{your_svc_a}"
  branch:
    value: master
    props:
      type: git-ref
      description: 拉取分支
      repo-id: "{your_repo_hash_id}"
  chart_version:
    value: "{your_chart_version}"
    props:
      type: vuex-input
      required: true
  namespace:
    value: "{your_namespace}"
    props:
      type: vuex-input
      required: true
```

`options` 必须来自用户模块名单。`description` / `label` 只能写在 `props` 里。

### 2.4 勾选 → 动态 matrix

前置 step 把逗号列表收成 `{"service":["a","b"]}`，写入流水线变量 `parameters`，部署 job 再 `fromJSON`。不要把模块名写死在 `strategy.matrix.service: [a, b]` 里（那样启动勾选会失效）。

```yaml
# job_prepare 某 step（Linux 自建机可用 linuxScript@1.* 或 run@1.*）
# 先 atom-yaml --atom-code linuxScript 或 run
script: |
  # 将 ${services}（逗号分隔）转为 {"service":["a","b"]}
  echo "::set-variable name=parameters::{...json...}"

# job_deploy
strategy:
  matrix: "${{ fromJSON(variables.parameters) }}"
  fast-kill: false
  max-parallel: 20
```

静态多维矩阵、include/exclude 见 schema / 现网 `get-version` 样例。前置 job `::set-output` 再 `fromJSON(jobs.x.steps.y.outputs.parameters)` 是另一条路；勾选启动参数用 `::set-variable` + `variables.parameters` 更直接。

### 2.5 自建 Linux 集群 `runs-on`

```yaml
resources:
  pools:
    - from: "bkdevops@{your_pool_name}"
      name: "{your_pool_name}"

# job
runs-on:
  self-hosted: true
  pool-name: "{your_pool_name}"
  concurrency-limit-per-node: 1
  agent-selector:
    - linux
```

`{your_pool_name}` 问用户，不要抄其它项目的集群名。Docker 公共集群用 `pool-name: docker`，不要无故加 `self-hosted`。

### 2.6 拉代码

原生 `checkout`（不是商店插件）。两个仓时：业务仓 + `localPath` 拉配置仓。`refName` 绑 `git-ref` 变量。

### 2.7 Helm 步骤怎么生成

1. `atom-yaml --atom-code helm`（当前主版本在 `35.*`，以接口返回的 `uses: helm@…` 为准，不要写死旧大版本）
2. 按用户意图选 `op_type`，**只填该操作声明必填的字段**（模板注释里「当 op_type=… 时必选」）
3. 常见组合（仍要对照最新模板）：
   - 先判断有没有 release：`check_release_exist`，下游 `if` 看输出 `is_release_exist`
   - 日常发布：`update_or_create`
   - 命名空间形态：`{your_cluster_id}/{your_namespace}`，两边都问用户
   - 镜像 tag、模块开关用 `cmd_flags` 的 `--set`，值来自变量 / `matrix.service` / 上游 `setEnv`，不要写死 tag
4. `customize_values_content`、chart 名、自定义 release 名全部占位后确认

`atom-detail --atom-code helm --version 35.*` 可查 `op_type` 枚举与 `output.is_release_exist`。

### 2.8 通知与并发

用户要失败通知再写 `notices`。不要复制别人的 `recommended-version` / `concurrency` 数字。不做版本号管理就不要写 `recommended-version`。

---

## 3. 模式 A–D（骨架）

拆法：单任务；编译→加工→发布；静态 matrix；按环境条件 stage。构建机用本节 `self-hosted` / docker 公共池写法（问用户集群名，勿抄其它项目）。

---

## 4. 保存前

占位未替换不要把这一截 `save-draft`。lint 0 error 后再保存。Helm `with` 与官方模板字段名必须一致（例如 `charts_input_for_update_create` 不要自造 `chart`）。骨架阶段允许只有 `name` / `on.manual` / 一个空 job 或占位 step，不要为了「看起来完整」补上未确认的 Helm。

清理 workspace / retry 时不要删自举工具目录；先提升产物再清 staging。
