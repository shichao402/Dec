# CursorToolset

Cursor 工具集管理器 - 用于管理和安装 Cursor 工具集的命令行工具。

## 功能特性

- 📦 从 `available-toolsets.json` 读取工具集列表
- 🔧 使用普通 Git 克隆方式安装（不依赖 Git 子模块）
- 📁 默认安装到 `.cursor/toolsets/` 目录（统一管理所有 Cursor 相关内容）
- 📋 根据 `toolset.json` 自动安装文件
- 🎯 支持选择性安装特定工具集
- 🧹 一键清理已安装的工具集
- 🔄 内置更新功能（自更新 + 更新工具集）
- 📌 **智能版本控制**：自动检查版本号，只在需要时更新
- 🚀 一键安装脚本（类似 Homebrew）
- ✅ 完整的测试覆盖
- 🌍 跨平台支持（Linux、macOS、Windows）
- 💡 不需要 Git 仓库（可在任何目录运行）

## 快速安装

### 一键安装（推荐）

#### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/firoyang/CursorToolset/main/install.sh | bash
```

#### Windows (PowerShell)

以管理员身份运行：

```powershell
iwr -useb https://raw.githubusercontent.com/firoyang/CursorToolset/main/install.ps1 | iex
```

安装完成后，重新打开终端即可使用。

详细安装说明请查看 [INSTALL_GUIDE.md](./INSTALL_GUIDE.md)

### 从源码构建

```bash
git clone https://github.com/firoyang/CursorToolset.git
cd CursorToolset
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

### 更新

```bash
# 更新所有（CursorToolset + 配置 + 工具集）
cursortoolset update

# 只更新 CursorToolset 自身
cursortoolset update --self

# 只更新配置文件
cursortoolset update --available

# 只更新已安装的工具集
cursortoolset update --toolsets
```

**智能版本控制**：
- ✅ 自动检查版本号，只在有新版本时更新
- ✅ 显示当前版本和最新版本对比
- ✅ 避免不必要的下载和构建
- 详细说明请查看 [VERSION_CONTROL.md](./VERSION_CONTROL.md)

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
│   ├── clean.go     # 清理命令
│   └── update.go    # 更新命令
├── pkg/              # 核心包
│   ├── types/       # 数据类型定义
│   ├── loader/      # 配置加载器
│   └── installer/   # 安装器
├── available-toolsets.json    # 可用工具集列表
├── install.sh       # Linux/macOS 一键安装脚本
├── install.ps1      # Windows 一键安装脚本
├── go.mod
├── main.go
├── README.md        # 项目文档
├── ARCHITECTURE.md  # 架构设计文档
├── MIGRATION.md     # 迁移指南
├── INSTALL_GUIDE.md # 安装指南
└── VERSION_CONTROL.md # 版本控制说明
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

## 安装位置

CursorToolset 使用一键安装脚本后，会安装到：

- **Linux/macOS**: `~/.cursor/toolsets/CursorToolset/`
- **Windows**: `%USERPROFILE%\.cursor\toolsets\CursorToolset\`

并自动添加到系统 PATH，可在任何位置运行。

## 开发

```bash
# 运行
go run main.go install

# 构建
go build -o cursortoolset
```

## 许可证

MIT


