---
name: dec
description: >
  Dec 个人 AI 知识仓库代理。支持跨项目复用 Skills、Rules、MCP 配置。
  推荐用户保存新创建的资产、搜索已有资产、或把当前项目中已经验证过的能力沉淀进 Dec。
---

# Dec 代理

Dec 是个人 AI 知识仓库，用来积累和复用 Skills、Rules、MCP。用户交互以 **Dec Console** 为第一入口；Agent 走 **`dec-mcp`**（零业务知识的 stdio 壳）经本机唯一 Console 网关调用当前连接的 `dec-server`。工具名 / schema 来自 `~/.dec/run/agent-tools.json`（Console 对齐运行时后写出），不要假设它们编译在 `dec-mcp` 里。不要发明已下线的用户面子命令（旧的 list / search / config / pull CLI），也不要再引导用户运行终端 TUI。

项目里由 Dec pull 出来的 IDE 配置不等于「禁止提交」。像 `.cursor/`、`.claude/`、`.codex/`、`.codebuddy/`、`.mcp.json` 这类项目级输出，如果是托管资产生成的结果，通常可以按仓库约定单独提交。敏感值放 `.dec/vars.yaml`、`~/.dec/local/vars.yaml` 或用户本机配置，不要写回这些输出文件。

消费声明只有一处：`.dec/config.yaml`（项目）与 `~/.dec/config.yaml`（本机）的 `requires` map，项目名 → `latest` / `v*`（官方 `registry` 分支）/ `vault`（设置里的个人私仓）。密钥走 Bitwarden。改官方安装物不要 `dec_push`，用 Console 项目下级页「本地覆写」+ `dec_propose_upstream`。

## 何时使用

### 主动建议用户的场景

1. **新项目需要接入 Dec**
   - Console **引导 / 项目** 初始化项目（可选套用 vault 同名 project）；Agent 用 `dec_init_project`（必填 `project_root`）
   - 模板有 `{{VAR_NAME}}` 时，在 Console 项目设置里编辑 `.dec/vars.yaml`
   - Console 项目页「订阅」勾选并保存；Agent 用 `dec_set_requires` 后 `dec_pull`（`plane=local`，带 `project_root`）
   - pull 后若仓库跟踪 Dec 托管的 IDE 输出，询问是否单独 commit

2. **用户要找以前做过的工具/配置**
   - Agent：`dec_list_assets`（`plane=project|user|both`）看已订阅项目与成员
   - 用户：Console 项目 / Global 资产页浏览/搜索

3. **用户改了已拉取的官方资产**
   - 改动应出现在 `.dec/overrides/`；不要改 `.dec/cache/`（重装会丢）
   - Agent：`dec_propose_upstream`（`origin_repo`、`diff`、`mode=auto|pr|issue`）
   - **禁止** `dec_push` 官方路径进私仓

4. **新增个人资产**
   - 把当前项目里已验证的能力抽出来复用：优先 `dec-extract-asset`
   - 个人 Git 写作者目录或私仓，人提交私仓
   - Console **同步** push 仅个人 Git + 密钥；Agent：`dec_push`

5. **从当前项目沉淀已有能力**
   - 用 `dec-extract-asset`
   - 官方产品资产进提供方源仓 `DecAssets/`，打 `v*` 后 CI `dec-registry publish-provides`
   - 个人资产进私仓，不要写进官方 registry

6. **删除远端或本机托管资产**
   - Console **删除** 页；Agent 先 `dec_list_delete_candidates`，再 `dec_delete`（`confirmed=true`，一次一个平面）
   - 远端密钥删除是软删，进 Bitwarden 回收站，可在官方客户端恢复

7. **刚 pull 完**
   - 检查 `.cursor/`、`.claude/`、`.codex/`、`.codebuddy/`、`.mcp.json` 等项目级 IDE 输出
   - 适合单独提交，不要和业务代码混在一笔里
   - `.dec/vars.yaml`、本机配置、密钥类内容不要因为这条规则自动纳入

8. **命令尾部出现「Dec 资产已落后远端」**
   - 任意 `dec` 启动（pull/push 等同步类除外）可能在 stderr 打 freshness 提示
   - 先看 cache 有没有未推本地改动：有则先 push 或 stash，再 pull
   - Console **同步** pull，或 Agent `dec_pull`
   - 再按第 7 条处理 IDE 输出 diff
   - 关键路径可先搁置；临时关闭：`DEC_FRESHNESS_CHECK=off`

## Agent MCP 快速参考

`dec-mcp` 不绑定某个仓库。先 `dec_console_status` / `dec_list_managed_projects`，本地平面操作带上 `project_root`。目标是 Console **当前连接**的设备（本机或 SSH 远端）。

当前平面用 `plane=local`（项目内 IDE 目录）或 `plane=global`（本机）。用户平面在 Console 的 Global 资产里管理。

