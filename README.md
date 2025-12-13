# Dec

Cursor 工具集管理器 - 一个简洁的包管理工具，用于管理和安装 Cursor AI 工具集。

## 设计理念

- **简单** - 像 pip/brew 一样简单：下载、解压、完成
- **安全** - 不执行任何脚本，只做文件分发
- **透明** - 所有包信息公开可查，SHA256 校验

## ✨ 特性

- 📦 **简单易用** - 类似 npm/pip 的命令行体验
- 🔒 **安全可靠** - SHA256 校验，不执行任何脚本
- 🌍 **跨平台** - 支持 Linux、macOS、Windows
- 🔗 **可执行程序暴露** - 自动创建符号链接，像系统命令一样使用
- 📚 **Registry 机制** - 中心化的包索引，易于发现和管理
- 🔄 **依赖管理** - 自动处理包依赖关系
- 💾 **智能缓存** - 减少重复下载，支持离线使用

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
# 更新包索引
dec registry update

# 列出可用包
dec list

# 安装包
dec install github-action-toolset

# 搜索包
dec search github

# 查看包详情
dec info github-action-toolset

# 卸载包
dec uninstall github-action-toolset
```

## 命令参考

### 包管理

| 命令 | 说明 |
|------|------|
| `install [name]` | 安装包（不指定则安装所有） |
| `uninstall <name>` | 卸载包 |
| `list [--installed]` | 列出可用/已安装的包 |
| `search <keyword>` | 搜索包 |
| `info <name>` | 查看包详情 |
| `update` | 更新管理器和包 |

### 包开发

| 命令 | 说明 |
|------|------|
| `init <name>` | 初始化新的工具集包项目 |
| `pack [dir]` | 标准化打包工具集包 🆕 |

### Registry 管理

| 命令 | 说明 |
|------|------|
| `registry update` | 更新本地包索引 |
| `registry list` | 列出 registry 中的包 |
| `registry add <name>` | 添加包（维护者） |
| `registry remove <name>` | 移除包（维护者） |
| `registry export` | 导出 registry |

### 包开发

| 命令 | 说明 |
|------|------|
| `init <name>` | 初始化新的包项目 |

### 其他

| 命令 | 说明 |
|------|------|
| `clean [--cache] [--all]` | 清理缓存或所有 |
| `update --self` | 更新管理器本身 |

## 目录结构

```
~/.decs/
├── repos/                    # 已安装的包
│   └── github-action-toolset/
├── cache/
│   ├── packages/             # 下载缓存
│   └── manifests/            # manifest 缓存
├── config/
│   └── registry.json         # 本地 registry
└── bin/
    └── dec
```

## 开发包

### 1. 初始化包项目

```bash
# 初始化新包
dec init my-toolset
cd my-toolset
```

生成的结构：

```
my-toolset/
├── package.json          # 包配置（必需）
├── .dec/       # 开发规则
├── README.md
└── .gitignore
```

### 2. 开发你的工具集

```bash
# 创建规则文件
mkdir -p rules
echo "# My Rules" > rules/my-rules.md

# 添加可执行程序（可选）
mkdir -p bin
echo "#!/bin/bash" > bin/mytool
chmod +x bin/mytool
```

### 3. 标准化打包 🆕

```bash
# 验证配置并打包
dec pack --verify

# 生成：my-toolset-1.0.0.tar.gz
# 自动计算并更新 SHA256
```

### 4. 发布

```bash
# 创建 GitHub Release
git tag v1.0.0
git push origin v1.0.0

