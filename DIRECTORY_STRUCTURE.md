# 目录结构说明

## 设计理念

CursorToolset 的目录结构参考了 pip 和 Homebrew 的设计理念：

- **用户级别的全局安装目录**：类似 `~/.local`（pip）或 `/usr/local`（Homebrew）
- **清晰的职责分离**：可执行文件、配置、源码分别存放
- **环境变量配置**：通过 `CURSOR_TOOLSET_HOME` 自定义安装位置

## 目录结构

### 默认安装目录

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

### 开发环境目录（项目根目录）

```
CursorToolset/                         <- 项目根目录
├── .root/                             <- 开发测试目录（被 .gitignore 忽略）
│   ├── bin/                           <- （可选）测试可执行文件
│   ├── repos/                         <- 测试安装的工具集
│   └── config/                        <- 测试配置文件
├── cmd/                               <- 命令行命令实现
├── pkg/                               <- 核心包
│   ├── paths/                         <- 路径处理
│   ├── loader/                        <- 配置加载
│   └── installer/                     <- 安装器
├── main.go                            <- 主入口
├── available-toolsets.json            <- 默认工具集列表（用于开发和分发）
└── ...                                <- 其他文件
```

## 目录职责

### 1. bin/ - 可执行文件目录

**用途：** 存放 CursorToolset 的可执行文件

**路径：** `$CURSOR_TOOLSET_HOME/bin/cursortoolset`

**特点：**
- 需要添加到 `PATH` 环境变量
- 安装脚本会自动配置
- 只包含 CursorToolset 自身的可执行文件

**示例：**
```bash
# 添加到 PATH
export PATH="$HOME/.cursortoolsets/bin:$PATH"

# 使用
cursortoolset install
```

### 2. repos/ - 工具集仓库目录

**用途：** 存储所有工具集的 Git 仓库（类似 Homebrew 的 Cellar）

**路径：** `$CURSOR_TOOLSET_HOME/repos/`

**特点：**
- 每个工具集一个子目录
- 保留完整的 Git 仓库结构
- 用于读取 `toolset.json` 和源文件
- 安装时从这里复制文件到项目目录

**工作流程：**
```bash
# 安装工具集
cursortoolset install github-action-toolset

# 目录结构
repos/
└── github-action-toolset/           <- Git 仓库克隆到这里
    ├── .git/                        <- Git 元数据
    ├── toolset.json                 <- 配置文件
    ├── core/                        <- 源文件
    │   ├── rules/*.mdc              <- AI 规则文件
    │   └── tools/...                <- 工具脚本
    └── ...

# 然后根据 toolset.json 的配置，复制文件到项目目录
# 例如：core/rules/*.mdc -> .cursor/rules/github-actions/
```

### 3. config/ - 配置文件目录

**用途：** 存储全局配置文件

**路径：** `$CURSOR_TOOLSET_HOME/config/`

**主要文件：**
- `available-toolsets.json` - 可用工具集列表

**特点：**
- 用户级别的全局配置
- 独立于项目目录
- 便于版本控制和备份

## 环境变量

### CURSOR_TOOLSET_HOME

**作用：** 指定 CursorToolset 的根目录

**默认值：**
- Linux/macOS: `~/.cursortoolsets`
- Windows: `%USERPROFILE%\.cursortoolsets`

**使用方法：**
```bash
# 临时设置
export CURSOR_TOOLSET_HOME=/path/to/custom/location

# 永久设置（添加到 ~/.bashrc 或 ~/.zshrc）
export CURSOR_TOOLSET_HOME=$HOME/.cursortoolsets
```

**开发环境：**
```bash
# 使用项目本地目录（不影响系统安装）
export CURSOR_TOOLSET_HOME=$(pwd)/.root
./cursortoolset install
```

## 与其他包管理器的对比

| 特性 | CursorToolset | pip | Homebrew |
|-----|---------------|-----|----------|
| **根目录环境变量** | `CURSOR_TOOLSET_HOME` | `PYTHONUSERBASE` | `HOMEBREW_PREFIX` |
| **默认根目录** | `~/.cursortoolsets` | `~/.local` | `/usr/local` |
| **可执行文件** | `bin/` | `bin/` | `bin/` |
| **包存储** | `repos/` | `lib/python3.x/site-packages/` | `Cellar/` |
| **配置文件** | `config/` | - | `etc/` |
| **包类型** | Git 仓库 | Python 包 | Formula |
| **安装位置** | 用户主目录 | 用户主目录 | 系统目录或用户主目录 |

## 工作流程

### 1. 安装 CursorToolset

