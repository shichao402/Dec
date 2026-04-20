---
name: dec
description: >
  Dec 个人 AI 知识仓库代理。支持跨项目复用 Skills、Rules、MCP 配置。
  推荐用户保存新创建的资产、搜索已有资产、或把当前项目中已经验证过的能力沉淀进 Dec。
---

# Dec 代理

Dec 是一个个人 AI 知识仓库，帮助你积累和复用 AI 资产（Skills、Rules、MCP 配置）。

项目里由 Dec `pull` 出来的 IDE 配置文件并不等于“禁止提交”。像 `.cursor/`、`.claude/`、`.codex/`、`.codebuddy/`、`.mcp.json` 这类项目级输出，如果内容是 Dec 托管资产生成的结果，通常可以按仓库约定单独提交；敏感值应继续放在 `.dec/vars.yaml`、`~/.dec/local/vars.yaml` 或用户本机配置里，而不是重新写回这些输出文件。

## 何时使用

### 主动建议用户的场景

1. **用户在新项目中需要初始化 Dec**
   - 运行 `dec config init` 生成项目配置，从 available 复制需要的资产到 enabled
   - 如资产模板使用 `{{VAR_NAME}}` 占位符，编辑 `.dec/vars.yaml` 填写变量
   - 运行 `dec pull` 拉取所有已启用的资产
   - pull 完成后，如当前仓库会跟踪 Dec 托管的 IDE 输出，主动询问用户是否要把这些变更单独 commit 到当前仓库

2. **用户提到需要之前做过的工具/配置**
   - 运行 `dec search "<query>"` 搜索已有资产
   - 或用 `dec list` 列出所有资产

3. **用户修改了已拉取的资产**
   - 修改 `.dec/cache/` 中的缓存文件
   - 运行 `dec push` 推送修改到远程仓库
   - **禁止**手动去修改其他 IDE 目录中的同名文件，统一通过 dec 命令同步

4. **用户需要新增资产**
   - 如果用户是要把当前项目里已经存在的好用能力抽出来复用，优先使用 `dec-extract-asset` skill
   - 否则按 Dec 资产流程在 `.dec/cache/<vault>/` 下创建内容
   - 把资产写入 `.dec/config.yaml` 的 `enabled`
   - 运行 `dec push` 推送到远程仓库

5. **用户需要从当前项目中沉淀已有能力**
   - 优先使用 `dec-extract-asset` skill 做抽象和入库
   - 确保结果最终落在 `.dec/cache/`，而不是只留在 IDE 目录
   - 完成后运行 `dec push`

6. **用户需要删除远程资产**
   - 运行 `dec push --remove <type> <name>` 删除（需交互确认）

7. **用户刚执行完 `dec pull`**
   - 检查当前项目仓库里由 Dec 托管的项目级 IDE 输出是否有变更，例如 `.cursor/`、`.claude/`、`.codex/`、`.codebuddy/`、`.mcp.json`
   - 默认把这类变更视为“可以提交，但应单独提交”的候选，而不是和业务代码混在同一笔提交里
   - 主动询问用户是否要顺手创建一笔独立 commit；如果用户同意，再继续提交
   - `.dec/vars.yaml`、用户本机配置、或任何看起来像密钥/口令的内容，不要因为这条规则就自动纳入提交

## 快速参考

### 资产管理

| 操作 | 命令 | 说明 |
|------|------|------|
| 列出所有资产 | `dec list` | 显示仓库中的所有 Skills、Rules、MCP |
| 搜索资产 | `dec search "<query>"` | 按资产名称搜索 |
| 拉取资产 | `dec pull` | 拉取 config.yaml 中所有已启用的资产 |
| 新增资产 | `dec push` | 先在 `.dec/config.yaml` 和 `.dec/cache/` 中写好资产，再推送 |
| 拉取指定版本 | `dec pull --version <ref>` | 拉取指定 commit/tag 版本 |
| 推送修改 | `dec push` | 将缓存中的修改推回远程仓库 |
| 删除远程资产 | `dec push --remove <type> <name>` | 需交互确认 |

补充：`dec pull` 后，如果当前仓库希望跟踪 Dec 托管的 IDE 配置，agent 应询问用户是否要把这些变更单独 commit；推荐与业务代码分开提交。

### 配置和初始化

| 操作 | 命令 | 说明 |
|------|------|------|
| 连接仓库 | `dec config repo <url>` | 连接个人仓库（GitHub URL） |
| 配置全局 IDE | `dec config global` | 为本机 IDE 安装 Dec 内置 Skills，并创建 `~/.dec/local/vars.yaml` 模板 |
| 初始化项目 | `dec config init` | 生成项目配置和 `.dec/vars.yaml` 模板，选择启用的资产 |
| 查看配置 | `dec config show` | 显示全局和项目配置 |

## 配置文件格式

项目配置位于 `.dec/config.yaml`，采用 available/enabled 双区结构。当前版本为 `v2`，按 `vault -> item -> type` 组织：

```yaml
version: v2

ides:               # 可选；当前项目覆盖全局 IDE 列表
  - cursor
  - codex

editor: code --wait # 可选；也可写成 vim / vi

available:          # 仓库中所有可用资产（dec config init 自动生成）
  my-vault:
    my-rule:
      rules: true
    my-mcp:
      mcp: true

enabled:            # 已启用资产（从 available 复制到这里即为启用）
  my-vault:
    my-rule:
      rules: true
```

