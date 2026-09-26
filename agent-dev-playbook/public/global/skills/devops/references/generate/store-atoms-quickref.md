# 普通流水线高频插件速查

生成 `steps` 时：**先本表认 atomCode，再 `atom-yaml` 拿官方 `uses`+`with` 骨架**，不要把某条业务流水线的 `with` 整段粘过来。字段随插件版本会变（Helm 尤其如此）。

```
list-atoms --project-id {projectId} --keyword {关键词}
  → atom-yaml --atom-code {code}          # 首选
  → atom-detail --atom-code {code} --version {ver}   # 字段含义 / 枚举
```

`list-atoms` 必须带项目英文名。Helm 查详情时版本用接口里的大版本，例如 `--version 35.*`，不要默认 `1.*`。

---

## `run@1.*` / `linuxScript@1.*`

- **新编排优先 `run@1.*`**（跨平台）。用户已有 Linux 自建集群稿、或明确要 Bash 插件时，可用 `linuxScript@1.*`。
- 生成前：`atom-yaml --atom-code run` 或 `linuxScript`。linuxScript 精简模板往往只有 `uses`/`name`，脚本写在 `with.script`。
- 同 job 传变量：`setEnv KEY VALUE` 或 `echo "::set-variable name=KEY::VALUE"`；给 step 加 `id` 后用 `::set-output`，下游 `${{ steps.id.outputs.KEY }}`。
- 勾选服务转 matrix：把逗号分隔的 checkbox 值收成 JSON，`::set-variable name=parameters::...`，见 [pipeline-yaml-patterns.md](./pipeline-yaml-patterns.md) 模式 E。

---

## `helm@35.*`（以 atom-yaml 返回的 uses 为准）

商店名「HELM功能插件」。**每次生成前现拉模板**，不要凭记忆列出全部 30+ 字段。

常用 `op_type`（`atom-detail` 枚举）：

- `check_release_exist` — 判断 release 是否存在，输出 `is_release_exist`
- `update_or_create` — 有则更新、无则创建（日常发布）
- `create` / `update` / `delete` / `diff` / `rollout_stateful` / `add_repo` / `push_chart`

生成规则：

1. `atom-yaml --atom-code helm`，按注释只保留当前 `op_type` 需要的 key。
2. 集群与命名空间问用户，写成 `{cluster}/{namespace}`，对应 `namespaces_input_*`。
3. chart 名、chart 版本、release 名、是否自定义名称，全部占位。
4. `cmd_flags` 是 JSON 字符串，用 `--set` 注入变量（镜像仓库、tag、模块开关）。tag 用上游 `setEnv` 的值，不要写死时间戳样例。
5. 需要等就绪时再开 `wait` / `timeout`，超时单位以模板为准。
6. 「先检查再创建」：`check_release_exist` 之后 `if` 匹配 `is_release_exist: "false"` 再跑创建类 `op_type`。输出参数名以 `atom-detail` 的 `props.output` 为准（当前有 `is_release_exist`）。

禁止：把其它项目的 BCS 集群 ID、chart 名、values 原文、镜像仓库账号拷进新编排。

文档：[研发商店 helm](https://devops.woa.com/console/store/atomStore/detail/atom/helm)

---

## 原生 `checkout`（不是商店插件）

```yaml
- checkout:
    repo-id: "{your_repo_hash_id}"
  name: 拉取代码
  with:
    refName: "${branch}"
```

第二仓加 `localPath`。`repo-id` 必须 `codelib_client.py resolve` 命中后再写。

---

## 其它

通知、归档、子流水线等：`list-atoms --keyword` 后走 atom-yaml。不要默认塞入桌面会话 / 本机 OpenClaw 类专用插件；普通流水线用集群与商店通用插件即可。

归档路径：工作区**根目录**文件不要只写 `**/foo`（匹配不到）；用精确文件名，或根路径与 `**/` 各一条。跨 job 传文件必须归档，gitignore / 未跟踪文件不会自动带到下一 job。细则见 [ci-runtime-pitfalls.md](./ci-runtime-pitfalls.md)。
