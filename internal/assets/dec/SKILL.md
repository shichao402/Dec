---
name: dec
description: >
  Dec 个人 AI 知识仓库代理。支持跨项目复用 Skills、Rules、MCP 配置。
  推荐用户保存新创建的资产、搜索已有资产、或把当前项目中已经验证过的能力沉淀进 Dec。
---

# Dec 代理

Dec 是个人 AI 知识仓库，用来积累和复用 Skills、Rules、MCP。用户交互以 **TUI** 为第一入口（无参运行 `dec`）；Agent 走 **`dec-mcp`** 调本机 `dec-server`。不要发明已下线的用户面子命令（旧的 list / search / config / pull CLI）。

项目里由 Dec pull 出来的 IDE 配置不等于「禁止提交」。像 `.cursor/`、`.claude/`、`.codex/`、`.codebuddy/`、`.mcp.json` 这类项目级输出，如果是托管资产生成的结果，通常可以按仓库约定单独提交。敏感值放 `.dec/vars.yaml`、`~/.dec/local/vars.yaml` 或用户本机配置，不要写回这些输出文件。

## 何时使用

### 主动建议用户的场景

1. **新项目需要接入 Dec**
   - TUI **Home** 初始化项目（可选套用 vault 同名 project）；Agent 用 `dec_init_project`
   - 模板有 `{{VAR_NAME}}` 时，到 TUI **Project** 页按 `e` 编辑 `.dec/vars.yaml`
   - TUI **Bundles** 勾选并保存；Agent 用 `dec_set_assets` 后 `dec_pull`（`plane=project`）
   - pull 后若仓库跟踪 Dec 托管的 IDE 输出，询问是否单独 commit

2. **用户要找以前做过的工具/配置**
   - Agent：`dec_list_assets`（`plane=project|user|both`）看已启用 bundle 与成员
   - 用户：TUI **Bundles** 浏览/搜索

3. **用户改了已拉取的资产**
   - 只改 `.dec/cache/`（项目）或 `~/.dec/cache/`（用户平面）
   - TUI **Run** 页 push；Agent 用 `dec_push`（对应 `plane`）
   - **禁止**手改其他 IDE 目录里的同名副本

4. **新增资产**
   - 把当前项目里已验证的能力抽出来复用：优先 `dec-extract-asset`
   - 否则在对应平面的 cache 下写内容，并确保目标 bundle 已启用
   - TUI **Run** push；Agent：`dec_push`

5. **从当前项目沉淀已有能力**
   - 用 `dec-extract-asset`；结果必须落到 cache，而不是只留在 IDE 目录
   - 完成后 `dec_push` / Run 页 push

6. **删除远端或本机托管资产**
   - TUI **Remote** / **Run**；Agent 先 `dec_list_delete_candidates`，再 `dec_delete`（`confirmed=true`，一次一个平面）

7. **刚 pull 完**
   - 检查 `.cursor/`、`.claude/`、`.codex/`、`.codebuddy/`、`.mcp.json` 等项目级 IDE 输出
   - 适合单独提交，不要和业务代码混在一笔里
   - `.dec/vars.yaml`、本机配置、密钥类内容不要因为这条规则自动纳入

8. **命令尾部出现「Dec 资产已落后远端」**
   - 任意 `dec` 启动（pull/push 等同步类除外）可能在 stderr 打 freshness 提示
   - 先看 cache 有没有未推本地改动：有则先 push 或 stash，再 pull
   - TUI **Run** pull，或 Agent `dec_pull`
   - 再按第 7 条处理 IDE 输出 diff
   - 关键路径可先搁置；临时关闭：`DEC_FRESHNESS_CHECK=off`

## Agent MCP 快速参考

当前平面用 `plane=project`（项目内 IDE 目录）或 `plane=user`（`~` 用户级 IDE 目录）。`dec --user` 对应 user 平面。