- `dec config init` 自动填充 available，enabled 留空
- 读取到没有 `version` 的旧配置时，Dec 会按 `v1` 自动迁移到 `v2` 后再继续执行
- `ides` 可选，填写当前项目要部署到的 IDE 列表；不写则继承全局配置
- `editor` 可选，项目级可覆盖全局交互式编辑器
- 用户从 available 复制想要的资产到 enabled
- `dec pull` 只拉取 enabled 中的资产
- pull 时自动校验 enabled vs available，清理不再启用的旧资产
- 对于 Claude / Claude Internal，项目级输出统一写入 `.claude/`；只有用户级目录仍然区分 `~/.claude/` 与 `~/.claude-internal/`。
- 对于 Codex / Codex Internal，项目级输出统一写入 `.codex/`；其中 MCP 会写入 `.codex/config.toml` 的 `[mcp_servers.<name>]` 段。只有用户级目录仍然区分 `~/.codex/` 与 `~/.codex-internal/`。

## 占位符变量

资产模板中可以使用 `{{VAR_NAME}}` 占位符，`dec pull` 时会替换为实际值。
变量名必须以大写字母开头，只能包含大写字母、数字和下划线。

变量优先级如下：

1. `.dec/vars.yaml` 中的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 中的 `vars`
3. `~/.dec/local/vars.yaml` 中的 `vars`

- `dec config global` 会创建 `~/.dec/local/vars.yaml` 模板，适合存放机器级敏感变量。
- `dec config init` 会创建 `.dec/vars.yaml` 模板，适合项目级变量和按资产覆盖的变量。

- 只把确实会按项目变化、且必须保留的值做成占位符，例如默认项目名、目录、URL。
- 稳定的流程词汇或共享约定，例如固定 bucket 名、固定 label 名，不要默认变量化。


示例：

```yaml
vars:
  API_BASE_URL: "https://api.example.com"

assets:
  mcp:
    my-mcp:
      vars:
        API_TOKEN: "<TOKEN>"
```

如果变量缺失，pull 时会提示，并保留原始 `{{VAR_NAME}}` 不替换。

## 新增资产

当前 `dec push` 的推送源是项目中的 `.dec/cache/`，不是 IDE 目录。
也就是说，可以直接在已初始化项目的 `.dec/` 目录中组织并编写资产，然后执行 `dec push`。

推荐流程：

1. 先确保项目已经执行过 `dec config init`。
2. 编辑 `.dec/config.yaml`，把新资产写入 `enabled`。
   例如：

   ```yaml
   version: v2

   enabled:
     my-vault:
       my-skill:
         skills: true
       my-rule:
         rules: true
       my-mcp:
         mcp: true
   ```
3. 如需避免当前项目后续 `dec pull` 出现 available 校验警告，也把同一项写入 `available`，或在 push 后再执行一次 `dec config init` 刷新 available。
4. 在 `.dec/cache/` 下按类型创建资产文件：

```text
.dec/cache/<vault>/skills/<name>/SKILL.md
.dec/cache/<vault>/rules/<name>.mdc
.dec/cache/<vault>/mcp/<name>.json
```

5. 如果模板里使用了 `{{VAR_NAME}}`，编辑 `.dec/vars.yaml` 或 `~/.dec/local/vars.yaml`。
6. 运行 `dec push` 推送到远程仓库。

不要直接在 `.cursor/`、`.codex/` 等 IDE 目录里创建资产；这些目录只是 pull 后的部署结果，`dec push` 不会从那里读取新资产。

但这不等于这些目录永远不能提交。如果它们是 Dec 托管资产生成的项目级输出，而且敏感值已经通过 vars / 本机配置抽离，那么可以按仓库约定把它们作为独立 commit 纳入当前项目；只是不要把它们当作新增资产的来源。

## 资产格式

### Skill（目录）

Skill 必须是一个包含 `SKILL.md` 的目录。

### Rule（文件）

Rule 是单个 `.mdc` 文件。

### MCP（JSON 片段）

MCP 必须是单个 server 片段 JSON，其中 `command` 必填，`args`、`env` 按需提供。

Dec 仓库中的 MCP 资产仍然保存为 JSON 片段；部署到 Cursor / CodeBuddy / Claude / Claude Internal 等 IDE 时会写入对应的 JSON MCP 配置文件，部署到 Codex / Codex Internal 时则会转写到 `.codex/config.toml` 的 `[mcp_servers.<name>]` 段。

## 故障排查

### "仓库未连接"

运行 `dec config repo <url>` 连接你的仓库。

### "找不到资产"

1. 确认资产名称：`dec search "<partial-name>"`
2. 列出所有资产：`dec list`

### "拉取失败"

1. 检查仓库连接：`dec config show`
2. 验证资产存在：`dec search <name>`
3. 检查 enabled 配置是否正确
4. 如输出提示 `变量 {{XXX}} 未定义`，在 `.dec/vars.yaml` 或 `~/.dec/local/vars.yaml` 中补充

### "配置校验警告"

pull 前会校验 enabled 中的资产是否在 available 中存在。如果看到警告：
- 检查资产名是否拼写正确
- 运行 `dec config init` 更新 available 列表

## 修改资产的正确流程

1. **修改 `.dec/cache/` 中的缓存文件**
2. **推送修改**：`dec push`
3. **在其他项目中拉取**：`dec pull`

如果刚执行了 `dec pull`，且当前项目希望跟踪 Dec 生成的 IDE 配置，继续动作通常是：先看 diff，再问用户是否要把这些项目级输出单独 commit。

**禁止**：直接操作 `~/.dec/repo.git` 或其他底层目录。所有操作必须通过 `dec` CLI 完成。

## 更多信息

运行 `dec --help` 查看完整帮助。
