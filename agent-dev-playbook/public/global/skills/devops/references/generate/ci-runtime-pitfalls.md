# 编排运行时通用坑

蓝盾 YAML 能稳定复现的契约，生成或改编排时必守。外部 SCM 工作区规则、产品目录约定不写在这里。

配套：[`pipeline-yaml-patterns.md`](./pipeline-yaml-patterns.md) · [`git-auth.md`](./git-auth.md)

---

## 1. 凭据不要进日志

- token / 密码只进凭据中心，编排里用 `${{settings.<id>.password}}` 或凭据 ID 字段。
- **禁止**把密钥拼进命令行参数、URL query、`echo`、`Write-Output`。
- 脚本里不要开 `set -x` / `set -ex` / `Set-PSDebug -Trace`；这些会把整条命令（含密钥）打进构建日志。排障用显式 `Write-Host` / `echo` 打非敏感状态。
- Git 凭据不要填成另一套 SCM 的账号。工蜂走代码库绑定或 Git credential；其它系统各自用自己的凭据。

创建流水线前确认当前项目具备要用的插件和代码库能力；没有就换项目或先托管，不要假设每个蓝盾项目能力相同。

---

## 2. 工作区与产物

同 job 内 step 共享磁盘；**跨 job / 跨 stage 不共享**。`depend-on` 只保证启动顺序。

| 坑 | 正确做法 |
|----|----------|
| 归档 glob `**/foo.zip` 匹配不到工作区**根目录**的 `foo.zip` | 根文件写精确路径（如 `foo.zip`），或根路径与 `**/` 各写一条；上传前先 `ls` 校验 |
| 未跟踪或 `.gitignore` 里的文件，下一 job 可能消失 | 跨 job 传文件必须归档再拉，或打包与发布放**同一 job** |
| 清理 staging 时把刚打好的安装包删掉 | 先把产物提升/复制到稳定目录，再清临时目录 |
| 失败重试或「重置 workspace」删掉自举工具（如 `.ci-tools/`） | 清理范围不要包含本 job 重试还要用的目录 |

普通流水线跨 job 传文件：上游归档插件 → 下游拉取，`pipeLineId` 用 `${{ci.pipeline_id}}`。创作流同理，插件以 `atom-yaml` 为准。

---

## 3. Windows 脚本

- **禁止嵌套** `pwsh -File` / `powershell.exe` 再调另一份脚本：子进程退出码常被吞掉，流水线会假绿。
- 调原生 exe 后必须看 `$LASTEXITCODE`（不要只看 PowerShell 自己的 `$?`）。
- 日志走 `Write-Host`；函数不要把普通输出和返回值混在 stdout，否则下游会把日志当路径。
- 工具吃路径不稳时优先 **UTF-8 + 相对路径**，避免绝对路径切片和 argv 编码把中文/空格路径打坏。
- Windows 节点用 `shell: pwsh`，不要写 bash heredoc。

---

## 4. 代码库触发

- **普通流水线** PAC：`on.push` / `mr` / `tag` 的仓库绑定用 `NAME`（已托管别名）或明确 `repo-id`，不要用 `SELF`。SELF 在未开 PAC / 无绑定上下文时 push 不触发。写完用一次真实 push 验证。
- **创作流**：Git 事件触发必须先 `codelib resolve` 并关联；构建机本地 git 登录收不到 webhook。

---

## 5. 流水线颜色 ≠ 外部结果

发布/提交类步骤要在脚本里做独立校验（制品是否还在、远端是否真有那次提交）。

- **假绿**：步骤 exit 0，外部系统没更新。
- **假红**：外部已提交/已上传，后置校验或文案解析失败，流水线标红。此时不要当「什么都没发出去」再发一次。

隔离缓存：下载器/打包器缓存按 build 隔离；只对明确可恢复错误做有界重试。不要对所有失败自动重试。
