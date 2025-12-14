# Dec

Dec 是一个规则和 MCP 工具管理器，用于管理 Cursor/IDE 的规则文件和 MCP 工具配置。

## 设计理念

- **规则是编程规范，MCP 是工具能力**
- **工具使用说明由 MCP 自描述，不需要规则文件**
- **配置即声明，修改配置就是管理规则和工具**

## 特性

- 📦 **规则管理** - 核心规则、技术栈规则、功能规则分层管理
- 🔧 **MCP 工具** - 自动配置 MCP Server，AI 可直接调用
- 🌍 **跨平台** - 支持 Linux、macOS、Windows
- 🔗 **多 IDE 支持** - Cursor、CodeBuddy、Windsurf 等
- 📚 **包注册表** - 中心化的包索引，易于发现和管理

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
# 初始化项目配置
dec init

# 同步规则和 MCP 配置
dec sync

# 列出可用包
dec list

# 搜索包
dec search github

# 查看包详情
dec info github
```

## 命令参考

| 命令 | 说明 |
|------|------|
| `init` | 初始化项目配置（创建 `.dec/config/`） |
| `sync` | 同步规则文件和 MCP 配置到 IDE |
| `list` | 列出可用/已安装的包 |
| `search <keyword>` | 搜索包 |
| `info <name>` | 查看包详情 |
| `link` | 链接本地开发包 |
| `unlink <name>` | 移除本地链接 |
| `publish-notify` | 通知注册表更新（发布后执行） |
| `serve` | 启动 MCP Server 模式 |

## 项目配置

Dec 使用 `.dec/config/` 目录存储项目配置：

```
.dec/config/
├── project.json      # 项目信息 + 目标 IDE
├── technology.json   # 技术栈（语言/框架/平台）
└── packs.json        # 规则包 + MCP 工具启用/配置
```

### packs.json 示例

```json
{
  "documentation": {
    "enabled": true
  },
  "version-management": {
    "enabled": true
  },
  "github": {
    "enabled": true
  },
  "dec": {
    "enabled": true
  }
}
```

## 规则分层

| 层级 | 类型 | 说明 |
|------|------|------|
| Layer 0 | 核心规则 | 始终启用（principles, security, git-config 等） |
| Layer 1 | 技术栈规则 | 根据 technology.json 自动启用 |
| Layer 2 | 功能规则 | 用户在 packs.json 中选择启用 |

## 目录结构

```
~/.dec/
├── registry/                 # 注册表
│   ├── registry.json         # 正式注册表
│   ├── test.json             # 测试注册表
│   └── local.json            # 本地开发包链接
├── mcp/                      # MCP Server 安装目录
│   └── <package-name>/
└── bin/
    └── dec
```

## 开发包

### 包类型

| 类型 | 说明 |
|------|------|
| `rule` | 规则包，包含 `.mdc` 规则文件 |
| `mcp` | MCP 工具包，提供 MCP Server |

### package.json 示例

```json
{
  "name": "my-pack",
  "version": "1.0.0",
  "type": "rule",
  "description": "我的规则包",
  "rules": ["rules/my-rules.mdc"],
  "repository": {
    "type": "git",
    "url": "https://github.com/user/my-pack"
  }
}
```

### 本地开发

```bash
# 在包目录下链接到本地注册表
dec link

# 查看已链接的包
dec link --list

# 移除链接
dec unlink my-pack
```

### 发布包

1. 创建 GitHub Release
2. 执行 `dec publish-notify` 通知注册表更新
3. 或创建 Issue（标题 `[pack-sync] 包名`）触发同步

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
