# 更新日志

## [v1.2.0] - 2024-12-04

### 🎯 重大改进：目录结构重构（参考 pip/brew）

#### 设计理念变更

从"项目级工具"转变为"用户级包管理器"，参考 pip 和 Homebrew 的最佳实践。

#### 新的目录结构

**之前（v1.1.0）：**
```
~/.cursortoolsets/CursorToolset/    <- 根目录
├── bin/cursortoolset
└── available-toolsets.json
```

**现在（v1.2.0）：**
```
~/.cursortoolsets/                   <- 根目录（更简洁）
├── bin/                              <- 可执行文件
│   └── cursortoolset
├── repos/                            <- 工具集仓库（类似 brew Cellar）
│   └── github-action-toolset/
└── config/                           <- 配置文件
    └── available-toolsets.json
```

#### 环境变量更名

- ❌ 旧版本：`CURSOR_TOOLSET_ROOT`（已废弃）
- ✅ 新版本：`CURSOR_TOOLSET_HOME`（推荐）

**理由：** 更符合业界惯例（类似 `PYTHONUSERBASE`、`HOMEBREW_PREFIX`）

#### 核心改进

1. **清晰的职责分离**
   - `bin/` - 可执行文件
   - `repos/` - 工具集源码（Git 仓库）
   - `config/` - 配置文件

2. **更简洁的路径**
   - 去掉冗余的 `CursorToolset` 层级
   - 类似 `/usr/local` 或 `~/.local` 的设计

3. **更好的隔离**
   - 开发环境：使用 `.root/` 目录（已添加到 `.gitignore`）
   - 用户环境：使用 `~/.cursortoolsets/`

4. **向后兼容**
   - 安装脚本自动使用新路径
   - 配置文件加载支持多个位置（优先级：环境目录 > 工作目录）

#### 相关文件更新

- ✅ `pkg/paths/paths.go` - 完全重构路径逻辑
- ✅ `pkg/loader/loader.go` - 配置文件加载适配新路径
- ✅ `install.sh` - 使用新目录结构
- ✅ `install.ps1` - 使用新目录结构
- ✅ `uninstall.sh` - 使用新目录结构
- ✅ `uninstall.ps1` - 使用新目录结构
- ✅ `ENV_VARIABLES.md` - 完整文档更新
- ✅ 新增 `DIRECTORY_STRUCTURE.md` - 详细说明目录结构设计

#### 开发体验改进

**开发环境设置：**
```bash
# 使用项目本地目录（不影响系统安装）
export CURSOR_TOOLSET_HOME=$(pwd)/.root
go build -o cursortoolset .
./cursortoolset install
```

**目录说明：**
- `.root/repos/` - 测试安装的工具集
- `.root/config/` - 测试配置文件
- 已添加到 `.gitignore`

#### 迁移指南

**如果您使用旧版本（v1.1.0 或更早）：**

1. **卸载旧版本：**
   ```bash
   # 使用旧版本的卸载脚本
   curl -fsSL https://example.com/uninstall.sh | bash
   ```

2. **安装新版本：**
   ```bash
   # 自动使用新的目录结构
   curl -fsSL https://example.com/install.sh | bash
   ```

3. **更新环境变量（如果有自定义）：**
   ```bash
   # 旧版本
   export CURSOR_TOOLSET_ROOT=$HOME/.cursortoolsets/CursorToolset

   # 新版本
   export CURSOR_TOOLSET_HOME=$HOME/.cursortoolsets
   ```

#### 与其他包管理器的对比

| 特性 | CursorToolset | pip | Homebrew |
|-----|---------------|-----|----------|
| **环境变量** | `CURSOR_TOOLSET_HOME` | `PYTHONUSERBASE` | `HOMEBREW_PREFIX` |
| **默认路径** | `~/.cursortoolsets` | `~/.local` | `/usr/local` |
| **包存储** | `repos/` | `lib/python3.x/site-packages/` | `Cellar/` |
| **可执行文件** | `bin/` | `bin/` | `bin/` |
| **配置文件** | `config/` | - | `etc/` |

### 📚 新增文档