| 目的 | 工具 |
|------|------|
| Console / 当前连接 | `dec_console_status`；`dec_list_connections` / `dec_connect` |
| 受管项目 / 设备 | `dec_list_managed_projects`、`dec_list_managed_devices`、`dec_register_managed_project` |
| 新建本地资产 | `dec_create_local_asset` |
| 状态 | `dec_status`（local 需 `project_root`） |
| 已订阅项目 / 成员 | `dec_list_assets` |
| 可订阅项目（私仓 ∪ 官方已发布） | `dec_list_subscription_candidates` |
| 改订阅 | `dec_set_requires`（整表覆盖，不支持 both；改完通常再 `dec_pull`） |
| 拉取并渲染 | `dec_pull` |
| 个人 Git / 密钥推回 | `dec_push`；官方路径禁止 |
| 提交官方本地覆写 | `dec_propose_upstream` |
| 私密资产元数据 | `dec_list_secrets`（绝不返回正文/密钥；Console 在同步页「密钥清单」） |
| 删除候选 / 删除 | `dec_list_delete_candidates` / `dec_delete`（Console 在删除页） |
| 置备远端设备 | `dec_provision_remote`（Linux/macOS；首次置备必须 `confirmed=true`） |
| 连仓库 | `dec_connect_repo` |
| 初始化项目 | `dec_init_project`（`project_root` 必填） |

env 注入给子进程用独立程序 `dec-exec`，不经过 `dec-server`、不是用户面入口。

## 用户 Console 入口

| 操作 | 页面 |
|------|------|
| 连接仓库 / 全局 IDE / Bitwarden / 本机 vars | **设置** |
| 项目初始化 | **引导 / 项目** |
| 勾选订阅（官方 ∪ 私仓） | **项目 / Global 资产** |
| 项目变量 | **项目**（本机平面无项目配置） |
| 已订阅项目的更新预览与安装 | **更新** |
| 个人 Git / 密钥写回 | **项目 / Global 资产** |
| Console 自动检查 / 手动安装更新 | **设置**（连接 / 解锁页复用；MCP 无更新工具） |
| 远端设备探测与置备 | **连接** |

## 配置要点

项目：`<project>/.dec/config.yaml`。本机 global：`~/.dec/config.yaml`。两处都只有一张 `requires` 表。

```yaml
version: v2
project_name: my-app   # 作者身份：本仓创作私仓里的 my-app，不进 requires
ides:
  - cursor
requires:
  relkit: latest       # 官方注册表，跟随最新已发布 tag
  my-notes: vault      # 个人私仓，跟随 HEAD
```

- 旧的 `enabled_bundles` / `enabled_projects` 读到即折叠为 `requires{<名>: vault}`，不再写回；更早的 `available` / `enabled` 已移除
- `ides` 不写则继承 Settings 全局列表
- pull 会清掉不在本次启用目标集里的 cache / IDE 托管副本（secrets/SSH 仅在远端对照成功时 prune）
- Claude / Codex 分别使用 `.claude/`、`.codex/`（项目级与用户级同名目录）

## 占位符变量

模板里的 `{{VAR_NAME}}` 在 pull 时替换。须大写字母开头，只含大写字母、数字、下划线。

优先级：

1. `.dec/vars.yaml` 的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 的 `vars`
3. `~/.dec/local/vars.yaml` 的 `vars`

Settings 可编辑本机 vars；Project 页编辑项目 vars。缺失变量会提示并保留占位符。

## 新增资产（作者源由 provides 决定）

新项目通过 Console 项目页从作者目录 `skills/`、`commands/`、`rules/`、`mcp/`
选择资产，Console 派生并写入 `provides`；`.dec/` 与各 IDE 目录都是状态或
渲染副本，不能被选择，配置校验也会拒绝。

作者目录在新项目中默认位于 `DecAssets/`，项目可用 `.dec/config.yaml` 的
`provides_root` 整组挪动。基准点只改本地路径，派生的 vault 目标不变；它不能指向
`.dec/`、`.cursor/` 等点目录。已有 `provides` 但没有该字段的旧项目仍按仓库根解释。

1. 项目已初始化（Home / `dec_init_project`）
2. 在产品仓规范作者目录中创建作者文件
3. 在项目页勾选扫描到的资产；source、type、name 自动派生，只选择 visibility 和 plane：

```text
source: skills/my-skill
visibility: public
plane: local
type: skill
name: my-skill
```

目标 vault 路径由 Dec 派生。`provides` 只登记 Git 资产：secrets 由 `.secrets/<项目>`
的 SyncTarget 规则同步，声明它反而写出第二套规则，配置校验会拒绝 `type: secret`。
4. 有占位符则补 vars
5. Console **同步**；旧项目没有 provides 时才继续使用 cache push

不要把 cache 或 IDE 目录当新增来源。项目级托管输出可以单独提交，只要敏感值已抽到 vars。

## 资产格式

- **Skill**：含 `SKILL.md` 的目录
- **Rule**：单个 `.mdc`
- **MCP**：单个 server JSON 片段（`command` 必填）。部署到 Cursor / CodeBuddy / Claude 写 JSON；Codex 写入 `.codex/config.toml` 的 `[mcp_servers.<name>]`

## 故障排查

- 仓库未连接：Settings，或 `dec_connect_repo` / `dec_status`
- 找不到资产：`dec_list_assets`；确认项目已订阅且成员名单包含它
- 拉取失败：`dec_status`；检查 `requires`；补齐未定义的 `{{VAR}}`
- 没有订阅任何项目：Console 项目 / Global 资产页勾选保存，或 `dec_set_requires`

## 修改资产的正确流程

1. 改 `provides[].source` 指向的作者文件
2. 在同步页预览后同步
3. 其他项目 / 平面再 pull

刚 pull 完且仓库跟踪 IDE 输出：先看 diff，再问是否单独 commit。

**禁止**直接操作 `~/.dec/repo.git` 或手改托管 IDE 副本。Agent 用 MCP；真人用 Console。
