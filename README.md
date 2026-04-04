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
available:          # 仓库中所有可用资产（dec config init 自动生成）
  rules:
    - name: my-rule
      vault: my-vault

enabled:            # 已启用资产（从 available 复制到这里即为启用）
  rules:
    - name: my-rule
      vault: my-vault
```

- `dec config init` 扫描仓库填充 available，重复执行时保留已有 enabled
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

### 4. 初始化项目

```bash
dec config init
```

这会扫描仓库中所有可用资产，生成 `.dec/config.yaml`，并打开编辑器让你选择。将 available 中想要的资产复制到 enabled 后保存即可。

### 5. 拉取资产

```bash
dec pull
```

根据 `.dec/config.yaml` 中 enabled 的资产，自动拉取并安装到 IDE。

### 6. 推送修改

```bash
dec push                              # 推送缓存中的修改到远程
dec push --remove skill my-skill      # 删除远程资产（需确认）
```

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
└── vars.yaml        # 变量定义（可选，用于占位符替换）
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