```bash
# 使用安装脚本
curl -fsSL https://example.com/install.sh | bash

# 创建的目录结构：
~/.cursortoolsets/
├── bin/cursortoolset                  <- 可执行文件
└── config/available-toolsets.json    <- 配置文件
```

### 2. 安装工具集

```bash
# 安装工具集
cursortoolset install github-action-toolset

# 步骤：
# 1. 克隆仓库到 repos/github-action-toolset/
# 2. 读取 toolset.json
# 3. 根据配置复制文件到项目目录（如 .cursor/rules/）
```

### 3. 更新工具集

```bash
# 更新指定工具集
cursortoolset update github-action-toolset

# 步骤：
# 1. 进入 repos/github-action-toolset/
# 2. 执行 git pull
# 3. 重新读取 toolset.json
# 4. 更新已安装的文件
```

### 4. 卸载工具集

```bash
# 卸载工具集
cursortoolset uninstall github-action-toolset

# 步骤：
# 1. 删除项目目录中安装的文件
# 2. 删除 repos/github-action-toolset/
```

## 开发环境设置

### 使用 .root/ 目录

开发时推荐使用项目根目录下的 `.root/` 目录，避免影响系统安装：

```bash
# 1. 设置环境变量
export CURSOR_TOOLSET_HOME=$(pwd)/.root

# 2. 构建
go build -o cursortoolset .

# 3. 测试安装
./cursortoolset install

# 目录结构：
.root/
├── repos/                    <- 测试安装的工具集
└── config/                   <- 测试配置文件
```

### .gitignore 配置

`.root/` 目录已添加到 `.gitignore`，不会提交到版本控制：

```gitignore
# 开发根目录（开发时使用）
.root/
```

## 迁移指南

### 从旧版本迁移

如果您使用的是旧版本（目录在 `~/.cursortoolsets/CursorToolset/`）：

**旧版本结构：**
```
~/.cursortoolsets/CursorToolset/      <- 旧的根目录
├── bin/cursortoolset
└── available-toolsets.json
```

**新版本结构：**
```
~/.cursortoolsets/                    <- 新的根目录（更简洁）
├── bin/cursortoolset
├── repos/                             <- 新增：仓库目录
└── config/available-toolsets.json    <- 移动到 config/
```

**迁移步骤：**

1. **卸载旧版本：**
   ```bash
   # 使用旧版本的卸载脚本
   curl -fsSL https://example.com/uninstall.sh | bash
   ```

2. **安装新版本：**
   ```bash
   # 使用新版本的安装脚本
   curl -fsSL https://example.com/install.sh | bash
   ```

3. **重新安装工具集：**
   ```bash
   cursortoolset install
   ```

4. **更新环境变量（如果有）：**
   ```bash
   # 旧版本
   export CURSOR_TOOLSET_ROOT=$HOME/.cursortoolsets/CursorToolset

   # 新版本
   export CURSOR_TOOLSET_HOME=$HOME/.cursortoolsets
   ```

## 常见问题

### Q: 为什么改变目录结构？

**A:** 参考 pip 和 Homebrew 的最佳实践，新结构具有以下优势：
- ✅ 更清晰的职责分离（bin/repos/config）
- ✅ 更简洁的路径（去掉冗余的 `CursorToolset` 层级）
- ✅ 更符合 Unix/Linux 传统（类似 `/usr/local`）
- ✅ 更容易理解和维护

### Q: repos/ 目录占用空间怎么办？

**A:** Git 仓库确实会占用一定空间，但这是必要的：
- 便于更新（`git pull`）
- 保留版本历史
- 支持版本切换
- 类似 Homebrew 的 Cellar 目录

如果需要清理，可以使用：
```bash
# 清理特定工具集
cursortoolset uninstall <toolset-name>

# 或手动删除
rm -rf ~/.cursortoolsets/repos/<toolset-name>
```

### Q: 可以改变安装位置吗？

**A:** 可以！使用 `CURSOR_TOOLSET_HOME` 环境变量：
```bash
export CURSOR_TOOLSET_HOME=/path/to/custom/location
cursortoolset install
```

### Q: 开发时如何避免影响系统安装？

**A:** 使用项目本地的 `.root/` 目录：
```bash
export CURSOR_TOOLSET_HOME=$(pwd)/.root
./cursortoolset install
```

## 总结

新的目录结构遵循以下原则：

1. **用户级别隔离**：每个用户有独立的安装环境
2. **职责清晰**：bin/repos/config 各司其职
3. **符合惯例**：参考 pip 和 Homebrew 的最佳实践
4. **易于理解**：目录名称直观，便于维护
5. **灵活配置**：支持通过环境变量自定义位置
6. **开发友好**：支持本地开发环境隔离
