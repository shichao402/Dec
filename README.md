# CursorToolset

Cursor 工具集管理器 - 用于管理和安装 Cursor 工具集的命令行工具。

## 功能特性

- 📦 从 `available-toolsets.json` 读取工具集列表
- 🔧 使用普通 Git 克隆方式安装（不依赖 Git 子模块）
- 📁 默认安装到 `.cursor/toolsets/` 目录（统一管理所有 Cursor 相关内容）
- 📋 根据 `toolset.json` 自动安装文件
- 🎯 支持选择性安装特定工具集
- 🧹 一键清理已安装的工具集
- ✅ 完整的测试覆盖
- 🚀 不需要 Git 仓库（可在任何目录运行）

## 安装

### 构建

```bash
go build -o cursortoolset
```

## 使用方法

### 列出所有可用工具集

```bash
cursortoolset list
```

### 安装所有工具集

```bash
cursortoolset install
```

### 安装特定工具集

```bash
cursortoolset install <toolset-name>
```

### 指定安装目录

```bash
# 默认安装到 .cursor/toolsets/
cursortoolset install

# 自定义安装目录
cursortoolset install --toolsets-dir ./my-toolsets
```

### 清理已安装的工具集

```bash
# 清理所有已安装的文件（会提示确认）
cursortoolset clean

# 强制清理，不提示确认
cursortoolset clean --force

# 只清理安装的文件，保留 .cursor/toolsets/ 目录
cursortoolset clean --keep-toolsets
```

## 配置文件

### available-toolsets.json

项目根目录下的 `available-toolsets.json` 文件定义了可用的工具集列表：

```json
[
  {
    "name": "github-action-toolset",
    "displayName": "GitHub Action AI 工具集",
    "githubUrl": "https://github.com/shichao402/GithubActionAISelfBuilder.git",
    "description": "GitHub Actions 构建和调试的 AI 规则工具集",
    "version": "1.0.0"
  }
]
```

### toolset.json

每个工具集都包含一个 `toolset.json` 文件，定义了工具的安装配置：

```json
{
  "name": "github-action-toolset",
  "install": {
    "targets": {
      ".cursor/rules/github-actions/": {
        "source": "core/rules/",
        "files": ["*.mdc"],
        "merge": true,
        "overwrite": false
      }
    }
  }
}
```

## 项目结构

```
CursorToolset/
├── cmd/              # CLI 命令
│   ├── root.go      # 根命令
│   ├── install.go   # 安装命令
│   ├── list.go      # 列表命令
│   └── clean.go     # 清理命令
├── pkg/              # 核心包
│   ├── types/       # 数据类型定义
│   ├── loader/      # 配置加载器
│   └── installer/   # 安装器
├── available-toolsets.json    # 可用工具集列表
├── go.mod
├── main.go
├── README.md        # 项目文档
├── ARCHITECTURE.md  # 架构设计文档
└── MIGRATION.md     # 迁移指南
```

### 使用项目的目录结构

当使用 CursorToolset 安装工具集后，目标项目的结构：

```
your-project/
├── .cursor/
│   ├── toolsets/              # 工具集源码（默认安装位置）
│   │   └── github-action-toolset/
│   └── rules/                 # 工具集安装的规则文件
│       └── github-actions/
├── scripts/                   # 工具集安装的脚本（可选）
│   └── toolsets/
└── ...其他项目文件
```

**重要**：建议在项目的 `.gitignore` 中添加 `.cursor/` 目录

## 开发

```bash
# 运行
go run main.go install

# 构建
go build -o cursortoolset
```

## 许可证

MIT


