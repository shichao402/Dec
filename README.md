# Dec

Dec 是一个个人 AI 知识仓库工具。

它把你在 Cursor、CodeBuddy、Windsurf、Trae 等 IDE 中积累的 Skills、Rules、MCP 配置统一保存到一个个人 Vault（Git 仓库）里，再按项目声明式地同步到目标 IDE 目录。这样你可以跨项目、跨机器复用自己的 AI 资产，而不是在每个仓库里重复维护一份。

## 这是什么问题的解法

很多团队和个人都会遇到这些问题：

- 常用 Skill 只能留在某一个项目里，迁移困难
- Rule 散落在不同仓库中，风格难以统一
- MCP 配置复制粘贴多次，容易漂移
- 项目里既想复用资产，又不想直接提交 IDE 生成副本

Dec 的做法是把“资产存储”和“项目使用”分开：

- 个人维度：把资产保存到自己的 Vault
- 项目维度：只在 `.dec/config/vault.yaml` 中声明需要哪些资产
- IDE 维度：由 `dec sync` 自动生成托管副本到 IDE 目录

## 核心概念

### 1. Vault

Vault 是你的个人知识仓库，底层是一个 Git 仓库，默认位于 `DEC_HOME/vault`，如果没有设置 `DEC_HOME`，则使用用户目录下的默认 Dec 根目录。

Vault 中目前支持三类资产：

- `skill`
- `rule`
- `mcp`

### 2. 项目声明

每个项目只维护两份核心配置：

```text
.dec/config/
├── ides.yaml
└── vault.yaml
```

- `ides.yaml` 声明本项目要同步到哪些 IDE
- `vault.yaml` 声明本项目需要哪些 Vault 资产

### 3. 托管副本

Dec 同步出来的资产会以 `dec-` 前缀写入 IDE 目录，例如：

- `.cursor/skills/dec-create-api-test/`
- `.cursor/rules/dec-my-rule.mdc`
- `.cursor/mcp.json` 中的 `dec-postgres-tool`

这些是 Dec 托管内容。

`dec sync` 只会清理和覆盖它自己托管的内容，不会主动删除用户自己的非 `dec-*` 资产。

### 4. 本地追踪

Dec 会把项目里已拉取/已同步的资产状态写到：

```text
.dec/tracking.json
```

这个文件用于 `dec vault status` 检查：

- 本地是否被修改
- 本地是否被删除
- Vault 是否有更新

## 快速开始

## 1. 安装

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.sh | bash
```

### Windows PowerShell

```powershell
iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.ps1 | iex
```

安装脚本会下载预编译二进制并加入 PATH。若你希望自定义运行目录，可以提前设置 `DEC_HOME`。

## 2. 初始化项目

在你的项目根目录执行：

```bash
dec init
```

或指定多个 IDE：

```bash
dec init --ide cursor --ide codebuddy --ide windsurf
```

这会创建：

```text
.dec/config/ides.yaml
.dec/config/vault.yaml
```

如果本地还没有安装 Dec Skill，`dec init` 还会自动尝试安装。

## 3. 初始化个人 Vault

### 使用已有仓库

```bash
dec vault init --repo https://github.com/<user>/<repo>
```

### 创建新的私有仓库

```bash
dec vault init --create my-dec-vault
```

使用 `--create` 时需要本机已安装并登录 GitHub CLI `gh`。

初始化成功后，Dec 会：

- 在本地创建/连接 Vault
- 把 Vault 地址记录到全局配置
- 自动尝试安装 Dec Skill

## 4. 保存资产到 Vault

### 保存 Skill

```bash
dec vault save skill .cursor/skills/my-skill
```

### 保存 Rule

```bash
dec vault save rule .cursor/rules/my-rule.mdc
```

### 保存 MCP

```bash
dec vault save mcp ./postgres-tool.json
```

### 保存时加标签

```bash
dec vault save skill .cursor/skills/my-skill --tag testing --tag api
```

## 5. 搜索和复用资产

### 搜索

```bash
dec vault find "api test"
```

### 列出全部资产

```bash
dec vault list
```

### 只列出某一类

```bash
dec vault list --type skill
dec vault list --type rule
dec vault list --type mcp
```

### 拉取到当前项目

```bash
dec vault pull skill create-api-test
dec vault pull rule my-logging-standard
dec vault pull mcp postgres-tool
```

`dec vault pull` 成功后会同时做三件事：

1. 把资产部署到当前项目配置的所有 IDE
2. 自动把该资产写入 `.dec/config/vault.yaml`
3. 更新 `.dec/tracking.json`

## 6. 按项目声明统一同步

```bash
dec sync
```

`dec sync` 会读取：

- `.dec/config/ides.yaml`
- `.dec/config/vault.yaml`

然后把声明的 Skills、Rules、MCP 同步到目标 IDE。

## 7. 查看本地变更状态

```bash
dec vault status
```

输出中的状态含义：

- `M`：本地已修改
- `D`：本地已删除
- `U`：Vault 有更新

## 8. 手动推送 Vault

```bash
dec vault push
```

这个命令适合在你希望手动控制远程同步时使用。

## 推荐工作流

## 工作流 A：第一次在新项目中使用 Dec

```bash
# 1. 初始化项目
dec init --ide cursor --ide codebuddy