- **DIRECTORY_STRUCTURE.md** - 详细的目录结构说明文档
  - 设计理念
  - 目录职责
  - 工作流程
  - 开发环境设置
  - 常见问题

---

## [v1.1.0] - 2024-12-04

### 🎉 新增功能

#### 包管理功能对齐 pip/brew

##### 1. 卸载单个工具集 (`uninstall` 命令)
- **精确卸载**: 支持卸载指定的单个工具集
- **交互式确认**: 默认需要用户确认，防止误操作
- **强制模式**: `--force` 参数跳过确认
- **完整清理**: 同时删除源码、规则文件、脚本文件
```bash
cursortoolset uninstall <toolset-name>
cursortoolset uninstall <toolset-name> --force
```

##### 2. 搜索工具集 (`search` 命令)
- **关键词搜索**: 支持模糊匹配
- **多字段搜索**: 搜索名称、显示名称、描述、仓库地址
- **匹配高亮**: 显示哪些字段匹配了关键词
- **状态显示**: 显示工具集是否已安装
```bash
cursortoolset search github
cursortoolset search action
```

##### 3. 查看详细信息 (`info` 命令)
- **完整信息展示**: 名称、版本、作者、许可证、关键词
- **安装状态**: 显示是否已安装及安装路径
- **安装目标**: 列出所有安装目标及其配置
- **功能列表**: 展示工具集提供的功能特性
- **文档链接**: 显示相关文档链接
```bash
cursortoolset info <toolset-name>
```

##### 4. 版本管理 (`install --version`)
- **指定版本安装**: 支持 Git 标签或提交哈希
- **版本切换**: 已安装时可切换到指定版本
- **自动 fetch**: 自动获取远程标签
```bash
cursortoolset install <name> --version v1.0.0
cursortoolset install <name> -v abc123
```

##### 5. SHA256 校验
- **安全验证**: 支持在配置中指定 SHA256 校验和
- **自动验证**: 安装时自动计算并比对
- **失败保护**: 校验失败时中止安装
- **配置方式**: 在 `available-toolsets.json` 中添加 `sha256` 字段

##### 6. 依赖管理
- **声明依赖**: 在配置中声明工具集依赖关系
- **自动安装**: 安装工具集时自动安装所有依赖
- **避免重复**: 智能检测已安装的依赖，避免重复安装
- **依赖检查**: 安装前检查依赖是否存在
- **配置方式**: 在 `available-toolsets.json` 中添加 `dependencies` 数组

### 🔧 改进

#### 类型系统扩展
- `ToolsetInfo` 新增 `sha256` 和 `dependencies` 字段
- `loader` 包新增 `ToolsetSearchResult` 类型
- `loader` 包新增 `SearchToolset` 搜索函数

#### 安装器增强
- `Installer` 新增 `Version` 字段支持版本控制
- 新增 `SetVersion` 方法设置安装版本
- 新增 `checkoutVersion` 方法切换 Git 版本
- 新增 `verifySHA256` 方法验证校验和
- 新增 `calculateDirSHA256` 方法计算目录哈希
- 新增 `UninstallToolset` 方法卸载工具集
- 新增 `removeInstalledFiles` 和 `removeToolsetDir` 辅助方法

#### 命令系统
- 注册新命令: `uninstall`, `search`, `info`
- 优化命令排序，更符合使用习惯
- 完善帮助文档

### 📚 文档

#### 新增文档
- **NEW_FEATURES.md**: 详细的新功能说明和使用示例
- **DEMO.md**: 完整的功能演示和使用场景

#### 更新文档
- **README.md**: 更新功能列表和使用方法
- **version.json**: 版本号更新为 v1.1.0

### 🐛 修复

- 无重大 Bug 修复（新功能版本）

### ⚡ 性能

- 搜索功能使用内存搜索，性能优秀
- 依赖安装避免重复操作，提升效率

### 🔒 安全

- SHA256 校验确保工具集完整性
- 交互式确认防止误删除

---

## [v1.0.0] - 2024-12-04

### 🎉 新增功能

