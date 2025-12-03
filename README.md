# CursorToolset

Cursor 工具集管理器 - 用于管理和安装 Cursor 工具集的命令行工具。

## 功能特性

- 📦 从 `toolsets.json` 读取工具集列表
- 🔧 将工具集作为 Git 子模块安装
- 📋 根据 `toolset.json` 自动安装文件
- 🎯 支持选择性安装特定工具集

## 安装

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
cursortoolset install --toolsets-dir ./my-toolsets
```

## 配置文件

### toolsets.json

项目根目录下的 `toolsets.json` 文件定义了可用的工具集列表：

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
│   └── list.go      # 列表命令
├── pkg/              # 核心包
│   ├── types/       # 数据类型定义
│   ├── loader/      # 配置加载器
│   └── installer/   # 安装器
├── toolsets.json    # 工具集列表
├── go.mod
└── main.go
```

## 开发

```bash
# 运行
go run main.go install

# 构建
go build -o cursortoolset
```

## 许可证

MIT

