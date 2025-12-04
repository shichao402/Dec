# 环境变量配置说明

## CURSOR_TOOLSET_HOME

### 概述

`CURSOR_TOOLSET_HOME` 环境变量用于指定 CursorToolset 的根目录（类似 pip 的 `PYTHONUSERBASE` 或 Homebrew 的 `HOMEBREW_PREFIX`）。如果未设置此环境变量，将使用默认路径。

### 目录结构设计（参考 pip/brew）

```
~/.cursortoolsets/           <- CURSOR_TOOLSET_HOME（默认）
├── bin/                       <- CursorToolset 可执行文件
│   └── cursortoolset
├── repos/                     <- 工具集仓库源码（类似 brew 的 Cellar）
│   ├── github-action-toolset/
│   └── other-toolset/
└── config/                    <- 配置文件
    └── available-toolsets.json
```

**设计理念：**
- **全局安装目录**：类似 `~/.local`（pip）或 `/usr/local`（brew）
- **用户级别隔离**：每个用户有独立的安装环境
- **清晰的目录职责**：
  - `bin/` - 可执行文件
  - `repos/` - 工具集源码（Git 仓库）
  - `config/` - 配置文件

### 默认路径

- **Linux/macOS**: `~/.cursortoolsets`
- **Windows**: `%USERPROFILE%\.cursortoolsets`

### 使用方法

#### 1. 临时设置（当前会话有效）

**Linux/macOS:**
```bash
export CURSOR_TOOLSET_HOME=/path/to/your/home
cursortoolset install
```

**Windows PowerShell:**
```powershell
$env:CURSOR_TOOLSET_HOME = "C:\path\to\your\home"
cursortoolset install
```

**Windows CMD:**
```cmd
set CURSOR_TOOLSET_HOME=C:\path\to\your\home
cursortoolset install
```

#### 2. 永久设置

**Linux/macOS (添加到 ~/.bashrc 或 ~/.zshrc):**
```bash
export CURSOR_TOOLSET_HOME=/path/to/your/home
```

**Windows (系统环境变量):**
1. 打开"系统属性" > "高级" > "环境变量"
2. 添加用户变量 `CURSOR_TOOLSET_HOME`，值为目标路径

### 开发环境

在开发 CursorToolset 项目时，可以使用项目本地目录（避免影响系统安装）：

```bash
# 在项目根目录
cd /path/to/CursorToolset

# 临时设置环境变量指向项目的 .root 目录
export CURSOR_TOOLSET_HOME=$(pwd)/.root

# 构建和测试
go build -o cursortoolset .
./cursortoolset install
```

这确保了开发过程中的所有操作都使用项目本地的 `.root` 目录，不会影响系统安装。

### 路径说明

当设置了 `CURSOR_TOOLSET_HOME` 后：

| 目录用途 | 路径 |
|---------|------|
| 根目录 | `$CURSOR_TOOLSET_HOME` |
| 可执行文件 | `$CURSOR_TOOLSET_HOME/bin/cursortoolset` |
| 工具集仓库 | `$CURSOR_TOOLSET_HOME/repos/` |
| 配置文件 | `$CURSOR_TOOLSET_HOME/config/available-toolsets.json` |

### 示例

#### 示例 1: 使用自定义根目录

```bash
# 设置环境变量
export CURSOR_TOOLSET_HOME=$HOME/my-tools

# 安装工具集（会安装到 $HOME/my-tools/repos）
cursortoolset install github-action-toolset

# 目录结构：
# ~/my-tools/
# ├── bin/cursortoolset
# ├── repos/github-action-toolset/
# └── config/available-toolsets.json
```

#### 示例 2: 开发时使用项目本地目录

```bash
# 在项目根目录运行
cd /path/to/CursorToolset

# 设置环境变量指向项目的 .root 目录
export CURSOR_TOOLSET_HOME=$(pwd)/.root

# 构建
go build -o cursortoolset .

# 工具集会安装到 .root/repos
./cursortoolset install

# 目录结构：
# .root/
# ├── bin/        （空，开发时可执行文件在项目根目录）
# ├── repos/      （测试安装的工具集）
# └── config/     （测试配置文件）
```

#### 示例 3: 多环境隔离

```bash
# 生产环境
export CURSOR_TOOLSET_HOME=$HOME/.cursortoolsets
cursortoolset install

# 测试环境
export CURSOR_TOOLSET_HOME=$HOME/.cursortoolsets-test
cursortoolset install
```

### 与旧版本的兼容性

**环境变量名称变更：**
- ❌ 旧版本：`CURSOR_TOOLSET_ROOT`（已废弃）
- ✅ 新版本：`CURSOR_TOOLSET_HOME`（推荐）

**目录结构变更：**
- ❌ 旧版本：`~/.cursortoolsets/CursorToolset/`
- ✅ 新版本：`~/.cursortoolsets/`（更简洁）

**迁移方法：**
```bash
# 如果您之前设置了 CURSOR_TOOLSET_ROOT，请更新为 CURSOR_TOOLSET_HOME
# 在 ~/.bashrc 或 ~/.zshrc 中：
export CURSOR_TOOLSET_HOME=$HOME/.cursortoolsets  # 旧版本使用 CURSOR_TOOLSET_ROOT
```

### 注意事项

1. **环境变量优先级最高**: 如果设置了 `CURSOR_TOOLSET_HOME`，将优先使用该路径
2. **路径必须存在或有创建权限**: 安装时会自动创建目录结构
3. **开发环境隔离**: 开发时使用 `.root` 目录可以避免影响系统安装
4. **推荐使用绝对路径**: 避免相对路径导致的混淆
5. **工具集安装位置**: 工具集的 Git 仓库存储在 `repos/` 目录下
6. **配置文件位置**: 全局配置文件在 `config/` 目录下

### 与 pip/brew 的对比

| 特性 | CursorToolset | pip | Homebrew |
|-----|---------------|-----|----------|
| 环境变量 | `CURSOR_TOOLSET_HOME` | `PYTHONUSERBASE` | `HOMEBREW_PREFIX` |
| 默认路径 | `~/.cursortoolsets` | `~/.local` | `/usr/local` |
| 包存储 | `repos/` | `lib/python3.x/site-packages/` | `Cellar/` |
| 可执行文件 | `bin/` | `bin/` | `bin/` |
| 配置文件 | `config/` | - | `etc/` |

### 相关文件

- `pkg/paths/paths.go`: 路径处理逻辑
- `pkg/loader/loader.go`: 配置文件加载逻辑
- `install.sh/install.ps1`: 安装脚本
- `.gitignore`: `.root/` 目录已添加到忽略列表

