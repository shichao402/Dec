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

# 更新包缓存
dec update

# 查看/切换包源
dec source [url]

# 切换版本
dec use <version>
```

## 命令参考

| 命令 | 说明 |
|------|------|
| `init` | 初始化项目配置（创建 `.dec/config/`） |
| `sync` | 同步规则文件和 MCP 配置到 IDE |
| `list` | 列出可用的规则和 MCP 包 |
| `update` | 更新包缓存 |
| `source [url]` | 查看/切换包源 |
| `use <version>` | 切换版本 (latest / v1.2.0) |
| `publish-notify` | 通知注册表更新（发布后执行） |
| `serve` | 启动 MCP Server 模式 |

## 项目配置

Dec 使用 `.dec/config/` 目录存储项目配置：

```
.dec/config/
├── ides.yaml         # 目标 IDE 配置
├── technology.yaml   # 技术栈（语言/框架/平台/设计模式）
└── mcp.yaml          # MCP 工具配置
```

### technology.yaml 示例

```yaml
languages:
  - go
  - python

frameworks:
  - flutter

platforms:
  - cli

patterns:
  - command
```

## 规则分层

| 层级 | 类型 | 说明 |
|------|------|------|
| Layer 0 | 核心规则 | 始终启用（principles, security, git-config 等） |
| Layer 1 | 技术栈规则 | 根据 technology.yaml 自动启用 |

## 目录结构

```
~/.dec/
├── config.yaml              # 全局配置（包源、版本）
├── cache/
│   └── packages-v1.0.0/     # 包缓存（按版本）
│       ├── rules/
│       └── mcp/
└── bin/
    └── dec
```

## 发布包

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
