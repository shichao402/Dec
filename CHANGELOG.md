# 更新日志

## [v1.0.0] - 2024-12-04

### 🎉 新增功能

#### 智能版本控制（最新）
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
- **统一安装位置**: 所有平台统一使用 `~/.cursor/toolsets/CursorToolset/`
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
git clone https://github.com/firoyang/CursorToolset.git
cd CursorToolset
go build
# 手动配置 PATH...
```

**现在**: 一条命令即可
```bash
curl -fsSL https://raw.githubusercontent.com/firoyang/CursorToolset/main/install.sh | bash
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
- 提交 Issue: https://github.com/firoyang/CursorToolset/issues
- 提交 PR: https://github.com/firoyang/CursorToolset/pulls

