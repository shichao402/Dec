# Dec

Dec 是一个个人 AI 知识仓库工具。

它把你在 Cursor、CodeBuddy 等 IDE 中积累的 Skills、Rules、MCP 配置统一保存到一个个人 Vault（Git 仓库）里。然后你可以在不同项目中快速获取这些资产，实现跨项目、跨机器复用自己的 AI 资产，而不是在每个仓库里重复维护一份。

## 这是什么问题的解法

很多团队和个人都会遇到这些问题：

- 常用 Skill 只能留在某一个项目里，迁移困难
- Rule 散落在不同仓库中，风格难以统一
- MCP 配置复制粘贴多次，容易漂移
- 项目里既想复用资产，又不想直接提交 IDE 生成副本

Dec 的解决方案：

- 个人维度：通过 `dec repo` 关联你的 Vault（个人知识库）
- 项目维度：使用 `dec vault pull` 从 Vault 中获取需要的资产
- IDE 维度：Dec 自动将资产部署到配置的 IDE 目录

## 核心概念

### 1. Vault（个人知识库）

Vault 是你的个人知识仓库，底层是一个 Git 仓库。使用 `dec repo <url>` 关联到你的 Vault。

Vault 中目前支持三类资产：

- `skill`：技能脚本（目录，包含 SKILL.md）
- `rule`：规则文件（.mdc 文件）
- `mcp`：MCP 服务配置（JSON 片段）

### 2. 资产部署

通过 `dec vault pull <type> <name>` 将资产从 Vault 部署到当前项目的配置 IDE。

Dec 部署出来的资产会以 `dec-` 前缀命名，例如：

- `.cursor/skills/dec-create-api-test/`
- `.cursor/rules/dec-my-rule.mdc`
- `.cursor/mcp.json` 中的 `dec-postgres-tool`

### 3. 支持的 IDE

Dec 目前支持以下 IDE：

| IDE | Skills 路径 | Rules 路径 | MCP 配置 |
|-----|-----------|----------|---------|
| Cursor | `.cursor/skills/` | `.cursor/rules/` | `.cursor/mcp.json` |
| CodeBuddy | `.codebuddy/skills/` | `.codebuddy/rules/` | `.mcp.json` |

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

安装脚本会下载预编译二进制并加入 PATH。若你希望自定义运行目录，可以提前设置 `DEC_HOME`。

### 2. 关联个人 Vault

```bash
dec repo https://github.com/<user>/<your-vault-repo>
```

这个命令会：
- 在本地克隆或更新你的 Vault 仓库
- 将 Vault 地址记录到全局配置
- 打印你的 Vault 根目录位置

### 3. 从 Vault 获取资产

```bash
# 获取 Skill
dec vault pull skill create-api-test

# 获取 Rule
dec vault pull rule my-logging-standard

# 获取 MCP
dec vault pull mcp postgres-tool
```

`dec vault pull` 会自动将资产部署到当前目录的 IDE（Cursor、CodeBuddy）。

### 4. 管理你的 Vault

#### 保存资产到 Vault

```bash
# 保存 Skill
dec vault save skill .cursor/skills/my-skill

# 保存 Rule  
dec vault save rule .cursor/rules/my-rule.mdc

# 保存 MCP
dec vault save mcp ./postgres-tool.json

# 保存时加标签
dec vault save skill .cursor/skills/my-skill --tag testing --tag api
```

#### 搜索 Vault 中的资产

```bash
dec vault search "api test"
```

#### 列出 Vault 中的所有资产

```bash
dec vault list
dec vault list --type skill
dec vault list --type rule
dec vault list --type mcp
```

#### 从项目中移除资产

```bash
dec vault remove skill create-api-test
dec vault remove rule my-logging-standard
dec vault remove mcp postgres-tool
```

#### 推送 Vault 变更到远程

```bash
dec vault push
```

这在你本地修改了 Vault 资产但推送失败时使用。

