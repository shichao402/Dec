# Dec

Dec 是一个个人 AI 知识仓库工具，用来保存、查找和复用 Skills、Rules、MCP 配置，并把这些资产同步到 Cursor、Windsurf、CodeBuddy、Trae 等 IDE。

## 核心思路

- 资产长期保存在个人 Vault（Git 仓库）中
- 项目只提交声明式配置，不提交 Dec 生成的托管副本
- `.dec/config/vault.yaml` 是项目级唯一真相源
- `dec sync` 负责把声明的资产部署到所有配置的 IDE

## 快速开始

### 安装 Dec

#### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.sh | bash
```

#### Windows (PowerShell)

```powershell
iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.ps1 | iex
```

### 基本使用

```bash
# 1. 初始化项目配置
dec init

# 2. 初始化个人知识仓库
dec vault init --repo https://github.com/<user>/<repo>
# 或
dec vault init --create my-dec-vault

# 3. 保存资产到 Vault
dec vault save skill .cursor/skills/my-skill
dec vault save rule .cursor/rules/my-rule.mdc
dec vault save mcp ./postgres-tool.json

# 4. 搜索和拉取已有资产
dec vault find "api test"
dec vault pull skill create-api-test

# 5. 根据项目声明同步到所有 IDE
dec sync
```

## 命令参考

| 命令 | 说明 |
|------|------|
| `init` | 初始化项目配置（创建 `.dec/config/ides.yaml` 和 `.dec/config/vault.yaml`） |
| `sync` | 根据 `vault.yaml` 同步项目声明的资产到所有 IDE |
| `vault init` | 初始化个人知识仓库 |
| `vault save` | 保存 skill / rule / mcp 到个人 Vault |
| `vault find` | 搜索 Vault 中的资产 |
| `vault pull` | 拉取资产到项目，并自动写入 `vault.yaml` |
| `vault list` | 列出 Vault 中的资产 |
| `vault status` | 检查当前项目已追踪资产的本地变更 |
| `vault push` | 手动推送 Vault 变更到远程仓库 |

## 项目配置

Dec 使用 `.dec/config/` 目录存储项目配置：

```text
.dec/config/
├── ides.yaml         # 目标 IDE 配置
└── vault.yaml        # 项目声明的 Vault 资产
```

### `ides.yaml` 示例

```yaml
ides:
  - cursor
  - windsurf
```

### `vault.yaml` 示例

```yaml
vault_skills:
  - create-api-test

vault_rules:
  - my-security-rule

vault_mcps:
  - postgres-tool
```

## MCP 资产格式

Vault 中的 MCP 资产保存为单个 MCP server 片段 JSON，而不是整份 `mcp.json`：

```json
{
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres"],
  "env": {
    "DATABASE_URL": "${DATABASE_URL}"
  }
}
```

`dec sync` 和 `dec vault pull mcp ...` 都会把它作为托管的 `dec-*` server 合并进 IDE 的 live `mcp.json`。

## 目录结构

```text
~/.dec/
├── config.yaml      # 全局配置（vault_source）
├── vault/           # 个人知识仓库（Git 管理）
└── bin/
    └── dec
```

## 从源码构建

```bash
git clone https://github.com/shichao402/Dec.git
cd Dec
go build -o dec .
```

## 文档

详细文档请查看 [Documents/](Documents/) 目录。

- [架构设计](Documents/design/architecture/ARCHITECTURE.md)
- [开发指南](Documents/development/setup/DEVELOPMENT.md)
- [构建指南](Documents/development/deployment/BUILD.md)
- [测试指南](Documents/development/testing/TESTING.md)
- [发布流程](Documents/development/deployment/RELEASE.md)

## 许可证

MIT