# 2. 初始化个人 Vault
dec vault init --create my-dec-vault

# 3. 把常用资产保存进去
dec vault save skill .cursor/skills/create-api-test
dec vault save rule .cursor/rules/my-security-rule.mdc
dec vault save mcp ./postgres-tool.json

# 4. 在 vault.yaml 中声明需要的资产
#    或使用 pull 自动声明

dec sync
```

## 工作流 B：在另一个项目里复用已有资产

```bash
dec init --ide cursor --ide windsurf
dec vault pull skill create-api-test
dec vault pull rule my-security-rule
dec sync
```

## 工作流 C：修改本地资产并回写到 Vault

```bash
# 1. 先在项目里编辑托管副本或本地资产
# 2. 保存回 Vault
dec vault save skill .cursor/skills/dec-create-api-test

# 3. 如有需要，手动推送
dec vault push
```

## 命令参考

## `dec init`

初始化当前项目的 Dec 配置。

```bash
dec init
dec init --ide cursor --ide codebuddy
```

作用：

- 创建 `.dec/config/ides.yaml`
- 创建 `.dec/config/vault.yaml`
- 若本机未安装 Dec Skill，则自动尝试安装

说明：

- 如果项目已经初始化，不会重复创建配置
- `--ide` 可以多次传入
- 如果未指定，默认启用 `cursor`

## `dec sync`

根据项目声明把 Vault 资产同步到 IDE。

```bash
dec sync
```

行为：

- 从 `.dec/config/vault.yaml` 读取需要的资产
- 从 Vault 解析并拉取 Skills、Rules、MCP
- 部署到 `.dec/config/ides.yaml` 中列出的所有 IDE
- 更新 `.dec/tracking.json`

说明：

- 如果 Vault 远程刷新失败，会回退到本地缓存并输出 warning
- 如果同步中途失败，Dec 会尝试恢复该 IDE 原有的托管资产
- 如果 `vault.yaml` 中没有声明任何资产，会打印提示但仍正常结束

## `dec vault init`

初始化个人知识仓库。

```bash
dec vault init --repo https://github.com/<user>/<repo>
dec vault init --create my-dec-vault
```

参数：

- `--repo <url>`：克隆已有 GitHub 仓库
- `--create <name>`：创建新的 GitHub 私有仓库

说明：

- `--create` 依赖 `gh` CLI
- 初始化后会记录 `vault_source` 到全局配置
- 初始化后会自动尝试安装 Dec Skill

## `dec vault save`

保存本地资产到 Vault。

```bash
dec vault save skill <path>
dec vault save rule <path>
dec vault save mcp <path>
dec vault save skill <path> --tag testing --tag api
```

支持类型：

- `skill`：目录，且必须包含 `SKILL.md`
- `rule`：单个 `.mdc` 文件
- `mcp`：单个 MCP server 片段 JSON

说明：

- 保存成功后会提交到 Vault 本地 Git 仓库
- 如果远程 push 失败，保存仍然视为成功，但会输出 warning
- 成功后会尽量更新当前项目的追踪信息

## `dec vault find`

搜索 Vault 中的资产。

```bash
dec vault find "api test"
```

搜索范围：

- 名称
- 描述
- 标签

## `dec vault pull`

把某个资产下载到当前项目。

```bash
dec vault pull skill <name>
dec vault pull rule <name>
dec vault pull mcp <name>
```

行为：

- 部署到当前项目配置的所有 IDE
- 自动写入 `.dec/config/vault.yaml`
- 自动写入 `.dec/tracking.json`

说明：

- 如果项目还没有 `ides.yaml`，内部会默认按 `cursor` 处理
- MCP 会被合并进目标 IDE 的 live `mcp.json`

## `dec vault list`

列出 Vault 中的资产。

```bash
dec vault list
dec vault list --type skill
```

参数：

- `--type skill|rule|mcp`

## `dec vault status`

显示当前项目中已追踪资产的变更状态。

```bash
dec vault status
```

用途：

- 检查本地副本是否被手工修改
- 检查本地副本是否被删除
- 检查 Vault 版本是否已更新

## `dec vault push`

把 Vault 的本地变更推送到远程仓库。

```bash
dec vault push
```

说明：

- 如果 Vault 没有配置远程仓库，会直接报错
- 这是显式推送命令，不依赖项目目录

## `dec update`

检查并更新 Dec 到最新版本。

```bash
dec update
```

行为：

- 检查 GitHub Release 上的最新版本
- 如已是最新版本，输出提示并退出
- 如有新版本，自动下载并替换当前二进制
- 更新过程包含备份原始二进制，失败时可恢复

说明：

- 自动检查：Dec 会在每次命令执行时后台检查新版本（每天最多一次）
- 发现新版本时会输出提示，建议运行 `dec update` 更新
- 支持平台：Linux (amd64/arm64)、macOS (amd64/arm64)、Windows (amd64)

## 配置文件说明

## 项目级配置

### `.dec/config/ides.yaml`

声明项目要同步到哪些 IDE。

示例：

```yaml
ides:
  - cursor
  - codebuddy
  - windsurf