## 推荐工作流

### 工作流 A：第一次设置个人 Vault

```bash
# 1. 创建或初始化你的 Vault 仓库
# （在 GitHub 或 GitLab 上创建一个空仓库）

# 2. 在本机关联到 Dec
dec repo https://github.com/<user>/<your-vault-repo>

# 3. 把常用资产保存进去
dec vault save skill .cursor/skills/create-api-test
dec vault save rule .cursor/rules/my-security-rule.mdc
dec vault save mcp ./postgres-tool.json

# 4. 推送到远程
dec vault push
```

### 工作流 B：在新项目中复用 Vault 资产

```bash
# 1. 进入新项目目录
cd my-new-project

# 2. 从 Vault 获取所需资产
dec vault pull skill create-api-test
dec vault pull rule my-security-rule
dec vault pull mcp postgres-tool

# 现在你的 IDE（.cursor/.codebuddy）已经自动获得这些资产
```

### 工作流 C：更新已有资产

```bash
# 1. 在项目的 IDE 中编辑托管资产（如 .cursor/skills/dec-create-api-test）
# 2. 将更改保存回 Vault
dec vault save skill .cursor/skills/dec-create-api-test

# 3. 推送到远程（可选）
dec vault push

# 4. 在其他项目中重新拉取最新版本
cd ../another-project
dec vault pull skill create-api-test
```

## 命令参考

### 顶级命令

#### `dec repo <url>`

关联你的 Vault 仓库（Git 地址）。

```bash
dec repo https://github.com/<user>/<your-vault-repo>
```

作用：

- 克隆或更新本地 Vault 副本
- 将 Vault 地址保存到全局配置

#### `dec vault`

管理 Vault 中的资产。包含以下子命令。

#### `dec vault init <vault-name>`

（已弃用/内部命令）创建一个新的本地 Vault。通常不需要手动使用，用 `dec repo` 代替。

### Vault 资产管理

#### `dec vault save <type> <path> [--tag <tag>...]`

保存本地资产到 Vault。

```bash
dec vault save skill .cursor/skills/my-skill
dec vault save rule .cursor/rules/my-rule.mdc
dec vault save mcp ./postgres-tool.json
dec vault save skill .cursor/skills/my-skill --tag testing --tag api
```

支持类型：

- `skill`：目录，且必须包含 `SKILL.md`
- `rule`：单个 `.mdc` 文件
- `mcp`：单个 MCP server 片段 JSON

说明：

- 保存成功后会提交到本地 Vault Git 仓库
- 如果远程 push 失败，保存仍然视为成功，但会输出 warning
- 使用 `--tag` 为资产添加标签，便于后续搜索

#### `dec vault pull <type> <name>`

从 Vault 下载资产到当前项目。

```bash
dec vault pull skill create-api-test
dec vault pull rule my-logging-standard
dec vault pull mcp postgres-tool
```

行为：

- 从 Vault 下载指定资产
- 自动部署到当前项目的 IDE（Cursor、CodeBuddy）
- 本地文件遵循 `dec-<name>` 命名约定

#### `dec vault remove <type> <name>`

从项目中移除已拉取的资产。

```bash
dec vault remove skill create-api-test
dec vault remove rule my-logging-standard
```

#### `dec vault search <query>`

搜索 Vault 中的资产。

```bash
dec vault search "api test"
dec vault search "logging"
```

搜索范围：

- 资产名称
- 资产描述
- 资产标签

#### `dec vault list [--type <type>]`

列出 Vault 中的所有资产。

```bash
dec vault list
dec vault list --type skill
dec vault list --type rule
dec vault list --type mcp
```

#### `dec vault push`

将 Vault 的本地变更推送到远程仓库。

```bash
dec vault push
```

说明：

- 如果本地 Vault 没有配置远程仓库，会报错
- 当 `dec vault save` 输出 push warning 时，可用此命令重试

### 其他命令

#### `dec update`

