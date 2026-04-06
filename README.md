# Dec

Dec 是一个个人 AI 知识仓库工具。

它把你在 Cursor、CodeBuddy 等 IDE 中积累的 Skills、Rules、MCP 配置统一保存到一个个人仓库（Git 仓库）里。然后你可以在不同项目中快速获取这些资产，实现跨项目、跨机器复用自己的 AI 资产，而不是在每个仓库里重复维护一份。

## 这是什么问题的解法

很多团队和个人都会遇到这些问题：

- 常用 Skill 只能留在某一个项目里，迁移困难
- Rule 散落在不同仓库中，风格难以统一
- MCP 配置复制粘贴多次，容易漂移
- 项目里既想复用资产，又不想直接提交 IDE 生成副本

Dec 的解决方案：

- 个人维度：通过 `dec config repo` 关联你的仓库
- 项目维度：使用 `dec config init` + `dec pull` 选择并拉取资产
- IDE 维度：Dec 自动将资产部署到配置的 IDE 目录

## 核心概念

### 1. 资产仓库

使用 `dec config repo <url>` 关联你的资产仓库，底层是一个 Git 仓库。

仓库中支持三类资产：

- `skill`：技能脚本（目录，包含 SKILL.md）
- `rule`：规则文件（.mdc 文件）
- `mcp`：MCP 服务配置（JSON 片段）

### 2. 项目配置

项目配置位于 `.dec/config.yaml`，采用 **available/enabled** 双区结构：

```yaml
ides:               # 可选；当前项目覆盖全局 IDE 列表
  - cursor
  - codex

editor: code --wait # 可选；也可写成 vim / vi

available:          # 仓库中所有可用资产（dec config init 自动生成）
  rules:
    - name: my-rule
      vault: my-vault

enabled:            # 已启用资产（从 available 复制到这里即为启用）
  rules:
    - name: my-rule
      vault: my-vault
```

- `dec config init` 扫描仓库填充 available，重复执行时保留已有 enabled / editor / ides
- `ides` 可选，填写当前项目要部署到的 IDE 列表；不写则继承 `~/.dec/config.yaml`。支持值见 `dec config global --ide ...`，例如 `cursor`、`codebuddy`、`windsurf`、`trae`、`claude`、`codex`
- `editor` 可选，支持在项目级指定交互式编辑器，例如 `vim`、`vi`、`code --wait`
- 用户从 available 复制想要的资产到 enabled
- `dec pull` 只拉取 enabled 中的资产
- pull 时自动校验 enabled vs available，清理不再启用的旧资产

### 3. 资产部署

`dec pull` 将资产部署到当前项目的配置 IDE。

Dec 部署出来的资产会以 `dec-` 前缀命名，例如：

- `.cursor/skills/dec-create-api-test/`
- `.cursor/rules/dec-my-rule.mdc`
- `.cursor/mcp.json` 中的 `dec-postgres-tool`

### 4. 支持的 IDE

| IDE | Skills 路径 | Rules 路径 | MCP 配置 |
|-----|-----------|----------|---------|
| Cursor | `.cursor/skills/` | `.cursor/rules/` | `.cursor/mcp.json` |
| CodeBuddy | `.codebuddy/skills/` | `.codebuddy/rules/` | `.mcp.json` |
| Windsurf | `.windsurf/skills/` | `.windsurf/rules/` | `.windsurf/mcp.json` |
| Trae | `.trae/skills/` | `.trae/rules/` | `.trae/mcp.json` |
| Claude | `.claude/skills/` | `.claude/rules/` | `.claude/mcp.json` |
| Claude Internal | `.claude-internal/skills/` | `.claude-internal/rules/` | `.claude-internal/mcp.json` |
| Codex | `.codex/skills/` | `.codex/rules/` | `.codex/mcp.json` |
| Codex Internal | `.codex-internal/skills/` | `.codex-internal/rules/` | `.codex-internal/mcp.json` |

更详细的使用语义见 `pkg/assets/dec/SKILL.md`，实现与存储结构见 `Documents/ARCHITECTURE.md`。

## 快速开始

### 1. 安装

#### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.sh | bash
```

#### Windows PowerShell

```powershell
iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.ps1 | iex
```

### 2. 连接个人仓库

```bash
dec config repo https://github.com/<user>/<your-repo>
```

### 3. 配置全局 IDE

```bash
dec config global                    # 配置所有支持的 IDE
dec config global --ide cursor       # 只配置 Cursor
```

这一步还会创建 `~/.dec/local/vars.yaml` 模板，用于填写机器级占位符变量，例如本机 API Token、数据库地址等。

### 4. 初始化项目

```bash
dec config init
```

这会扫描仓库中所有可用资产，生成 `.dec/config.yaml` 和 `.dec/vars.yaml`，并打开交互式编辑器让你选择。编辑器优先读取 `.dec/config.yaml` 的 `editor`，再读取 `~/.dec/config.yaml` 的 `editor`，默认优先 `vim` / `vi`。将 available 中想要的资产复制到 enabled 后保存即可。

### 5. 拉取资产

```bash
dec pull
```

根据 `.dec/config.yaml` 中 enabled 的资产，自动拉取并安装到 IDE。

如果资产模板里包含 `{{VAR_NAME}}` 形式的占位符，`dec pull` 会按以下优先级替换：

1. `.dec/vars.yaml` 中的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 中的 `vars`
3. `~/.dec/local/vars.yaml` 中的机器级变量

未定义的占位符会保留原样，并在 pull 时提示补充变量配置。

### 6. 推送修改

```bash
dec push                              # 推送缓存中的修改到远程
dec push --remove skill my-skill      # 删除远程资产（需确认）
```

如果要新增资产，直接在已初始化项目的 `.dec/` 目录中组织并编写即可：

1. 在 `.dec/config.yaml` 的 `enabled` 中加入新资产。
2. 在 `.dec/cache/<vault>/` 下创建对应文件：

```text
.dec/cache/<vault>/skills/<name>/SKILL.md
.dec/cache/<vault>/rules/<name>.mdc
.dec/cache/<vault>/mcp/<name>.json
```

3. 执行 `dec push` 推送到远程仓库。

`dec push` 的读取源是 `.dec/cache/`，不是 `.cursor/`、`.codex/` 等 IDE 目录。

## 推荐工作流

### 工作流 A：第一次设置

```bash
# 1. 连接仓库
dec config repo https://github.com/<user>/<your-repo>

