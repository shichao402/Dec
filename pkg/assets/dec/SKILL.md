---
name: dec
description: >
  Dec 个人 AI 知识仓库代理。支持跨项目复用 Skills、Rules、MCP 配置。
  推荐用户保存新创建的资产、搜索已有资产、或在新项目中拉取所需资产。
---

# Dec 代理

Dec 是一个个人 AI 知识仓库，帮助你积累和复用 AI 资产（Skills、Rules、MCP 配置）。

## 何时使用

### 主动建议用户的场景

1. **用户在新项目中需要初始化 Dec**
   - 运行 `dec config init` 生成项目配置，从 available 复制需要的资产到 enabled
   - 如资产模板使用 `{{VAR_NAME}}` 占位符，编辑 `.dec/vars.yaml` 填写变量
   - 运行 `dec pull` 拉取所有已启用的资产

2. **用户提到需要之前做过的工具/配置**
   - 运行 `dec search "<query>"` 搜索已有资产
   - 或用 `dec list` 列出所有资产

3. **用户修改了已拉取的资产**
   - 修改 `.dec/cache/` 中的缓存文件
   - 运行 `dec push` 推送修改到远程仓库
   - **禁止**手动去修改其他 IDE 目录中的同名文件，统一通过 dec 命令同步

4. **用户需要新增资产**
   - 在已初始化项目的 `.dec/config.yaml` 中把资产写入 `enabled`
   - 在 `.dec/cache/<vault>/` 下按类型创建资产内容
   - 运行 `dec push` 推送到远程仓库

5. **用户需要删除远程资产**
   - 运行 `dec push --remove <type> <name>` 删除（需交互确认）

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

### 配置和初始化

| 操作 | 命令 | 说明 |
|------|------|------|
| 连接仓库 | `dec config repo <url>` | 连接个人仓库（GitHub URL） |
| 配置全局 IDE | `dec config global` | 为本机 IDE 配置 Dec Skill，并创建 `~/.dec/local/vars.yaml` 模板 |
| 初始化项目 | `dec config init` | 生成项目配置和 `.dec/vars.yaml` 模板，选择启用的资产 |
| 查看配置 | `dec config show` | 显示全局和项目配置 |

## 配置文件格式

项目配置位于 `.dec/config.yaml`，采用 available/enabled 双区结构：

```yaml
available:          # 仓库中所有可用资产（dec config init 自动生成）
  rules:
    - name: my-rule
      vault: my-vault
  mcps:
    - name: my-mcp
      vault: my-vault

enabled:            # 已启用资产（从 available 复制到这里即为启用）
  rules:
    - name: my-rule
      vault: my-vault
```

- `dec config init` 自动填充 available，enabled 留空
- 用户从 available 复制想要的资产到 enabled
- `dec pull` 只拉取 enabled 中的资产
- pull 时自动校验 enabled vs available，清理不再启用的旧资产

## 占位符变量

资产模板中可以使用 `{{VAR_NAME}}` 占位符，`dec pull` 时会替换为实际值。
变量名必须以大写字母开头，只能包含大写字母、数字和下划线。

变量优先级如下：

1. `.dec/vars.yaml` 中的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 中的 `vars`
3. `~/.dec/local/vars.yaml` 中的 `vars`

- `dec config global` 会创建 `~/.dec/local/vars.yaml` 模板，适合存放机器级敏感变量。
- `dec config init` 会创建 `.dec/vars.yaml` 模板，适合项目级变量和按资产覆盖的变量。

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

## 资产格式

### Skill（目录）

Skill 必须是一个包含 `SKILL.md` 的目录。

### Rule（文件）

Rule 是单个 `.mdc` 文件。

### MCP（JSON 片段）

MCP 必须是单个 server 片段 JSON，其中 `command` 必填，`args`、`env` 按需提供。

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

**禁止**：直接操作 `~/.dec/repo.git` 或其他底层目录。所有操作必须通过 `dec` CLI 完成。

## 更多信息

运行 `dec --help` 查看完整帮助。
