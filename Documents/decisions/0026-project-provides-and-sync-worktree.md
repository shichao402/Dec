# 0026 — 项目作者源映射与可恢复同步工作副本

- **状态**：已接受
- **日期**：2026-09-14
- **关联**：0016（四象限）、0017（本地布局）、0023（门面能力）

## 问题

Dec 目前把 `.dec/cache/` 同时当作 Pull 缓存与 Push 读取源。对在产品仓中随代码共同
开发的 Skill / Rule / MCP，这会让可删除缓存反而成为作者源，并且 Console 只表达
`requires`，无法表达本项目提供哪些资产。Push 的临时 Git worktree 遇冲突会 abort
并删除，也无法让用户在真实工作副本中解决冲突。

## 决策

### 1. `provides` 是唯一作者源映射

工作区 `.dec/config.yaml` 可声明 `provides`。每项包含 `source`、`visibility`、
`plane`、`type`、`name`；`source` 是相对项目根的路径，位置不写死。目标路径由
`project_name` 与其余字段统一派生，配置不得再写一份目标路径。

未声明的仓库文件不是 Dec 作者源。声明存在时，公开资产 Push 不再读取
`.dec/cache/`；cache 只保留 Pull 安装缓存语义。映射必须双射且路径不得逃逸或重叠。

`global` 与 `local` 使用同一规则。`enabled_projects` 只决定安装哪些项目的 global
象限，不复制 `provides`。

`source` 必须落在作者目录 `<provides_root>/{skills,commands,rules,mcp}/` 下，且与
`type`/`name` 派生的文件名一致——写死一条路径就等于允许第二种表达。`provides_root`
在 `.dec/config.yaml` 中声明，新项目默认 `DecAssets`，它**只**平移本地落点，派生的
vault target 完全不受影响；换基准点不能改变远端布局。兼容期内，已有 `provides`
却没有该字段的旧配置仍按仓库根解释，避免升级凭空改变既有 source。

基准点的每一段都不得以 `.` 开头。`.dec/` 是 Dec 自己写的状态，IDE 目录是 pull 的
渲染目标，`.secrets/` 是密钥落地点；把作者根指进任何一个，都会让「人写的」与
「机器写的」重新同处一棵树，正是本 ADR 要消除的歧义。

### 2. Secret 不进 `provides`

`provides` 只登记 Git 资产。`.secrets/<project>` 整树到 Bitwarden 的映射早已由
SyncTarget 规则唯一决定：同步根与远端地址从项目名和平面派生，Note 名就是相对同步根
的路径，push 递归扫描整棵树。再在 `provides` 里逐条声明 secret，等于对同一件事写
第二套规则。

因此配置校验直接拒绝 `type: secret`，候选扫描也不产出 secret。项目同步照常带
secrets，但依据是 SyncTarget 规则而非登记内容；正文没有三方合并，自动模式只同步
公开资产并提示 secret 需要显式选择 Pull 或 Push。

逐条声明还会给出错误信息：派生规则取路径末段作名称，只有文件正好位于同步根下才与
真实 Note 名巧合一致。嵌套路径 `.secrets/<p>/.env/app.env` 的真实 Note 名是
`.env/app.env`，逐条声明会把它显示成一个不存在的地址。

### 3. Git 同步保留工作副本

每个提供方项目使用 `.dec/sync/vault/` 持久 Git worktree。自动同步使用系统 Git：

1. 将 `provides` 的公开 source 导入 worktree 并提交本地快照；
2. fetch 并 merge 远端；
3. 无冲突时将合并结果回写对应 source；
4. push，并刷新安装缓存；
5. 有冲突时保留 `MERGE_HEAD` 与文件，等待继续或显式放弃。

Dec 不实现自己的文本合并算法。仅 Push 遇远端领先时拒绝覆盖。工作副本与状态是派生
运行数据，不是第三份作者源。

### 4. Console 是规则与同步的人机门面

“我提供的”只在各项目页管理；Global 页只管理当前设备引用的 Global 资产。
“同步”进入二级页，先展示
本地 source 与远端目标、时间和动作，再执行自动 / Pull / Push。文本可用内置简单
diff 或标准 Git diff/merge 工具。“我提供的”分区先列出扫描到的候选供勾选，人不必
手写来源路径。候选只扫描作者根下的 `skills/`、`commands/`、`rules/`、`mcp/`；
`.dec/` 状态目录和各 IDE 渲染目录既不扫描，配置校验也禁止引用，避免安装产物被
反向当成作者源。项目页可改作者根，改动会让已登记来源整体平移。
Secret 只在同步页以只读清单呈现。

Console 与 MCP 调用同一 `internal/app` 编排。不得在前端、MCP 或产品仓脚本中复制
映射、合并或目标派生规则。

## 被否方案

**把 `.dec/cache` 升格为作者源。** cache 会被 Pull 与 layout 升级清理，不能与产品
代码一起审阅和提交。

**在 `.dec` 下再放一棵作者正文。** 会与产品仓原始文件形成双 SSOT。

**把作者目录整体搬进 `.dec/assets/`，与 `cache/`、`config.yaml` 并列。** 理由看似
成立——布局既然完全由 Dec 规定，就该住进 Dec 的命名空间。否决的原因是所有权而非
格式：`.dec/` 下的内容由 Dec 写，作者源由人写，在项目仓里审阅和提交。真搬进去，
`.dec/assets/skills/x/SKILL.md` 与 `.dec/cache/<p>/public/local/skills/x/SKILL.md`
肉眼几乎无法区分，而「误改缓存副本」正是本 ADR 的起因。`provides_root` 解决了
「Dec 在每个项目根占四个目录名」的真实顾虑，代价远小于放弃这条隔离。

**把作者目录硬编码为仓库根的 `skills/`。** `rules/`、`mcp/` 这类通名极易与业务目录
撞名，项目必须能挪走整组作者目录。但可配置的只有基准点一个，四个目录名与派生规则
仍是唯一的——否则就是每个项目一套布局。

**自写逐行合并器。** Git 已提供提交、三方 merge、冲突索引和外部工具协议，自写会
产生第二套语义。

**Secret 复用 Git 冲突标记。** 会把敏感正文写入 worktree、日志或 diff，违反
Secrets 边界。