#### 智能版本控制
- **版本比较引擎**: 新增 `pkg/version` 包，实现语义化版本号比较
- **自动版本检查**: 更新前自动检查是否有新版本
- **避免重复更新**: 已是最新版本时自动跳过更新
- **详细版本信息**: 显示当前版本 vs 最新版本对比
- **配置文件检查**: 比较文件内容，避免无意义的配置更新
- **工具集状态检查**: 使用 `git fetch` + `git status` 检查是否落后于远程
- **文档完善**: 新增 `VERSION_CONTROL.md` 详细说明版本控制机制

#### 一键安装脚本
- **Linux/macOS**: 通过 `curl -fsSL https://raw.githubusercontent.com/.../install.sh | bash` 一键安装
- **Windows**: 通过 `iwr -useb https://raw.githubusercontent.com/.../install.ps1 | iex` 一键安装
- 自动检测系统平台和架构
- 自动配置环境变量
- 支持从源码构建或下载预编译版本

#### 更新功能 (`update` 命令)
- **自更新**: `cursortoolset update --self` 更新 CursorToolset 本身
- **更新配置**: `cursortoolset update --available` 更新 available-toolsets.json
- **更新工具集**: `cursortoolset update --toolsets` 更新所有已安装的工具集
- **一键全部更新**: `cursortoolset update` 执行所有更新
- **Windows 特殊处理**: 解决文件占用问题

#### 安装位置优化
- **统一安装位置**: 所有平台统一使用 `~/.cursortoolsets/CursorToolset/`
- **环境变量集成**: 自动添加到系统 PATH
- **全局可用**: 安装后可在任何位置运行

#### 版本管理
- 添加 `--version` 参数查看版本信息
- 构建时注入版本号和构建时间
- Makefile 支持版本管理

### 🔧 改进

#### 构建系统
- 新增 `Makefile` 简化构建流程
  - `make build`: 构建当前平台
  - `make build-all`: 构建所有平台
  - `make test`: 运行测试
  - `make install`: 安装到本地
  - `make clean`: 清理构建产物

#### GitHub Actions
- 新增 Release 工作流
- 自动构建多平台版本（Linux/macOS/Windows, amd64/arm64）
- 自动生成 SHA256 校验和
- 支持预编译版本下载

#### 文档
- **INSTALL_GUIDE.md**: 详细的安装指南
- **CHANGELOG.md**: 更新日志
- 更新 README.md 添加快速安装说明
- 更新所有文档反映新的安装方式

### 📦 测试

- 新增 `test-update.sh` 测试更新功能
- 更新所有测试脚本适配新的目录结构
- 所有单元测试通过
- 集成测试全部通过

### 🎯 使用体验提升

**之前**: 需要手动克隆、构建、配置环境变量
```bash
git clone https://github.com/shichao402/CursorToolset.git
cd CursorToolset
go build
# 手动配置 PATH...
```

**现在**: 一条命令即可
```bash
curl -fsSL https://raw.githubusercontent.com/shichao402/CursorToolset/main/install.sh | bash
```

### 💡 技术亮点

1. **跨平台一致性**: Linux、macOS、Windows 使用统一的安装位置和逻辑
2. **智能更新**: Windows 上处理文件占用，Unix 系统直接替换
3. **自我管理**: 像 Homebrew 一样，可以更新自己
4. **零依赖运行**: 安装后不需要 Go 环境
5. **预编译支持**: 从 GitHub Releases 自动下载

---

## [v0.1.0] - 之前版本

### 初始功能

- 工具集安装 (`install` 命令)
- 工具集列表 (`list` 命令)
- 工具集清理 (`clean` 命令)
- 基于 `available-toolsets.json` 的配置管理
- 支持从 GitHub 克隆工具集
- 基于 `toolset.json` 的文件安装

---

## 路线图

### 未来计划

- [ ] 支持本地 toolset 目录安装（不需要 Git）
- [ ] 支持 toolset 搜索功能
- [ ] 支持 toolset 评分和推荐
- [ ] Web 界面管理工具集
- [ ] 更丰富的工具集模板
- [ ] 社区工具集仓库

### 欢迎贡献

如果你有好的想法或发现问题，欢迎：
- 提交 Issue: https://github.com/shichao402/CursorToolset/issues
- 提交 PR: https://github.com/shichao402/CursorToolset/pulls

