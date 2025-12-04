# CursorToolset

Cursor 工具集管理器 - 用于管理和安装 Cursor 工具集的命令行工具。

## 功能特性

- 📦 从 `available-toolsets.json` 读取工具集列表
- 🔧 使用普通 Git 克隆方式安装（不依赖 Git 子模块）
- 📁 **全局安装目录** - 类似 pip/brew 的设计理念（`~/.cursortoolsets/`）
- 📋 根据 `toolset.json` 自动安装文件
- 🎯 支持选择性安装特定工具集
- 🗑️ **支持卸载单个工具集**
- 🔍 **支持搜索工具集**
- 📋 **支持查看工具集详细信息**
- 📌 **支持指定版本安装**
- 🔒 **支持 SHA256 校验**
- 🔗 **支持依赖自动安装**
- 🧹 一键清理已安装的工具集
- 🔄 内置更新功能（自更新 + 更新工具集）
- 🚀 一键安装脚本（类似 Homebrew）
- ✅ 完整的测试覆盖
- 🌍 跨平台支持（Linux、macOS、Windows）
- 💡 不需要 Git 仓库（可在任何目录运行）
- 🏠 **环境变量配置** - 通过 `CURSOR_TOOLSET_HOME` 自定义安装位置

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

### 搜索工具集 (新)

```bash
# 根据关键词搜索
cursortoolset search github
cursortoolset search action
```

### 查看工具集详细信息 (新)

```bash
# 查看完整信息
cursortoolset info github-action-toolset
```

### 安装工具集

```bash
# 安装所有工具集
cursortoolset install

# 安装特定工具集（会自动安装依赖）
cursortoolset install <toolset-name>

# 安装指定版本 (新)
cursortoolset install <toolset-name> --version v1.0.0
cursortoolset install <toolset-name> -v v1.0.0
```

### 卸载工具集 (新)

```bash
# 卸载特定工具集（交互式确认）
cursortoolset uninstall <toolset-name>

# 强制卸载（跳过确认）
cursortoolset uninstall <toolset-name> --force
cursortoolset uninstall <toolset-name> -f
```

### 指定安装目录

```bash
# 默认安装到 .cursortoolsets/
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

# 只清理安装的文件，保留 .cursortoolsets/ 目录
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
- ✅ 版本号统一管理：`version.json` 作为唯一数据源
- ✅ 自动检查版本号，只在有新版本时更新
- ✅ 显示当前版本和最新版本对比
- ✅ 避免不必要的下载和构建
- 详细说明请查看：
  - [VERSION_MANAGEMENT.md](./VERSION_MANAGEMENT.md) - 版本号管理规范
  - [VERSION_CONTROL.md](./VERSION_CONTROL.md) - 版本比较和更新机制

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

## 目录结构

### 安装后的目录结构（类似 pip/brew）

```
~/.cursortoolsets/                    <- CURSOR_TOOLSET_HOME（默认根目录）
├── bin/                                <- 可执行文件目录
│   └── cursortoolset                  <- CursorToolset 主程序
├── repos/                              <- 工具集仓库源码（类似 brew 的 Cellar）
│   ├── github-action-toolset/         <- 工具集 Git 仓库
│   │   ├── toolset.json               <- 工具集配置文件
│   │   ├── core/                      <- 工具集核心文件
│   │   └── ...
│   └── other-toolset/                 <- 其他工具集
└── config/                             <- 配置文件目录
    └── available-toolsets.json        <- 可用工具集列表
```

**设计理念：**
- 📁 **全局安装目录**：类似 `~/.local`（pip）或 `/usr/local`（Homebrew）
- 🔗 **清晰的职责分离**：可执行文件、配置、源码分别存放
- 🌍 **环境变量配置**：通过 `CURSOR_TOOLSET_HOME` 自定义安装位置

详细说明请查看：[DIRECTORY_STRUCTURE.md](./DIRECTORY_STRUCTURE.md)

### 项目源码结构

```
CursorToolset/
├── cmd/              # CLI 命令
│   ├── root.go      # 根命令
│   ├── install.go   # 安装命令
│   ├── uninstall.go # 卸载命令（新）
│   ├── search.go    # 搜索命令（新）
│   ├── info.go      # 信息命令（新）
│   ├── list.go      # 列表命令
│   ├── clean.go     # 清理命令
│   └── update.go    # 更新命令
├── pkg/              # 核心包
│   ├── types/       # 数据类型定义
│   ├── paths/       # 路径处理（新）
│   ├── loader/      # 配置加载器
│   └── installer/   # 安装器
├── .root/            # 开发测试目录（不提交）
├── available-toolsets.json    # 可用工具集列表
├── install.sh       # Linux/macOS 一键安装脚本
├── install.ps1      # Windows 一键安装脚本
├── go.mod
├── main.go
├── README.md        # 项目文档
└── ...
```
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

- **Linux/macOS**: `~/.cursortoolsets/CursorToolset/`
- **Windows**: `%USERPROFILE%\.cursortoolsets\CursorToolset\`

并自动添加到系统 PATH，可在任何位置运行。

## 开发

### 本地构建

#### 方法 1: 使用构建脚本（推荐）

```bash
# 构建当前平台版本（自动清理、日志收集）
./build.sh

# 构建所有平台版本
./build.sh --all

# 指定输出和日志目录
./build.sh -o build -l build-logs

# 查看帮助
./build.sh --help
```

**特性**：
- ✅ **日志可收集**：所有构建日志保存到 `logs/` 目录，带时间戳
- ✅ **输出位置可确定**：构建产物统一输出到 `dist/` 目录（可配置）
- ✅ **自动清理遗留文件**：构建前自动清理旧的构建产物
- ✅ **构建信息记录**：生成 `BUILD_INFO.txt` 包含版本、时间、SHA256 等信息
- ✅ **开发环境隔离**：自动设置 `CURSOR_TOOLSET_HOME=$(pwd)/.root`

#### 方法 2: 使用 Makefile

```bash
# 构建当前平台版本（自动设置开发环境变量）
make build

# 构建所有平台版本
make build-all

# 运行测试
make test

# 格式化代码
make fmt

# 代码检查
make lint

# 查看所有可用命令
make help
```

**注意**: 使用 `make` 命令时，会自动设置 `CURSOR_TOOLSET_HOME=$(pwd)/.root`，确保开发环境隔离。

#### 方法 3: 直接使用 go build

```bash
# 基本构建
go build -o cursortoolset .

# 带版本信息构建
go build -ldflags "-X main.Version=$(cat version.json | grep -o '\"version\"[[:space:]]*:[[:space:]]*\"[^\"]*\"' | cut -d'\"' -f4) -X main.BuildTime=$(date -u '+%Y-%m-%d_%H:%M:%S')" -o cursortoolset .

# 运行（不构建）
go run main.go install
```

#### 开发环境变量

开发时推荐使用项目本地的 `.root/` 目录，避免影响系统安装：

```bash
# Linux/macOS
export CURSOR_TOOLSET_HOME=$(pwd)/.root
./cursortoolset install

# Windows PowerShell
$env:CURSOR_TOOLSET_HOME = "$PWD\.root"
.\cursortoolset.exe install
```

**目录说明：**
- `.root/` - 开发测试目录（已添加到 `.gitignore`）
- `.root/repos/` - 测试安装的工具集
- `.root/config/` - 测试配置文件

更多环境变量使用说明，请查看 [ENV_VARIABLES.md](ENV_VARIABLES.md)。

## 许可证

MIT