# 2. 配置 IDE
dec config global

# 3. 在项目中初始化
dec config init

# 4. 拉取选中的资产
dec pull
```

### 工作流 B：在新项目中复用

```bash
cd my-new-project
dec config init       # 扫描仓库，选择资产
dec pull              # 拉取到项目
```

### 工作流 C：更新已有资产

```bash
# 1. 修改 .dec/cache/ 中的缓存文件
# 2. 推送到远程
dec push

# 3. 在其他项目中拉取最新版本
cd ../another-project
dec pull
```

### 工作流 D：新增资产

```bash
# 1. 确保项目已执行过初始化
dec config init

# 2. 编辑 .dec/config.yaml，把新资产加入 enabled
# 3. 在 .dec/cache/<vault>/ 下创建资产文件
# 4. 推送到远程仓库
dec push

# 5. 如需刷新 available 列表
dec config init
```

## 命令参考

### 配置命令

| 命令 | 说明 |
|------|------|
| `dec config repo <url>` | 连接个人仓库 |
| `dec config global [--ide]` | 配置全局 IDE |
| `dec config init` | 初始化项目配置（扫描仓库，打开编辑器选择资产） |
| `dec config show` | 显示当前配置 |

### 资产命令

| 命令 | 说明 |
|------|------|
| `dec list` | 列出仓库中所有资产 |
| `dec search <query>` | 搜索资产 |
| `dec pull` | 拉取 enabled 中的资产到项目 |
| `dec pull --version <ref>` | 拉取指定版本 |
| `dec push` | 推送缓存中的修改到远程 |
| `dec push --remove <type> <name>` | 删除远程资产（需交互确认） |

### 其他命令

| 命令 | 说明 |
|------|------|
| `dec update` | 更新 Dec 到最新版本 |
| `dec version` | 显示版本号 |

## 资产格式要求

资产模板支持 `{{VAR_NAME}}` 占位符，变量名必须以大写字母开头，只能包含大写字母、数字和下划线。

### Skill

Skill 必须是目录，包含 `SKILL.md`。

### Rule

Rule 必须是单个 `.mdc` 文件。

### MCP

MCP 必须是单个 server 片段 JSON，`command` 必填：

```json
{
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres"],
  "env": {
    "DATABASE_URL": "${DATABASE_URL}"
  }
}
```

## 项目目录结构

```
.dec/
├── config.yaml      # 项目配置（available + enabled）
├── cache/           # 资产缓存（pull 时写入，push 时读取）
├── .version         # 当前 pull 的版本记录
└── vars.yaml        # 项目变量定义（config init 自动创建）
```

机器级变量文件位于 `~/.dec/local/vars.yaml`，由 `dec config global` 自动创建。

全局配置位于 `~/.dec/config.yaml`，例如：

```yaml
repo_url: https://github.com/<user>/<your-repo>

ides:
  - cursor
  - codebuddy

editor: code --wait
```

其中 `ides` 是默认部署目标，`editor` 是 `dec config init` 打开交互式编辑器时使用的命令。

项目级变量文件示例：

```yaml
vars:
  API_BASE_URL: "https://api.example.com"

assets:
  mcp:
    my-mcp:
      vars:
        API_TOKEN: "<TOKEN>"
```

## 故障排查

### 仓库未连接

执行 `dec config repo <url>` 连接仓库。

### 配置校验警告

pull 前会校验 enabled 中的资产是否在 available 中存在。如果看到警告，检查拼写或运行 `dec config init` 更新 available。

### 推送/拉取失败

如果出现远端冲突，重新执行命令即可。Dec 使用临时 worktree，不会留下中间状态。

## 安装、构建与测试

### 从源码构建

```bash
git clone https://github.com/shichao402/Dec.git
cd Dec
go build -o dec .
```

### 运行测试

```bash
go test ./...
```

## 平台支持

- macOS `amd64` / `arm64`
- Linux `amd64` / `arm64`
- Windows `amd64`

## 项目文档

- `pkg/assets/dec/SKILL.md`：Dec Skill 的完整使用说明
- `Documents/ARCHITECTURE.md`：架构设计与模块说明

## 许可证

MIT