检查并更新 Dec 到最新版本。

```bash
dec update
```

行为：

- 检查 GitHub Release 上的最新版本
- 如已是最新版本，输出提示并退出
- 如有新版本，自动下载并替换当前二进制

说明：

- 支持平台：Linux (amd64/arm64)、macOS (amd64/arm64)、Windows (amd64)
- 自动检查：Dec 会在每次命令执行后台检查新版本（每天最多一次）

#### `dec version`

显示当前 Dec 的版本号。

```bash
dec version
```

#### `dec help`

显示帮助信息。

```bash
dec help
dec vault help
```

## 配置文件说明

### 全局配置

全局配置位于 Dec 根目录下（`DEC_HOME` 或默认用户目录），用于记录 Vault 仓库地址等信息。

## 资产格式要求

## Skill

Skill 必须是目录，并且包含 `SKILL.md`。

例如：

```text
my-skill/
├── SKILL.md
└── ...
```

Dec 会尝试从 `SKILL.md` 的 front matter 中读取：

- `name`
- `description`

如果没有 `name`，则使用目录名。

## Rule

Rule 必须是单个 `.mdc` 文件。

例如：

```bash
dec vault save rule .cursor/rules/my-rule.mdc
```

Dec 会尝试从 front matter 或一级标题中提取描述。

## MCP

MCP 必须是单个 server 片段 JSON，而不是完整的 `mcp.json`。

正确示例：

```json
{
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres"],
  "env": {
    "DATABASE_URL": "${DATABASE_URL}"
  }
}
```

错误示例：

```json
{
  "mcpServers": {
    "postgres": {
      "command": "npx"
    }
  }
}
```

## MCP 合并策略

Dec 在部署 MCP 时采用智能合并：

- `dec-*` 前缀的 MCP 条目由 Dec 托管
- 用户自己添加的 MCP 条目会被保留
- 旧的、已移除的 `dec-*` MCP 会被自动清理

## 建议的 `.gitignore`

建议忽略 Dec 生成的托管副本和追踪文件。

```gitignore
.cursor/rules/
.cursor/skills/dec-*
.cursor/mcp.json
.codebuddy/rules/
.codebuddy/skills/dec-*
.mcp.json
.dec/
```

## 常见场景

### 场景 1：查看 Vault 中的资产

```bash
dec vault list
dec vault list --type skill
dec vault search "security"
```

### 场景 2：保存新 Skill 并在别的项目复用

```bash
dec vault save skill .cursor/skills/my-new-skill
cd ../another-project
dec vault pull skill my-new-skill
```

## 故障排查

### Vault 未关联

先执行 `dec repo <url>` 关联你的 Vault 仓库。

### `dec vault save` 输出 warning

如果出现"推送到远程仓库失败"的提示，说明本地保存已成功，只是远程 push 失败。稍后执行 `dec vault push` 重试即可。

## 安装、构建与测试

## 从源码构建

```bash
git clone https://github.com/shichao402/Dec.git
cd Dec
go build -o dec .
```

## 直接运行 Go 测试

```bash
go test ./...
```

## 使用统一测试入口

```bash
./scripts/run-tests.sh
```

它会执行：

1. `go test ./...`
2. `scripts/self_host_test.py`

可选参数：

```bash
./scripts/run-tests.sh --skip-go-test
./scripts/run-tests.sh --skip-self-host
./scripts/run-tests.sh --keep-self-host-artifacts
```

## 平台支持

安装脚本和预编译发布目前覆盖：

- macOS `amd64` / `arm64`
- Linux `amd64` / `arm64`
- Windows `amd64`

## 项目文档

详细的架构设计文档见：

- `Documents/ARCHITECTURE.md`：Dec 的架构设计、模块划分、核心流程说明

开发、构建与测试相关的脚本和工具见：

- `scripts/` 目录：包含构建、测试、安装脚本
- `Makefile`：开发工作流自动化

## 许可证

MIT