# 在 GitHub 上创建 Release 并上传 tar.gz
# 更新 package.json 中的 dist.tarball 地址
```

> 💡 **提示**：包开发完整指南请通过 CursorColdStart 获取：`coldstart enable dec`

### package.json 规范

```json
{
  "name": "my-toolset",
  "displayName": "My Toolset",
  "version": "1.0.0",
  "description": "包描述",
  "author": "作者",
  "license": "MIT",
  "keywords": ["keyword1", "keyword2"],
  
  "repository": {
    "type": "git",
    "url": "https://github.com/user/my-toolset.git"
  },
  
  "bin": {
    "mytool": "bin/mytool",
    "mytool-helper": "scripts/helper.sh"
  },
  
  "dist": {
    "tarball": "https://github.com/user/my-toolset/releases/download/v1.0.0/my-toolset-1.0.0.tar.gz",
    "sha256": "abc123..."
  },
  
  "dec": {
    "minVersion": "1.0.0"
  }
}
```

#### 可执行程序配置 (bin)

通过 `bin` 字段可以暴露包中的可执行程序，安装时会自动创建符号链接：

```json
{
  "bin": {
    "command-name": "path/to/executable"
  }
}
```

安装后：
1. 符号链接会创建到 `~/.decs/bin/`
2. 用户将该目录添加到 PATH 后即可直接使用命令

```bash
# 添加到 PATH（一次性配置）
export PATH="$HOME/.decs/bin:$PATH"

# 直接使用命令
mytool --help
mytool-helper process
```

> 💡 **提示**：包开发完整指南请通过 CursorColdStart 获取：`coldstart enable dec`

### 发布包

1. **打包**
   ```bash
   tar -czvf my-toolset-1.0.0.tar.gz *
   shasum -a 256 my-toolset-1.0.0.tar.gz
   ```

2. **更新 package.json**
   - 更新 `version`
   - 更新 `dist.tarball` URL
   - 更新 `dist.sha256`

3. **创建 GitHub Release**
   - 创建 tag: `git tag v1.0.0`
   - 上传 tarball 到 Release

4. **提交到 Registry（自动）**
   - 使用推荐的 release workflow 模板
   - 发布时自动注册/同步到 Dec

> 💡 **提示**：包开发完整指南请通过 CursorColdStart 获取：`coldstart enable dec`

## Registry

Registry 是包的索引文件，托管在 GitHub Release 中。新版本采用预构建索引，用户更新时只需 **1 次请求**：

```json
{
  "version": "4",
  "updated_at": "2024-12-07T10:00:00Z",
  "packages": [
    {
      "repository": "https://github.com/user/my-toolset",
      "name": "my-toolset",
      "version": "1.0.0",
      "description": "包描述",
      "dist": {
        "tarball": "my-toolset-1.0.0.tar.gz",
        "sha256": "..."
      }
    }
  ]
}
```

### 自动注册机制

包开发者使用推荐的 release workflow 模板，发布时会**自动注册**到 Dec：

1. **首次发布**：自动创建注册 issue → CI 验证 → 添加到注册表
2. **后续发布**：自动创建同步 issue → CI 立即更新版本信息
3. **定时同步**：每小时自动同步所有包的最新信息

**无需手动操作**，完全自动化。

### 安装流程

```
1. 下载 registry.json（1 次请求，包含所有包信息）
2. 本地比对版本，确定需要更新的包
3. 从 dist.tarball 下载包
4. 验证 SHA256
5. 解压到本地
```

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DEC_HOME` | 安装根目录 | `~/.decs` |

## 从源码构建

```bash
git clone https://github.com/shichao402/Dec.git
cd Dec
go build -o dec .
```

## 📚 文档

详细文档请查看 [Documents/](Documents/) 目录。

### 包开发文档

包开发文档和规则现已通过 **CursorColdStart** 的 `dec` pack 提供：

```bash
# 在包项目中启用
coldstart enable dec
coldstart init .
```

### 开发者文档
- [架构设计](Documents/design/architecture/ARCHITECTURE.md) - 系统架构和设计理念
- [开发指南](Documents/development/setup/DEVELOPMENT.md) - 开发环境和流程
- [构建指南](Documents/development/deployment/BUILD.md) - 从源码构建
- [测试指南](Documents/development/testing/TESTING.md) - 运行和编写测试
- [发布流程](Documents/development/deployment/RELEASE.md) - 版本发布流程
- [开发规则](.cursor/rules/dec-development.md) - 项目开发规范

## 许可证

MIT
