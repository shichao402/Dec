# Dec 架构设计

本文档描述 Dec 的系统架构和核心设计。

## 概述

Dec 是一个用于管理 Cursor AI 规则和 MCP 工具的包管理器。

### 设计理念

- **简单** - 配置即声明，修改配置就是管理规则和工具
- **安全** - 不执行任何脚本，只做文件分发
- **透明** - 所有包信息公开可查

## 目录结构

### 全局目录

```
~/.dec/
├── config.yaml              # 全局配置（包源、版本）
├── cache/
│   └── packages-v1.0.0/     # 包缓存（按版本）
│       ├── rules/
│       │   ├── core/        # 核心规则
│       │   ├── languages/   # 语言规则
│       │   ├── frameworks/  # 框架规则
│       │   ├── platforms/   # 平台规则
│       │   └── patterns/    # 设计模式规则
│       └── mcp/
└── bin/
    └── dec
```

### 项目目录

```
.dec/config/
├── ides.yaml         # 目标 IDE 配置
├── technology.yaml   # 技术栈配置
└── mcp.yaml          # MCP 工具配置
```

## 核心流程

### 包同步流程

```
1. 读取全局配置（包源、版本）
2. 检查本地缓存是否存在
3. 如果不存在，从包源下载
4. 扫描项目配置
5. 根据配置生成规则文件到 IDE 目录
6. 生成 MCP 配置
```

## 模块设计

### cmd/ - 命令行

| 文件 | 命令 | 说明 |
|------|------|------|
| `init.go` | `init` | 初始化项目配置 |
| `sync.go` | `sync` | 同步规则和 MCP |
| `list.go` | `list` | 列出可用包 |
| `update.go` | `update` | 更新包缓存 |
| `source.go` | `source` | 查看/切换包源 |
| `use.go` | `use` | 切换版本 |
| `serve.go` | `serve` | MCP Server 模式 |
| `publish_notify.go` | `publish-notify` | 通知注册表更新 |

### pkg/ - 核心包

| 包 | 说明 |
|---|------|
| `config` | 全局配置、项目配置、包获取 |
| `packages` | 包扫描、占位符解析 |
| `service` | 同步服务 |
| `ide` | IDE 抽象层 |
| `paths` | 路径管理 |
| `types` | 类型定义 |
| `version` | 版本管理 |

## 配置系统

### 全局配置

```yaml
# ~/.dec/config.yaml
packages_source: "https://github.com/shichao402/MyDecPackage"
packages_version: "latest"
```

### 项目配置

```yaml
# .dec/config/technology.yaml
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

## 相关文档

- [开发指南](../../development/setup/DEVELOPMENT.md)
- [测试指南](../../development/testing/TESTING.md)