| 目的 | 工具 |
|------|------|
| 状态 | `dec_status` |
| 已启用 bundle / 成员 | `dec_list_assets` |
| 改启用列表 | `dec_set_assets`（不支持 both；改完通常再 `dec_pull`） |
| 拉取并渲染 | `dec_pull` |
| 推回远端 | `dec_push`；先可用 `dec_preview_push` |
| 私密资产元数据 | `dec_list_secrets`（绝不返回正文/密钥） |
| 删除候选 / 删除 | `dec_list_delete_candidates` / `dec_delete` |
| 置备远端设备 | `dec_provision_remote`（Linux/macOS；首次置备必须 `confirmed=true`） |
| 连仓库 | `dec_connect_repo` |
| 初始化项目 | `dec_init_project` |

env 注入给子进程用独立程序 `dec-exec`，不经过 `dec-server`、不是用户面入口。

## 用户 TUI 入口

| 操作 | 页面 |
|------|------|
| 连接仓库 / 全局 IDE / Bitwarden / 本机 vars | **Settings** |
| 项目初始化 | **Home** |
| 勾选 bundle | **Bundles**（`dec --user` 的 Bundles 管用户平面启用） |
| 项目变量 | **Project**（用户平面无此页） |
| pull / push / remove / 自更新 `u` | **Run** |
| 远端浏览、登记、删除 | **Remote** |

## 配置要点

项目：`<project>/.dec/config.yaml` 的 `enabled_bundles`。用户平面：`~/.dec/config.yaml` 的 `enabled_bundles`（`scope: user`）。成员随 bundle 拉取，不能单资产启用。

```yaml
version: v2
project_name: my-app
ides:                 # 可选；覆盖全局 IDE
  - cursor
enabled_bundles:
  - my-vault
```

- 早期 `available` / `enabled` 已移除；读到旧配置会迁移
- `ides` 不写则继承 Settings 全局列表
- pull 会清掉不在本次启用目标集里的 cache / IDE 托管副本（secrets/SSH 仅在远端对照成功时 prune）
- Claude / Codex 项目级统一 `.claude/`、`.codex/`；用户级仍区分 `~/.claude-internal`、`~/.codex-internal`

## 占位符变量

模板里的 `{{VAR_NAME}}` 在 pull 时替换。须大写字母开头，只含大写字母、数字、下划线。

优先级：

1. `.dec/vars.yaml` 的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 的 `vars`
3. `~/.dec/local/vars.yaml` 的 `vars`

Settings 可编辑本机 vars；Project 页编辑项目 vars。缺失变量会提示并保留占位符。

## 新增资产（cache，不是 IDE 目录）

推送源是 cache，不是 `.cursor/` 等渲染副本。

1. 项目已初始化（Home / `dec_init_project`）
2. 目标 bundle 在对应平面 `enabled_bundles` 中
3. cache 里登记 bundle 成员并写文件：

```text
.dec/cache/<vault>/skills/<name>/SKILL.md
.dec/cache/<vault>/rules/<name>.mdc
.dec/cache/<vault>/mcp/<name>.json
```

用户平面把 `.dec/cache` 换成 `~/.dec/cache`。
4. 有占位符则补 vars
5. Run 页或 `dec_push`

不要把 IDE 目录当新增来源。项目级托管输出可以单独提交，只要敏感值已抽到 vars。

## 资产格式

- **Skill**：含 `SKILL.md` 的目录
- **Rule**：单个 `.mdc`
- **MCP**：单个 server JSON 片段（`command` 必填）。部署到 Cursor / CodeBuddy / Claude 写 JSON；Codex 写入 `.codex/config.toml` 的 `[mcp_servers.<name>]`

## 故障排查

- 仓库未连接：Settings，或 `dec_connect_repo` / `dec_status`
- 找不到资产：`dec_list_assets`；确认 bundle 已启用且成员名单包含它
- 拉取失败：`dec_status`；检查 `enabled_bundles`；补齐未定义的 `{{VAR}}`
- 没有启用任何 bundle：Bundles 勾选保存，或 `dec_set_assets`

## 修改资产的正确流程

1. 改对应平面 cache
2. push
3. 其他项目 / 平面再 pull

刚 pull 完且仓库跟踪 IDE 输出：先看 diff，再问是否单独 commit。

**禁止**直接操作 `~/.dec/repo.git` 或手改托管 IDE 副本。Agent 用 MCP；真人用 TUI。