```

当前内置 IDE：

- `cursor`
- `codebuddy`
- `windsurf`
- `trae`

### `.dec/config/vault.yaml`

声明项目依赖哪些 Vault 资产。

示例：

```yaml
vault_skills:
  - create-api-test
  - fix-cors-issue

vault_rules:
  - my-security-rule
  - my-code-style

vault_mcps:
  - postgres-tool
```

说明：

- `dec vault pull` 会自动把拉取的资产写入这里
- `dec sync` 只会同步这里声明的内容
- 文件会自动去重、去空值并保持稳定格式

### `.dec/tracking.json`

Dec 自动生成的追踪文件。

用途：

- 记录资产名称、类型、本地路径
- 记录同步时的哈希
- 支持 `dec vault status`

通常应加入 `.gitignore`。

## 全局配置

### `config.yaml`

全局配置位于 Dec 根目录下，用于记录类似 `vault_source` 的信息。

示例：

```yaml
vault_source: https://github.com/<user>/<repo>
```

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

## IDE 输出路径

Dec 目前对不同 IDE 的输出路径如下：

| IDE | Skills | Rules | MCP |
|---|---|---|---|
| Cursor | `.cursor/skills/` | `.cursor/rules/` | `.cursor/mcp.json` |
| CodeBuddy | `.codebuddy/skills/` | `.codebuddy/rules/` | `.mcp.json` |
| Windsurf | `.windsurf/skills/` | `.windsurf/rules/` | `.windsurf/mcp.json` |
| Trae | `.trae/skills/` | `.trae/rules/` | `.trae/mcp.json` |

命名规则：

- Skill 目录：`dec-<name>`
- Rule 文件：`dec-<name>.mdc`
- MCP server 名称：`dec-<name>`

## `dec sync` 的实际行为

这是最重要的命令之一，理解它的行为会帮助你正确使用项目。

### 它会做什么

- 读取项目声明
- 打开本地 Vault
- 尝试刷新远程状态
- 将声明的 Skills / Rules / MCPs 部署到所有目标 IDE
- 更新追踪数据

### 它不会做什么

- 不会删除你的非 `dec-*` Skill 或 Rule
- 不会覆盖用户手工维护的非托管 MCP 条目
- 不会因为 Vault 远程刷新失败就直接放弃本地同步

### MCP 合并策略

- 当前声明的 `dec-*` MCP 会写入目标配置
- 旧的、未声明的 `dec-*` MCP 会被移除
- 用户自己加的 MCP 条目会被保留

### 失败恢复

如果同步某个 IDE 的过程中出错，Dec 会尝试恢复该 IDE 原来已有的托管资产，避免“先清理后失败”导致项目状态被破坏。

## 建议的 `.gitignore`

建议忽略 Dec 生成的托管副本和追踪文件。

示例：

```gitignore
.cursor/rules/
.cursor/skills/dec-*
.cursor/mcp.json
.codebuddy/rules/
.codebuddy/skills/dec-*
.mcp.json
.windsurf/rules/
.windsurf/skills/dec-*
.windsurf/mcp.json
.trae/rules/
.trae/skills/dec-*
.trae/mcp.json
.dec/tracking.json
```

## 常见场景

## 场景 1：给团队项目声明标准资产

```bash
dec init --ide cursor --ide codebuddy
dec vault pull skill create-api-test
dec vault pull rule my-security-rule
dec vault pull mcp postgres-tool
dec sync
```

然后把 `.dec/config/ides.yaml` 和 `.dec/config/vault.yaml` 提交到仓库里。

## 场景 2：只想查看自己 Vault 里都有什么

```bash
dec vault list
dec vault list --type skill
dec vault find "security"
```

## 场景 3：保存一个新 Skill 并在别的项目复用

```bash
dec vault save skill .cursor/skills/my-new-skill
cd ../another-project
dec init
dec vault pull skill my-new-skill
dec sync
```

## 场景 4：检查本地副本是否已经漂移

```bash
dec vault status
```

如果出现 `M`，说明本地副本已经被修改；如果出现 `U`，说明 Vault 中对应资产已有更新。

## 故障排查

## 1. `Vault 未初始化`

先执行：

```bash
dec vault init --repo <url>
```

或：

```bash
dec vault init --create <name>
```

## 2. `dec vault init --create` 失败

通常是以下原因之一：

- 本机没有安装 `gh`
- `gh auth login` 尚未完成
- GitHub 权限不足，无法创建私有仓库

## 3. `dec vault save` 输出 warning

如果出现类似“推送到远程仓库失败，资产已保存到本地”的提示，表示：

- 本地保存已经成功
- 本地 Git commit 已完成
- 只是远程 push 失败

这时可以稍后执行：

```bash
dec vault push
```

## 4. `dec sync` 输出 warning

如果出现类似“同步 Vault 远程状态失败，已回退到本地缓存”的提示，表示：

- 远程刷新失败
- 但本地 Vault 仍可用
- 当前同步已回退到本地缓存继续执行

## 5. `dec vault status` 没有输出变更

如果追踪项都没有变化，会显示：

```text
所有追踪项均无变化
```

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
